package concurrency

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/skudra-go/ant"
	"github.com/skudra-go/ant/internal/testdb"
)

// Transactions on one shared context run side by side, each on a connection of
// its own: what one commits stays, what another rolls back goes, and a savepoint
// inside either takes back only its own part.
func TestTransactionsSideBySide(t *testing.T) {
	each(t, func(t *testing.T, target testdb.Target) {
		db := shared(t, target)
		ctx := t.Context()

		const n = 16

		var inserted, observed atomic.Int64

		require.NoError(t, ant.Observe(db, ant.Observer[Account]{
			Inserted: func(_ context.Context, a *Account) {
				if a.Owner == "" {
					panic("observed an empty account")
				}

				observed.Add(1)
			},
		}))

		errs := workers(n, func(w int) error {
			err := db.Tx(
				ctx,
				func(tx *BankDB) error {
					a := &Account{Owner: fmt.Sprintf("tx-%d", w)}
					if err := tx.Accounts.Insert(ctx, a); err != nil {
						return err
					}

					// A savepoint that fails takes back only its own payment.
					inner := tx.Tx(ctx, func(tx *BankDB) error {
						if err := tx.Payments.Insert(
							ctx,
							&Payment{AccountID: a.ID, Amount: -1, Note: "inner"},
						); err != nil {
							return err
						}

						return errRollBack
					})

					if !errors.Is(inner, errRollBack) {
						return fmt.Errorf("savepoint: %v", inner)
					}

					if err := tx.Payments.Insert(
						ctx,
						&Payment{AccountID: a.ID, Amount: 1, Note: "outer"},
					); err != nil {
						return err
					}

					if w%2 == 1 {
						return errRollBack
					}

					return nil
				},
				ant.Attempts(10),
			)

			switch {
			case w%2 == 1 && !errors.Is(err, errRollBack):
				return fmt.Errorf("rollback: %v", err)
			case w%2 == 0 && err != nil:
				return err
			case w%2 == 0:
				inserted.Add(1)
			}

			return nil
		})
		noErrors(t, errs)

		accounts, err := db.Accounts.Include(&A.Payments).Where(ant.Like(&A.Owner, "tx-%")).All(ctx)
		require.NoError(t, err)
		require.EqualValues(t, inserted.Load(), len(accounts))

		for _, a := range accounts {
			require.Equal(t, []int64{1}, amounts(a), a.Owner)
		}

		require.Equal(
			t,
			inserted.Load(),
			observed.Load(),
			"observers run once per committed insert",
		)
	})
}

// A transaction is one connection, which runs one statement at a time, so the
// goroutines of a worker pool that share one take turns with it, each with a
// savepoint of its own; the transaction then commits all of their work or none.
// Statements from two goroutines at once fail in the driver, and their
// savepoints would interleave on the connection's one stack.
func TestWorkersTakeTurnsInATransaction(t *testing.T) {
	each(t, func(t *testing.T, target testdb.Target) {
		db := shared(t, target)
		ctx := t.Context()
		accounts := seedAccounts(t, db, 4, 0)

		const n = 8

		for _, commit := range []bool{true, false} {
			t.Run(fmt.Sprintf("commit %v", commit), func(t *testing.T) {
				before, err := db.Payments.Count(ctx)
				require.NoError(t, err)

				err = db.Tx(ctx, func(tx *BankDB) error {
					var turn sync.Mutex

					errs := workers(n, func(w int) error {
						a := accounts[w%len(accounts)]

						turn.Lock()
						defer turn.Unlock()

						if err := tx.Payments.Insert(
							ctx,
							&Payment{AccountID: a.ID, Amount: 1, Note: "turns"},
						); err != nil {
							return err
						}

						if _, err := tx.Accounts.Where(ant.Eq(&A.ID, a.ID)).
							Update(ctx, ant.Inc(&A.Balance, int64(1))); err != nil {
							return err
						}

						inner := tx.Tx(ctx, func(tx *BankDB) error {
							if err := tx.Payments.Insert(
								ctx,
								&Payment{AccountID: a.ID, Amount: 99, Note: "turns"},
							); err != nil {
								return err
							}

							return errRollBack
						})

						if !errors.Is(inner, errRollBack) {
							return fmt.Errorf("savepoint: %w", inner)
						}

						return nil
					})

					if err := errors.Join(errs...); err != nil {
						return err
					}

					if !commit {
						return errRollBack
					}

					return nil
				})

				if commit {
					require.NoError(t, err)
				} else {
					require.ErrorIs(t, err, errRollBack)
				}

				after, err := db.Payments.Count(ctx)
				require.NoError(t, err)

				want := before

				if commit {
					want += n
				}

				require.Equal(t, want, after)
			})
		}

		require.EqualValues(t, n, total(t, db), "only the committed run moved the balances")
	})
}

