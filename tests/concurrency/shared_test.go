package concurrency

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/skudra-go/ant"
	"github.com/skudra-go/ant/internal/testdb"
)

// bank is seeded data the workers check what they read against.
type bank struct {
	accounts []*Account
	payments map[int64][]int64 // amounts by account, ascending
}

// seedBank inserts n accounts, each with balance 100 and three payments.
func seedBank(t *testing.T, db *BankDB, n int) *bank {
	t.Helper()

	b := &bank{accounts: seedAccounts(t, db, n, 100), payments: map[int64][]int64{}}

	var payments []*Payment

	for i, a := range b.accounts {
		for j := range 3 {
			amount := int64(i*10 + j + 1)
			payments = append(payments, &Payment{AccountID: a.ID, Amount: amount, Note: "seed"})
			b.payments[a.ID] = append(b.payments[a.ID], amount)
		}
	}

	_, err := db.Payments.InsertMany(t.Context(), payments)
	require.NoError(t, err)

	return b
}

// amounts returns the amounts of an account's loaded payments, ascending.
func amounts(a *Account) []int64 {
	out := make([]int64, len(a.Payments))

	for i, p := range a.Payments {
		out[i] = p.Amount
	}

	slices.Sort(out)

	return out
}

// check returns an error unless what was read matches what was seeded.
func (b *bank) check(a *Account) error {
	for _, want := range b.accounts {
		if want.ID != a.ID {
			continue
		}

		if a.Owner != want.Owner || a.Balance != want.Balance {
			return fmt.Errorf("account %d read as %s %d", a.ID, a.Owner, a.Balance)
		}

		if a.Payments != nil && !slices.Equal(amounts(a), b.payments[a.ID]) {
			return fmt.Errorf(
				"account %d has payments %v, want %v",
				a.ID,
				amounts(a),
				b.payments[a.ID],
			)
		}

		for _, p := range a.Payments {
			if p.AccountID != a.ID || p.Account != nil && p.Account != a {
				return fmt.Errorf("payment %d of account %d points elsewhere", p.ID, a.ID)
			}
		}

		return nil
	}

	return fmt.Errorf("account %d was never seeded", a.ID)
}

func (b *bank) checkAll(accounts []*Account, want int) error {
	if len(accounts) != want {
		return fmt.Errorf("read %d accounts, want %d", len(accounts), want)
	}

	seen := map[int64]bool{}

	for _, a := range accounts {
		if seen[a.ID] {
			return fmt.Errorf("account %d read twice", a.ID)
		}

		seen[a.ID] = true

		if err := b.check(a); err != nil {
			return err
		}
	}

	return nil
}

