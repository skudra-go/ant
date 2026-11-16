package transactions

import (
	"context"
	"database/sql"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/skudra-go/ant"
	"github.com/skudra-go/ant/tests/features/fixture"
)

// WithRetry applies to every Tx and SaveChanges, for an application that wants
// serialization conflicts handled in one place.
func TestRetryPolicy(t *testing.T) {
	fixture.Each(t, func(t *testing.T, target fixture.Target) {
		ctx := context.Background()
		db := fixture.Open[AppDB](t, target, ant.WithRetry(ant.Retry{Attempts: 20}))
		alice := user("Alice")
		require.NoError(t, db.Users.Insert(ctx, alice))

		// Concurrent read-modify-write at serializable isolation: PostgreSQL cancels
		// all but one, and without retries most would fail.
		var wg sync.WaitGroup

		errs := make(chan error, 8)

		for range 8 {
			wg.Go(func() {
				errs <- db.Tx(ctx, func(tx *AppDB) error {
					if _, err := ant.Raw[any](tx, "SET TRANSACTION ISOLATION LEVEL SERIALIZABLE").Exec(ctx); err != nil {
						return err
					}

					stored, err := tx.Users.Find(ctx, alice.ID)
					if err != nil {
						return err
					}

					_, err = tx.Users.Where(ant.Eq(&Users.ID, alice.ID)).
						Update(ctx, ant.Set(&Users.Age, stored.Age+1))

					return err
				})
			})
		}

		wg.Wait()
		close(errs)

		for err := range errs {
			require.NoError(t, err)
		}

		stored, err := db.Users.Find(ctx, alice.ID)
		require.NoError(t, err)
		require.Equal(t, 8, stored.Age, "no update was lost")
	})
}

// The policy is for every Tx, whatever else the call asks for. Only Attempts
// replaces it, for that call.
func TestRetryPolicyWithTxOptions(t *testing.T) {
	fixture.Each(t, func(t *testing.T, target fixture.Target) {
		ctx := context.Background()
		db := fixture.Open[AppDB](t, target, ant.WithRetry(ant.Retry{Attempts: 4}))

		runs := func(options ...ant.TxOption) int {
			n := 0

			err := db.Tx(
				ctx,
				func(*AppDB) error {
					n++

					return conflict{}
				},
				options...,
			)
			require.ErrorAs(t, err, &conflict{})

			return n
		}

		require.Equal(t, 4, runs())
		require.Equal(t, 4, runs(ant.Isolation(sql.LevelSerializable)))
		require.Equal(t, 4, runs(ant.ReadOnly()))
		require.Equal(t, 4, runs(ant.ReadOnly(), ant.Isolation(sql.LevelRepeatableRead)))
		require.Equal(t, 2, runs(ant.Attempts(2)))
		require.Equal(t, 2, runs(ant.Attempts(2), ant.ReadOnly()))
		require.Equal(t, 1, runs(ant.Attempts(1)), "one call can opt out")
		require.Equal(t, 1, runs(ant.Attempts(0)), "f always runs")

		plain := fixture.Open[AppDB](t, target)

		n := 0

		require.Error(t, plain.Tx(
			ctx,
			func(*AppDB) error {
				n++

				return conflict{}
			},
			ant.ReadOnly(),
		))

		require.Equal(t, 1, n, "no policy, no retry")
	})
}

// conflicting fails every statement as a serialization failure would, and then
// calls cancel, so the wait before the next attempt is cut short.
type conflicting struct{ cancel func() }

func (c conflicting) Before(ctx context.Context, _ *ant.Statement) (context.Context, error) {
	c.cancel()

	return ctx, conflict{}
}

func (conflicting) After(context.Context, *ant.Statement, ant.Outcome) {}

// A retry that the context cuts short reports what it was retrying as well as
// the context's error, so the conflict is not lost in a timeout.
func TestRetryCutShortKeepsTheFailure(t *testing.T) {
	fixture.Each(t, func(t *testing.T, target fixture.Target) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		db := fixture.Open[AppDB](
			t,
			target,
			ant.WithRetry(ant.Retry{Attempts: 5}),
			ant.WithInterceptor(conflicting{cancel}),
		)

		require.NoError(t, ant.Add(ctx, db, user("Cut short")))

		err := db.SaveChanges(ctx)
		require.ErrorIs(t, err, context.Canceled)
		require.ErrorAs(t, err, &conflict{}, "SaveChanges")
		require.True(t, ant.IsRetryable(err))
	})
}

// Tx did so already, and goes on doing it.
func TestTxRetryCutShortKeepsTheFailure(t *testing.T) {
	fixture.Each(t, func(t *testing.T, target fixture.Target) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		plain := fixture.Open[AppDB](t, target, ant.WithRetry(ant.Retry{Attempts: 5}))

		err := plain.Tx(ctx, func(*AppDB) error {
			cancel()

			return conflict{}
		})
		require.ErrorIs(t, err, context.Canceled)
		require.ErrorAs(t, err, &conflict{}, "Tx")
	})
}
