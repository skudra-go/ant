package concurrency

import (
	"database/sql"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/skudra-go/ant"
	"github.com/skudra-go/ant/dialect"
	"github.com/skudra-go/ant/internal/testdb"
)

// errRollBack makes a transaction roll back on purpose.
var errRollBack = errors.New("roll back on purpose")

// Each worker takes a session of the shared context, as a request does, and
// changes its own rows through SaveChanges: loads, edits, a payment added to and
// one taken out of a loaded relation, all saved as one unit of work.
func TestSessionPerWorker(t *testing.T) {
	each(t, func(t *testing.T, target testdb.Target) {
		db := shared(t, target)
		ctx := t.Context()

		const n, rounds = 12, 6

		errs := workers(n, func(w int) error {
			session := ant.Session(db, ant.WithTracking(true))

			account := &Account{
				Owner:    fmt.Sprintf("session-%d", w),
				Payments: []Payment{{Amount: 1, Note: "opening"}},
			}

			if err := ant.Add(ctx, session, account); err != nil {
				return err
			}

			if err := session.SaveChanges(ctx); err != nil {
				return err
			}

			for i := range rounds {
				// A new session each round, as a new request would have.
				session := ant.Session(db, ant.WithTracking(true))

				a, err := session.Accounts.Include(&A.Payments, byID).Find(ctx, account.ID)
				if err != nil {
					return err
				}

				a.Balance += 10
				a.Payments = append(a.Payments, Payment{Amount: int64(10 + i), Note: "round"})

				if i%2 == 1 {
					a.Payments = a.Payments[1:] // the oldest goes
				}

				if err := session.SaveChanges(ctx); err != nil {
					return err
				}

				changed, err := session.HasChanges(ctx)
				if err != nil {
					return err
				}

				if changed {
					return errors.New("changes left after SaveChanges")
				}
			}

			return nil
		})
		noErrors(t, errs)

		accounts, err := db.Accounts.Include(&A.Payments).
			Where(ant.Like(&A.Owner, "session-%")).
			All(ctx)
		require.NoError(t, err)
		require.Len(t, accounts, n)

		for _, a := range accounts {
			require.EqualValues(t, 10*rounds, a.Balance, a.Owner)
			require.Equal(t, []int64{12, 13, 14, 15}, amounts(a), a.Owner)
		}
	})
}

// Workers that load the same row and save it race; the version field makes the
// loser's save fail with ErrConcurrency instead of overwriting the winner. Each
// retries with a fresh session, so every increment lands exactly once.
func TestOptimisticConcurrency(t *testing.T) {
	each(t, func(t *testing.T, target testdb.Target) {
		db := shared(t, target)
		ctx := t.Context()
		account := seedAccounts(t, db, 1, 0)[0]

		const n, rounds = 8, 5

		var conflicts atomic.Int64

		errs := workers(n, func(int) error {
			for range rounds {
				for {
					session := ant.Session(db, ant.WithTracking(true))

					a, err := session.Accounts.Find(ctx, account.ID)
					if err != nil {
						return err
					}

					a.Balance++

					err = session.SaveChanges(ctx)
					if errors.Is(err, ant.ErrConcurrency) {
						conflicts.Add(1)

						entry, err := ant.Entry(ctx, session, a)
						if err != nil {
							return err
						}

						if entry.State() != ant.Modified {
							return errors.New("a failed save left the entity as saved")
						}

						continue
					}

					if err != nil {
						return err
					}

					break
				}
			}

			return nil
		})
		noErrors(t, errs)

		stored, err := db.Accounts.Find(ctx, account.ID)
		require.NoError(t, err)
		require.EqualValues(t, n*rounds, stored.Balance, "an increment was lost or doubled")
		require.EqualValues(t, account.Version+n*rounds, stored.Version)
		t.Logf("%d conflicts", conflicts.Load())
	})
}

