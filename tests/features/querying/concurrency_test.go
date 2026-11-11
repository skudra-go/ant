package querying

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/skudra-go/ant"
	"github.com/skudra-go/ant/tests/features/fixture"
)

// A context and a built query can be shared between goroutines.
func TestConcurrentUse(t *testing.T) {
	fixture.Run(t, func(t *testing.T, db *fixture.AppDB) {
		fixture.Seed(t, db)
		ctx := t.Context()
		adults := db.Users.Where(ant.Gt(&Users.Age, 18))

		var wg sync.WaitGroup

		errs := make(chan error, 40)

		for range 20 {
			wg.Go(func() {
				got, err := adults.OrderBy(&Users.Name).All(ctx)
				if err == nil && len(got) != 2 {
					t.Errorf("got %d rows", len(got))
				}

				errs <- err
			})

			wg.Go(func() {
				_, err := adults.Where(ant.Eq(&Users.Name, "Alice")).Single(ctx)
				errs <- err
			})
		}

		wg.Wait()
		close(errs)

		for err := range errs {
			require.NoError(t, err)
		}
	})
}