// reads are the ways a request reads, each checking what it got.
func reads(db *BankDB, b *bank) []func(ctx context.Context, i int) error {
	n := len(b.accounts)

	return []func(ctx context.Context, i int) error{
		func(ctx context.Context, _ int) error { // All
			got, err := db.Accounts.OrderBy(&A.ID).All(ctx)
			if err != nil {
				return err
			}

			return b.checkAll(got, n)
		},
		func(ctx context.Context, i int) error { // Find
			want := b.accounts[i%n]

			got, err := db.Accounts.Find(ctx, want.ID)
			if err != nil {
				return err
			}

			return b.check(got)
		},
		func(ctx context.Context, i int) error { // Where and Single
			want := b.accounts[i%n]

			got, err := db.Accounts.Where(ant.Eq(&A.Owner, want.Owner)).Single(ctx)
			if err != nil {
				return err
			}

			return b.check(got)
		},
		func(ctx context.Context, _ int) error { // Count and Exists
			count, err := db.Accounts.Count(ctx)
			if err != nil {
				return err
			}

			found, err := db.Payments.Where(ant.Eq(&P.Note, "seed")).Exists(ctx)
			if err != nil {
				return err
			}

			if count != int64(n) || !found {
				return fmt.Errorf("count %d, exists %v", count, found)
			}

			return nil
		},
		func(ctx context.Context, i int) error { // Page
			page, err := db.Accounts.OrderBy(&A.ID).Page(ctx, i%3+1, 4)
			if err != nil {
				return err
			}

			if page.Total != int64(n) {
				return fmt.Errorf("page total %d", page.Total)
			}

			for _, a := range page.Items {
				if err := b.check(a); err != nil {
					return err
				}
			}

			return nil
		},
		func(ctx context.Context, _ int) error { // After, page by page
			var all []*Account

			cursor := ""

			for {
				page, err := db.Accounts.OrderBy(&A.ID).After(ctx, cursor, 5)
				if err != nil {
					return err
				}

				all = append(all, page.Items...)

				if cursor = page.Next; cursor == "" {
					break
				}
			}

			return b.checkAll(all, n)
		},
		func(ctx context.Context, _ int) error { // Chunk
			var all []*Account

			err := db.Accounts.OrderBy(&A.ID).Chunk(ctx, 5, func(batch []*Account) error {
				all = append(all, batch...)

				return nil
			})
			if err != nil {
				return err
			}

			return b.checkAll(all, n)
		},
		func(ctx context.Context, _ int) error { // Each
			var all []*Account

			for a, err := range db.Accounts.Each(ctx) {
				if err != nil {
					return err
				}

				all = append(all, &a)
			}

			return b.checkAll(all, n)
		},
		func(ctx context.Context, _ int) error { // Include
			got, err := db.Accounts.Include(&A.Payments).OrderBy(&A.ID).All(ctx)
			if err != nil {
				return err
			}

			for _, a := range got {
				if len(a.Payments) != 3 {
					return fmt.Errorf("account %d has %d payments", a.ID, len(a.Payments))
				}
			}

			return b.checkAll(got, n)
		},
		func(ctx context.Context, i int) error { // IncludeOne and IncludeJoined
			want := b.accounts[i%n]

			for _, q := range []*ant.Query[Payment]{
				db.Payments.IncludeOne(&P.Account),
				db.Payments.IncludeJoined(&P.Account),
			} {
				got, err := q.Where(ant.Eq(&P.AccountID, want.ID)).All(ctx)
				if err != nil {
					return err
				}

				if len(got) != 3 {
					return fmt.Errorf("account %d has %d payments", want.ID, len(got))
				}

				for _, p := range got {
					if p.Account == nil || p.Account.ID != want.ID {
						return fmt.Errorf("payment %d has the wrong account", p.ID)
					}

					if err := b.check(p.Account); err != nil {
						return err
					}
				}
			}

			return nil
		},
		func(ctx context.Context, i int) error { // Load on entities of its own
			want := b.accounts[i%n]
			own := &Account{ID: want.ID, Owner: want.Owner, Balance: want.Balance}

			if err := db.Accounts.Load(ctx, &A.Payments, own, own); err != nil {
				return err
			}

			return b.check(own)
		},
		func(ctx context.Context, _ int) error { // aggregates and Pluck
			sum, err := ant.SumOf(ctx, db.Payments.Query(), &P.Amount)
			if err != nil {
				return err
			}

			var want int64

			for _, amounts := range b.payments {
				for _, a := range amounts {
					want += a
				}
			}

			owners, err := ant.Pluck(ctx, db.Accounts.OrderBy(&A.ID), &A.Owner)
			if err != nil {
				return err
			}

			if sum != want || len(owners) != n {
				return fmt.Errorf("sum %d, want %d, %d owners", sum, want, len(owners))
			}

			return nil
		},
		func(ctx context.Context, i int) error { // Raw
			want := b.accounts[i%n]

			got, err := ant.Raw[Account](db, "SELECT * FROM cc_accounts WHERE id = ?", want.ID).
				First(ctx)
			if err != nil {
				return err
			}

			return b.check(got)
		},
		func(ctx context.Context, i int) error { // a join projected to a value
			want := b.accounts[i%n]

			type row struct {
				Owner  string
				Amount int64
			}

			got, err := db.Payments.Join(&P.Account).
				Where(ant.Eq(&P.AccountID, want.ID)).
				Select(func(p *Payment, a *Account) row { return row{a.Owner, p.Amount} }).
				All(ctx)
			if err != nil {
				return err
			}

			if len(got) != 3 || got[0].Owner != want.Owner {
				return fmt.Errorf("joined %v", got)
			}

			return nil
		},
	}
}

