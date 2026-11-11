package concurrency

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/skudra-go/ant"
	"github.com/skudra-go/ant/internal/testdb"
)

// A tracking context shared by mistake: goroutines hammer it with every kind of
// operation. Each either runs, or is refused with ErrConcurrentUse; none crashes,
// races, or leaves the tracker wrong, and the context works once they stop.
func TestSharedTrackingContext(t *testing.T) {
	each(t, func(t *testing.T, target testdb.Target) {
		db := open(t, target, ant.WithTracking(true))
		ctx := t.Context()
		b := seedBank(t, db, 4)

		var refused, ran atomic.Int64

		ops := []func(w, i int) error{
			func(int, int) error {
				_, err := db.Accounts.Include(&Accounts.Payments).All(ctx)

				return err
			},
			func(w, _ int) error {
				_, err := db.Accounts.Find(ctx, b.accounts[w%4].ID)

				return err
			},
			func(w, _ int) error {
				_, err := db.Payments.IncludeOne(&Payments.Account).
					Where(ant.Eq(&Payments.AccountID, b.accounts[w%4].ID)).All(ctx)

				return err
			},
			func(w, _ int) error {
				return db.Accounts.Load(ctx, &Accounts.Payments, &Account{ID: b.accounts[w%4].ID})
			},
			func(w, i int) error {
				return db.Tickets.Insert(ctx, &Ticket{Title: fmt.Sprintf("hammer %d-%d", w, i)})
			},
			func(w, i int) error {
				_, err := db.Counters.Upsert(
					ctx,
					&Counter{Name: fmt.Sprintf("hammer-%d-%d", w, i), Hits: 1},
					ant.OnConflict(&Counters.Name).DoNothing(),
				)
				if errors.Is(err, ant.ErrUnsupported) {
					return nil
				}

				return err
			},
			func(int, int) error {
				err := db.Tickets.DeleteByID(ctx, int64(-1))
				if errors.Is(err, ant.ErrNotFound) {
					return nil
				}

				return err
			},
			func(w, _ int) error {
				return db.Accounts.Reload(ctx, &Account{ID: b.accounts[w%4].ID})
			},
			func(w, _ int) error {
				// The tracked instance, whose relation other operations load.
				a, err := db.Accounts.Find(ctx, b.accounts[w%4].ID)
				if err != nil {
					return err
				}

				return db.Accounts.LoadMissing(ctx, &Accounts.Payments, a)
			},
			func(int, int) error { return db.SaveChanges(ctx) },
			func(w, i int) error {
				err := db.Tx(ctx, func(tx *BankDB) error {
					if _, err := tx.Accounts.All(ctx); err != nil {
						return err
					}

					if err := tx.Tickets.Insert(
						ctx,
						&Ticket{Title: fmt.Sprintf("tx %d-%d", w, i)},
					); err != nil {
						return err
					}

					if i%2 == 0 {
						return errRollBack
					}

					return nil
				})
				if errors.Is(err, errRollBack) {
					return nil
				}

				return err
			},
			func(int, int) error {
				// Untracked work never claims, so it always runs.
				_, err := db.Accounts.NoTracking().Include(&Accounts.Payments).All(ctx)
				if errors.Is(err, ant.ErrConcurrentUse) {
					return errors.New("an untracked read was refused")
				}

				return err
			},
		}

		errs := workers(12, func(w int) error {
			for i := range 3 * len(ops) {
				err := ops[(w+i)%len(ops)](w, i)

				switch {
				case errors.Is(err, ant.ErrConcurrentUse):
					refused.Add(1)
				case err != nil:
					return fmt.Errorf("op %d: %w", (w+i)%len(ops), err)
				default:
					ran.Add(1)
				}
			}

			return nil
		})
		noErrors(t, errs)
		t.Logf("%d ran, %d refused", ran.Load(), refused.Load())

		// Once the goroutines stop, the context is whole: one instance per row,
		// each relation loaded once, and nothing left to save.
		require.NoError(t, db.SaveChanges(ctx))

		changed, err := db.HasChanges(ctx)
		require.NoError(t, err)
		require.False(t, changed)

		accounts, err := db.Accounts.Include(&Accounts.Payments).OrderBy(&Accounts.ID).All(ctx)
		require.NoError(t, err)
		require.NoError(t, b.checkAll(accounts, 4))

		for _, a := range accounts {
			again, err := db.Accounts.Find(ctx, a.ID)
			require.NoError(t, err)
			require.Same(t, a, again)
		}

		tickets, err := db.Tickets.NoTracking().Count(ctx)
		require.NoError(t, err)

		tracked, err := ant.Tracked[Ticket](ctx, db)
		require.NoError(t, err)

		require.EqualValues(
			t,
			len(tracked),
			tickets,
			"every ticket stored is tracked, and no rolled back one is",
		)
	})
}

// Workers that each build part of a result hand it to the goroutine that owns
// the session, which adds it, and one SaveChanges writes it all. Add claims the
// tracker as any operation does, so workers that called it on the session at
// once would be refused.
func TestWorkersHandEntitiesToOneSession(t *testing.T) {
	each(t, func(t *testing.T, target testdb.Target) {
		db := shared(t, target)
		ctx := t.Context()
		session := ant.Session(db, ant.WithTracking(true))

		const n = 16

		built := make(chan *Account)

		go func() {
			defer close(built)

			workers(n, func(w int) error {
				built <- &Account{
					Owner:    fmt.Sprintf("added-%d", w),
					Payments: []Payment{{Amount: int64(w), Note: "added"}},
				}

				return nil
			})
		}()

		for a := range built {
			require.NoError(t, ant.Add(ctx, session, a))
		}

		tracked, err := ant.Tracked[Account](ctx, session)
		require.NoError(t, err)
		require.Len(t, tracked, n)
		require.NoError(t, session.SaveChanges(ctx))

		accounts, err := db.Accounts.Include(&Accounts.Payments).
			Where(ant.Like(&Accounts.Owner, "added-%")).
			All(ctx)
		require.NoError(t, err)
		require.Len(t, accounts, n)

		for _, a := range accounts {
			require.Len(t, a.Payments, 1, a.Owner)
		}
	})
}