// A transaction that waits for a connection from a full pool stops waiting when
// its context ends, and one whose context ends while it is open, as when a
// request is cancelled before it commits, is rolled back and gives back its
// connection, as database/sql does. ant rolls it back itself, with a context
// the driver can finish on, which keeps go-mssqldb from racing on the
// connection it reuses.
func TestTransactionContextEnds(t *testing.T) {
	each(t, func(t *testing.T, target testdb.Target) {
		db := shared(t, target)
		db.DB().SetMaxOpenConns(1)

		ctx := t.Context()

		t.Run("waiting for a connection", func(t *testing.T) {
			held, err := ant.Begin(ctx, db)
			require.NoError(t, err)

			short, cancel := context.WithTimeout(ctx, 50*time.Millisecond)
			defer cancel()

			err = db.Tx(short, func(*BankDB) error { return nil })
			require.ErrorIs(t, err, context.DeadlineExceeded)
			require.NoError(t, held.Rollback())
		})

		t.Run("while open", func(t *testing.T) {
			request, cancel := context.WithCancel(ctx)

			tx, err := ant.Begin(request, db)
			require.NoError(t, err)
			require.NoError(t, tx.Accounts.Insert(request, &Account{Owner: "cancelled"}))

			cancel()

			require.Eventually(
				t,
				func() bool { return db.DB().Stats().InUse == 0 },
				5*time.Second,
				5*time.Millisecond,
				"the transaction kept its connection",
			)

			require.Error(t, tx.Commit(), "a transaction whose context ended does not commit")

			n, err := db.Accounts.Where(ant.Eq(&A.Owner, "cancelled")).Count(ctx)
			require.NoError(t, err)
			require.Zero(t, n)
		})

		t.Run("before it commits", func(t *testing.T) {
			request, cancel := context.WithCancel(ctx)

			err := db.Tx(request, func(tx *BankDB) error {
				if err := tx.Accounts.Insert(request, &Account{Owner: "late"}); err != nil {
					return err
				}

				cancel()

				return nil
			})
			require.ErrorIs(t, err, context.Canceled)

			n, err := db.Accounts.Where(ant.Eq(&A.Owner, "late")).Count(ctx)
			require.NoError(t, err)
			require.Zero(t, n, "a transaction whose context ended before its commit rolled back")
		})
	})
}

// Begin hands a transaction to other goroutines, which take turns with it, and
// the one that started it ends it.
func TestBeginHandedBetweenGoroutines(t *testing.T) {
	each(t, func(t *testing.T, target testdb.Target) {
		db := shared(t, target)
		ctx := t.Context()

		tx, err := ant.Begin(ctx, db)
		require.NoError(t, err)

		turn := make(chan struct{}, 1)
		turn <- struct{}{}

		errs := workers(6, func(w int) error {
			<-turn

			defer func() { turn <- struct{}{} }()

			return tx.Accounts.Insert(ctx, &Account{Owner: fmt.Sprintf("begin-%d", w)})
		})
		noErrors(t, errs)
		require.NoError(t, tx.Commit())
		require.ErrorIs(t, tx.Commit(), ant.ErrTxDone)

		n, err := db.Accounts.Where(ant.Like(&A.Owner, "begin-%")).Count(ctx)
		require.NoError(t, err)
		require.EqualValues(t, 6, n)
	})
}

// A context on one connection, from UseConn, is shared by goroutines the same
// way: they take turns with it.
func TestUseConnTakingTurns(t *testing.T) {
	each(t, func(t *testing.T, target testdb.Target) {
		db := shared(t, target)
		ctx := t.Context()
		accounts := seedAccounts(t, db, 3, 0)

		conn, err := db.DB().Conn(ctx)
		require.NoError(t, err)

		defer conn.Close()

		one, err := ant.UseConn(db, conn)
		require.NoError(t, err)

		var turn sync.Mutex

		errs := workers(8, func(w int) error {
			a := accounts[w%3]

			turn.Lock()
			defer turn.Unlock()

			got, err := one.Accounts.Include(&A.Payments).Find(ctx, a.ID)
			if err != nil {
				return err
			}

			if got.Owner != a.Owner {
				return fmt.Errorf("read %s", got.Owner)
			}

			return one.Tx(ctx, func(tx *BankDB) error {
				return tx.Payments.Insert(ctx, &Payment{AccountID: a.ID, Amount: 1, Note: "conn"})
			})
		})
		noErrors(t, errs)

		n, err := db.Payments.Count(ctx)
		require.NoError(t, err)
		require.EqualValues(t, 8, n)
	})
}