// A context with tracking off is what a server shares between requests: every
// read may run on any goroutine at once, and each gets rows of its own.
func TestSharedContextReads(t *testing.T) {
	each(t, func(t *testing.T, target testdb.Target) {
		db := shared(t, target)
		b := seedBank(t, db, 12)
		ops := reads(db, b)

		errs := workers(24, func(w int) error {
			for i := range 2 * len(ops) {
				if err := ops[(w+i)%len(ops)](t.Context(), w+i); err != nil {
					return fmt.Errorf("read %d: %w", (w+i)%len(ops), err)
				}
			}

			return nil
		})
		noErrors(t, errs)
	})
}

// A query is a value: one built once, at startup, may be the base of queries on
// any number of goroutines, and none of them changes it.
func TestSharedBaseQuery(t *testing.T) {
	each(t, func(t *testing.T, target testdb.Target) {
		db := shared(t, target)
		b := seedBank(t, db, 12)
		ctx := t.Context()

		base := db.Accounts.Where(ant.Gt(&A.Balance, int64(0))).Include(&A.Payments)
		text, args, err := base.ToSQL()
		require.NoError(t, err)

		errs := workers(24, func(w int) error {
			for i := range 10 {
				want := b.accounts[(w+i)%len(b.accounts)]

				q := base.Where(ant.Eq(&A.ID, want.ID))

				if w%2 == 0 {
					q = q.OrderBy(&A.Owner).Limit(5)
				} else {
					q = q.OrderByDesc(&A.ID).Tag(fmt.Sprintf("worker %d", w))
				}

				got, err := q.All(ctx)
				if err != nil {
					return err
				}

				if err := b.checkAll(got, 1); err != nil {
					return err
				}
			}

			return nil
		})
		noErrors(t, errs)

		after, afterArgs, err := base.ToSQL()
		require.NoError(t, err)
		require.Equal(t, text, after, "the base query changed")
		require.Equal(t, args, afterArgs)

		got, err := base.All(ctx)
		require.NoError(t, err)
		require.NoError(t, b.checkAll(got, len(b.accounts)))
	})
}

// With tracking off, each read returns entities of its own, so the goroutines
// that read the same row can each change what they got.
func TestEntitiesAreNotShared(t *testing.T) {
	each(t, func(t *testing.T, target testdb.Target) {
		db := shared(t, target)
		b := seedBank(t, db, 2)
		ctx := t.Context()
		id := b.accounts[0].ID

		errs := workers(16, func(w int) error {
			for i := range 10 {
				var (
					got *Account
					err error
				)

				switch i % 3 {
				case 0:
					got, err = db.Accounts.Find(ctx, id)
				case 1:
					got, err = db.Accounts.Query().NoTrackingResolved().Include(&A.Payments).
						Where(ant.Eq(&A.ID, id)).First(ctx)
				default:
					var payments []*Payment

					payments, err = db.Payments.IncludeJoined(&P.Account).
						Where(ant.Eq(&P.AccountID, id)).All(ctx)
					if err == nil {
						got = payments[0].Account
					}
				}

				if err != nil {
					return err
				}

				if got.Balance != 100 {
					return fmt.Errorf("balance %d: another worker's change showed", got.Balance)
				}

				// Each worker writes to what it read, which the race detector
				// reports if another worker was given the same value.
				got.Balance = int64(w)
				got.Owner = fmt.Sprint(w)

				for k := range got.Payments {
					got.Payments[k].Note = fmt.Sprint(w)
				}
			}

			return nil
		})
		noErrors(t, errs)
	})
}

