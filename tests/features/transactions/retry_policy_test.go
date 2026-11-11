package transactions

import (
	"context"
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