// Transfers between random accounts, each in a transaction: on databases with
// row locks, each locks both rows in key order first. Deadlocks and serialization
// failures are retried. Money is neither made nor lost.
func TestTransfers(t *testing.T) {
	each(t, func(t *testing.T, target testdb.Target) {
		db := shared(t, target)
		ctx := t.Context()
		accounts := seedAccounts(t, db, 6, 1000)
		before := total(t, db)
		locks := dialect.Supports(target.Dialect, dialect.RowLocks)

		var moved atomic.Int64

		errs := workers(12, func(w int) error {
			for i := range 8 {
				from, to := accounts[(w+i)%6], accounts[(w*7+i*3+1)%6]
				if from.ID == to.ID {
					continue
				}

				amount := int64(w + i + 1)

				err := db.Tx(
					ctx,
					func(tx *BankDB) error {
						q := tx.Accounts.Where(ant.In(&A.ID, from.ID, to.ID)).OrderBy(&A.ID)

						if locks {
							q = q.ForUpdate()
						}

						if _, err := q.All(ctx); err != nil {
							return err
						}

						for _, step := range []struct {
							id     int64
							amount int64
						}{{from.ID, -amount}, {to.ID, amount}} {
							changed, err := tx.Accounts.Where(ant.Eq(&A.ID, step.id)).
								Update(ctx, ant.Inc(&A.Balance, step.amount))
							if err != nil {
								return err
							}

							if changed != 1 {
								return fmt.Errorf("moved money on %d rows", changed)
							}
						}

						_, err := tx.Payments.InsertMany(ctx, []*Payment{
							{AccountID: from.ID, Amount: -amount, Note: "transfer"},
							{AccountID: to.ID, Amount: amount, Note: "transfer"},
						})

						return err
					},
					ant.Attempts(30),
				)
				if err != nil {
					return err
				}

				moved.Add(amount)
			}

			return nil
		})
		noErrors(t, errs)
		require.Equal(t, before, total(t, db), "money was made or lost")
		require.Positive(t, moved.Load())

		stored, err := db.Accounts.Include(&A.Payments).OrderBy(&A.ID).All(ctx)
		require.NoError(t, err)

		for _, a := range stored {
			sum := int64(0)

			for _, p := range a.Payments {
				sum += p.Amount
			}

			require.EqualValues(
				t,
				1000+sum,
				a.Balance,
				"account %s disagrees with its payments",
				a.Owner,
			)
		}
	})
}

// Workers that all upsert one counter, which none of them knows exists, each
// add one hit: the first inserts it, and the rest update it.
func TestUpsertRace(t *testing.T) {
	each(t, func(t *testing.T, target testdb.Target) {
		if !dialect.Supports(target.Dialect, dialect.OnConflict) {
			t.Skip("no upsert on " + target.Name + ": TestMergeRace covers its MERGE")
		}

		db := shared(t, target)
		ctx := t.Context()

		const n, rounds = 12, 5

		errs := workers(n, func(w int) error {
			for i := range rounds {
				_, err := db.Counters.Upsert(
					ctx,
					&Counter{Name: "hits", Hits: 1},
					ant.OnConflict(&C.Name).Set(ant.Inc(&C.Hits, int64(1))),
				)
				if err != nil {
					return fmt.Errorf("upsert %d: %w", i, err)
				}

				_, err = db.Counters.InsertMany(
					ctx,
					[]*Counter{{Name: fmt.Sprintf("bulk-%d", i), Hits: 1}},
					ant.WithConflict(ant.OnConflict(&C.Name).Set(ant.Inc(&C.Hits, int64(1)))),
				)
				if err != nil {
					return fmt.Errorf("insert many %d: %w", i, err)
				}
			}

			return nil
		})
		noErrors(t, errs)

		counters, err := db.Counters.OrderBy(&C.Name).All(ctx)
		require.NoError(t, err)
		require.Len(t, counters, rounds+1)

		for _, c := range counters {
			want := n

			if c.Name == "hits" {
				want = n * rounds
			}

			require.EqualValues(t, want, c.Hits, c.Name)
		}
	})
}

// hit is a source row for MERGE.
type hit struct {
	Name string
	N    int64
}

var H = ant.Fields[hit]()

// Workers that all MERGE one counter, as an upsert, each add one hit: the first
// inserts the row, and the rest update it. PostgreSQL documents that a MERGE
// whose insert meets a row a concurrent one inserted fails with a unique
// violation, which the application retries; SQL Server's MERGE must not.
func TestMergeRace(t *testing.T) {
	each(t, func(t *testing.T, target testdb.Target) {
		if !dialect.Supports(target.Dialect, dialect.Merge) {
			t.Skip("no MERGE on " + target.Name)
		}

		db := shared(t, target)
		ctx := t.Context()

		if target.Name == "postgres" {
			version, err := ant.Raw[int](
				db,
				"SELECT current_setting('server_version_num')::int",
			).First(ctx)
			require.NoError(t, err)

			if *version < 150000 {
				t.Skip("MERGE needs PostgreSQL 15")
			}
		}

		const n, rounds = 12, 5

		var violations atomic.Int64

		errs := workers(n, func(int) error {
			for range rounds {
				for {
					_, err := ant.Merge(&db.Counters, ant.Values(db, hit{"merged", 1}), ant.EqCol(&C.Name, &H.Name)).
						WhenMatched(ant.Inc(&C.Hits, int64(1))).
						WhenNotMatched(ant.Into(&C.Name, &H.Name), ant.Into(&C.Hits, &H.N)).
						Exec(ctx)
					if ant.IsUniqueViolation(err) {
						violations.Add(1)

						continue
					}

					if err != nil {
						return err
					}

					break
				}
			}

			return nil
		})
		noErrors(t, errs)

		got, err := db.Counters.Find(ctx, "merged")
		require.NoError(t, err)
		require.EqualValues(t, n*rounds, got.Hits)

		if target.Name == "sqlserver" {
			require.Zero(t, violations.Load(), "a MERGE upsert collided with another")
		}
	})
}