// The helpers claim the tracker as operations do: one called while a load on
// the session runs is refused, unless it is called from that load's hook with
// the hook's context, which shares the load's claim.
func TestHelpersClaimTheTracker(t *testing.T) {
	each(t, func(t *testing.T, target testdb.Target) {
		db := shared(t, target)
		ctx := t.Context()
		seedBank(t, db, 2)
		require.NoError(t, db.Tickets.Insert(ctx, &Ticket{Title: "loaded"}))

		session := ant.Session(db, ant.WithTracking(true))

		loaded, err := session.Accounts.All(ctx)
		require.NoError(t, err)

		a := loaded[0]
		helpers := map[string]func(ctx context.Context) error{
			"Add": func(ctx context.Context) error {
				return ant.Add(ctx, session, &Account{Owner: "added in a hook"})
			},
			"Attach": func(ctx context.Context) error { return ant.Attach(ctx, session, a) },
			"Remove": func(ctx context.Context) error { return ant.Remove(ctx, session, a) },
			"MarkModified": func(ctx context.Context) error {
				return ant.MarkModified(ctx, session, a)
			},
			"Detach": func(ctx context.Context) error { return ant.Detach(ctx, session, a) },
			"TrackGraph": func(ctx context.Context) error {
				return ant.TrackGraph(ctx, session, a, func(ant.GraphNode) ant.State {
					return ant.Unchanged
				})
			},
			"Tracked": func(ctx context.Context) error {
				_, err := ant.Tracked[Account](ctx, session)

				return err
			},
			"Entry": func(ctx context.Context) error {
				_, err := ant.Entry(ctx, session, a)

				return err
			},
			"HasChanges": func(ctx context.Context) error {
				_, err := session.HasChanges(ctx)

				return err
			},
			"DebugTracked": func(ctx context.Context) error {
				_, err := session.DebugTracked(ctx)

				return err
			},
			"ClearTracked": session.ClearTracked,
		}

		for name, helper := range helpers {
			t.Run(name, func(t *testing.T) {
				var during, inside error

				hook := func(ctx context.Context) {
					during = helper(context.WithoutCancel(t.Context()))
					inside = helper(ctx)
				}

				ticketHook.Store(&hook)
				defer ticketHook.Store(nil)

				_, err := session.Tickets.All(ctx)
				require.NoError(t, err)

				require.ErrorIs(
					t,
					during,
					ant.ErrConcurrentUse,
					"another context, while the load runs",
				)

				require.NoError(t, inside, "the hook's context")
			})
		}
	})
}

// A worker pool may share one tracking session if the workers take turns with
// it: then each operation finds the tracker free, and their work adds up.
func TestTrackingSessionTakingTurns(t *testing.T) {
	each(t, func(t *testing.T, target testdb.Target) {
		db := shared(t, target)
		ctx := t.Context()
		b := seedBank(t, db, 4)
		session := ant.Session(db, ant.WithTracking(true))

		var mu sync.Mutex

		errs := workers(12, func(w int) error {
			for i := range 4 {
				err := func() error {
					mu.Lock()
					defer mu.Unlock()

					a, err := session.Accounts.Include(&Accounts.Payments).
						Find(ctx, b.accounts[(w+i)%4].ID)
					if err != nil {
						return err
					}

					a.Balance++
					a.Payments = append(a.Payments, Payment{Amount: 1, Note: "turn"})

					return session.SaveChanges(ctx)
				}()
				if err != nil {
					return err
				}
			}

			return nil
		})
		noErrors(t, errs)

		accounts, err := db.Accounts.Include(&Accounts.Payments).OrderBy(&Accounts.ID).All(ctx)
		require.NoError(t, err)

		for _, a := range accounts {
			require.EqualValues(t, 100+12, a.Balance, a.Owner)
			require.Len(t, a.Payments, 3+12, a.Owner)
		}
	})
}

// A hook runs inside its operation, and goroutines it starts with the hook's
// context share the operation's claim: untracked work from them runs.
func TestHookGoroutinesShareTheClaim(t *testing.T) {
	each(t, func(t *testing.T, target testdb.Target) {
		db := open(t, target, ant.WithTracking(true))
		seedBank(t, db, 2)

		hookDB.Store(db)
		defer hookDB.Store(nil)

		accounts, err := db.Accounts.All(context.WithValue(t.Context(), fanOut{}, true))
		require.NoError(t, err)
		require.Len(t, accounts, 2)

		for _, a := range accounts {
			require.Equal(t, 3, a.counted, a.Owner)
		}
	})
}

// hookDB is the context the fan-out hook queries.
var hookDB atomic.Pointer[BankDB]

type fanOut struct{}

// AfterLoad counts the account's payments from three goroutines at once, with
// the context the hook was given.
func (a *Account) AfterLoad(ctx context.Context) error {
	db := hookDB.Load()
	if db == nil || ctx.Value(fanOut{}) == nil {
		return nil
	}

	counts := make([]int64, 3)

	errs := workers(3, func(i int) error {
		n, err := db.Payments.Where(ant.Eq(&Payments.AccountID, a.ID)).Count(ctx)
		counts[i] = n

		return err
	})

	if err := errors.Join(errs...); err != nil {
		return err
	}

	for _, n := range counts {
		a.counted += int(n)
	}

	a.counted /= 3

	return nil
}