// Writes from many goroutines at once each land once, and leave the rows what
// the writes together say.
func TestSharedContextWrites(t *testing.T) {
	each(t, func(t *testing.T, target testdb.Target) {
		db := shared(t, target)
		ctx := t.Context()

		const n, rounds = 12, 8

		errs := workers(n, func(w int) error {
			account := &Account{Owner: fmt.Sprintf("writer-%d", w), Balance: 0}
			if err := db.Accounts.Insert(ctx, account); err != nil {
				return err
			}

			for i := range rounds {
				p := &Payment{AccountID: account.ID, Amount: int64(i + 1), Note: "in"}
				if err := db.Payments.Insert(ctx, p); err != nil {
					return err
				}

				if p.ID == 0 {
					return errors.New("insert gave no key")
				}

				changed, err := db.Accounts.Where(ant.Eq(&A.ID, account.ID)).
					Update(ctx, ant.Inc(&A.Balance, int64(i+1)))
				if err != nil {
					return err
				}

				if changed != 1 {
					return fmt.Errorf("update changed %d rows", changed)
				}
			}

			many := make([]*Payment, rounds)
			for i := range many {
				many[i] = &Payment{AccountID: account.ID, Amount: 1000, Note: "bulk"}
			}

			if _, err := db.Payments.InsertMany(ctx, many); err != nil {
				return err
			}

			gone, err := db.Payments.Where(
				ant.Eq(&P.AccountID, account.ID),
				ant.Eq(&P.Note, "bulk"),
			).Delete(ctx)
			if err != nil {
				return err
			}

			if gone != rounds {
				return fmt.Errorf("deleted %d bulk payments", gone)
			}

			return nil
		})
		noErrors(t, errs)

		accounts, err := db.Accounts.Include(&A.Payments).All(ctx)
		require.NoError(t, err)
		require.Len(t, accounts, n)

		for _, a := range accounts {
			require.EqualValues(t, rounds*(rounds+1)/2, a.Balance, a.Owner)
			require.Equal(t, []int64{1, 2, 3, 4, 5, 6, 7, 8}, amounts(a), a.Owner)
		}
	})
}

// A pool smaller than the number of workers makes them wait for a connection,
// never deadlock, even when each runs a transaction, an include, a chunked read
// that queries between batches, or SaveChanges in a session.
func TestSmallPool(t *testing.T) {
	each(t, func(t *testing.T, target testdb.Target) {
		for _, size := range []int{1, 2} {
			t.Run(fmt.Sprintf("%d connections", size), func(t *testing.T) {
				create(t, target)

				db := shared(t, target)
				db.DB().SetMaxOpenConns(size)
				b := seedBank(t, db, 6)

				ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
				defer cancel()

				ops := reads(db, b)

				errs := workers(12, func(w int) error {
					if err := ops[w%len(ops)](ctx, w); err != nil {
						return err
					}

					switch w % 4 {
					case 0:
						return db.Tx(ctx, func(tx *BankDB) error {
							_, err := tx.Payments.Where(ant.Eq(&P.Note, "nothing")).Delete(ctx)

							return err
						})
					case 1:
						session := ant.Session(db, ant.WithTracking(true))

						a, err := session.Accounts.Include(&A.Payments).
							Find(ctx, b.accounts[w%6].ID)
						if err != nil {
							return err
						}

						// The session writes to the counters, which the reads
						// leave alone: the accounts must stay as seeded.
						counter := &Counter{
							Name: fmt.Sprintf("pool-%d", w),
							Hits: int64(len(a.Payments)),
						}

						if err := ant.Add(ctx, session, counter); err != nil {
							return err
						}

						if err := session.SaveChanges(ctx); err != nil {
							return err
						}

						counter.Hits++

						return session.SaveChanges(ctx)
					case 2:
						return db.Accounts.OrderBy(&A.ID).
							Chunk(ctx, 2, func(batch []*Account) error {
								_, err := db.Payments.Where(ant.Eq(&P.AccountID, batch[0].ID)).
									Count(ctx)

								return err
							})
					default:
						_, err := db.Accounts.Where(ant.Eq(&A.ID, b.accounts[0].ID)).
							Update(ctx, ant.Inc(&A.Balance, int64(0)))

						return err
					}
				})
				noErrors(t, errs)

				require.NoError(
					t,
					ctx.Err(),
					"the workers took too long, likely waiting on each other",
				)
			})
		}
	})
}