// FirstOrCreate from many workers at once creates the row once; the others find
// it, whether they run in a transaction or not.
func TestFirstOrCreateRace(t *testing.T) {
	each(t, func(t *testing.T, target testdb.Target) {
		db := shared(t, target)
		ctx := t.Context()

		for round, inTx := range []bool{false, true} {
			name := fmt.Sprintf("first-%d", round)

			var created atomic.Int64

			errs := workers(12, func(w int) error {
				find := func(tx *BankDB) error {
					got, isNew, err := tx.Counters.FirstOrCreate(
						ctx,
						&Counter{Name: name, Hits: int64(w)},
						ant.Eq(&C.Name, name),
					)
					if err != nil {
						return err
					}

					if got.Name != name {
						return fmt.Errorf("got %q", got.Name)
					}

					if isNew {
						created.Add(1)
					}

					return nil
				}

				if !inTx {
					return find(db)
				}

				return db.Tx(ctx, find, ant.Attempts(10))
			})
			noErrors(t, errs)
			require.EqualValues(t, 1, created.Load(), "in a transaction: %v", inTx)
		}
	})
}

// A session's transaction that rolls back leaves its tracker as it was before,
// so the same session saves the work again, while other workers do the same in
// sessions of their own.
func TestSessionRollbacks(t *testing.T) {
	each(t, func(t *testing.T, target testdb.Target) {
		db := shared(t, target)
		ctx := t.Context()
		accounts := seedAccounts(t, db, 8, 0)

		errs := workers(8, func(w int) error {
			session := ant.Session(db, ant.WithTracking(true))

			a, err := session.Accounts.Include(&A.Payments).Find(ctx, accounts[w].ID)
			if err != nil {
				return err
			}

			for i := range 4 {
				a.Balance += 5
				a.Payments = append(a.Payments, Payment{Amount: 5, Note: fmt.Sprint(i)})

				err := session.Tx(ctx, func(tx *BankDB) error {
					if err := tx.SaveChanges(ctx); err != nil {
						return err
					}

					if i%2 == 0 {
						return errRollBack
					}

					return nil
				})

				switch {
				case i%2 == 0 && !errors.Is(err, errRollBack):
					return fmt.Errorf("round %d: %v", i, err)
				case i%2 == 1 && err != nil:
					return fmt.Errorf("round %d: %w", i, err)
				case i%2 == 0 && a.Payments[len(a.Payments)-1].ID != 0:
					// The key the insert generated went with its row, so the
					// next save inserts the payment again.
					return fmt.Errorf("round %d: the rollback left the payment its key", i)
				}
			}

			return nil
		})
		noErrors(t, errs)

		stored, err := db.Accounts.Include(&A.Payments).OrderBy(&A.ID).All(ctx)
		require.NoError(t, err)

		for _, a := range stored {
			require.EqualValues(t, 20, a.Balance, a.Owner)
			require.Equal(t, []int64{5, 5, 5, 5}, amounts(a), a.Owner)
		}
	})
}

// Serializable transactions that read and then write one row conflict, and
// Attempts retries the loser until every increment has landed once.
func TestSerializableRetries(t *testing.T) {
	each(t, func(t *testing.T, target testdb.Target) {
		db := shared(t, target)
		ctx := t.Context()
		account := seedAccounts(t, db, 1, 0)[0]

		const n, rounds = 6, 4

		errs := workers(n, func(int) error {
			for range rounds {
				err := db.Tx(
					ctx,
					func(tx *BankDB) error {
						a, err := tx.Accounts.Find(ctx, account.ID)
						if err != nil {
							return err
						}

						_, err = tx.Accounts.Where(ant.Eq(&A.ID, a.ID)).
							Update(ctx, ant.Set(&A.Balance, a.Balance+1))

						return err
					},
					ant.Isolation(sql.LevelSerializable),
					ant.Attempts(200),
				)
				if err != nil {
					return err
				}
			}

			return nil
		})
		noErrors(t, errs)

		stored, err := db.Accounts.Find(ctx, account.ID)
		require.NoError(t, err)
		require.EqualValues(t, n*rounds, stored.Balance)
	})
}

// byID orders included payments, oldest first.
func byID(q *ant.Query[Payment]) *ant.Query[Payment] { return q.OrderBy(&P.ID) }