// A request whose context is cancelled stops, gives its connection back, and
// takes back its transaction whole. The others go on.
func TestCancelledRequests(t *testing.T) {
	each(t, func(t *testing.T, target testdb.Target) {
		db := shared(t, target)
		b := seedBank(t, db, 4)
		ops := reads(db, b)

		var (
			mu        sync.Mutex
			committed []string
			cancelled atomic.Int64
		)

		errs := workers(16, func(w int) error {
			for i := range 6 {
				ctx, cancel := context.WithCancel(t.Context())

				if (w+i)%2 == 0 {
					// Cancelled at some point while it runs.
					time.AfterFunc(time.Duration(w*i%7)*time.Millisecond, cancel)
				}

				owner := fmt.Sprintf("cancel-%d-%d", w, i)

				err := db.Tx(ctx, func(tx *BankDB) error {
					a := &Account{Owner: owner}
					if err := tx.Accounts.Insert(ctx, a); err != nil {
						return err
					}

					return tx.Payments.Insert(
						ctx,
						&Payment{AccountID: a.ID, Amount: 1, Note: "pair"},
					)
				})
				if err == nil {
					mu.Lock()

					committed = append(committed, owner)
					mu.Unlock()
				}

				readErr := ops[(w+i)%len(ops)](ctx, w)

				if ctx.Err() != nil {
					cancelled.Add(1)
				}

				cancel()

				for _, err := range []error{err, readErr} {
					if err != nil && ctx.Err() == nil {
						return err
					}
				}
			}

			return nil
		})
		noErrors(t, errs)
		require.Positive(t, cancelled.Load(), "no request was cancelled while it ran")

		// A transaction that reported a commit left both of its rows. One that
		// failed may still have committed, if it was cancelled as the commit
		// went out, but never left one row without the other.
		accounts, err := db.Accounts.Where(ant.Like(&A.Owner, "cancel-%")).
			Include(&A.Payments).All(t.Context())
		require.NoError(t, err)

		owners := map[string]bool{}

		for _, a := range accounts {
			owners[a.Owner] = true

			require.Len(t, a.Payments, 1, a.Owner)
		}

		for _, owner := range committed {
			require.True(t, owners[owner], "%s committed, but its rows are gone", owner)
		}

		require.Eventually(
			t,
			func() bool { return db.DB().Stats().InUse == 0 },
			5*time.Second,
			10*time.Millisecond,
			"a cancelled request kept its connection",
		)
	})
}

// Sessions made at once from one context each get a tracker of their own.
func TestSessionsMadeAtOnce(t *testing.T) {
	each(t, func(t *testing.T, target testdb.Target) {
		db := shared(t, target)
		b := seedBank(t, db, 4)
		ctx := t.Context()
		id := b.accounts[0].ID

		var mu sync.Mutex

		seen := map[*Account]bool{}

		errs := workers(24, func(w int) error {
			session := ant.Session(db, ant.WithTracking(true))

			first, err := session.Accounts.Find(ctx, id)
			if err != nil {
				return err
			}

			again, err := session.Accounts.Include(&A.Payments).Where(ant.Eq(&A.ID, id)).First(ctx)
			if err != nil {
				return err
			}

			if first != again {
				return errors.New("a session gave two instances of one row")
			}

			if len(again.Payments) != 3 {
				return fmt.Errorf("%d payments", len(again.Payments))
			}

			mu.Lock()
			defer mu.Unlock()

			if seen[first] {
				return errors.New("two sessions share an instance")
			}

			seen[first] = true

			return nil
		})
		noErrors(t, errs)
	})
}
