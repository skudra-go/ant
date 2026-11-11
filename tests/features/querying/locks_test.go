package querying

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/skudra-go/ant"
	"github.com/skudra-go/ant/tests/features/fixture"
)

// Locks hold rows until the transaction ends.
func TestLocks(t *testing.T) {
	fixture.Run(t, func(t *testing.T, db *fixture.AppDB) {
		alice, bob, _ := fixture.Seed(t, db)
		ctx := t.Context()
		q := db.Users.OrderBy(&Users.Name)

		t.Run("NoWait fails at once on a locked row", func(t *testing.T) {
			tx, err := ant.Begin(ctx, db)
			require.NoError(t, err)

			defer tx.Rollback()

			_, err = tx.Users.Where(ant.Eq(&Users.ID, alice.ID)).ForUpdate().Single(ctx)
			require.NoError(t, err)

			_, err = q.Where(ant.Eq(&Users.ID, alice.ID)).ForUpdate().NoWait().All(ctx)
			require.ErrorContains(t, err, "could not obtain lock")
		})

		t.Run("SkipLocked leaves locked rows out", func(t *testing.T) {
			tx, err := ant.Begin(ctx, db)
			require.NoError(t, err)

			defer tx.Rollback()

			_, err = tx.Users.Where(ant.Eq(&Users.ID, alice.ID)).ForUpdate().Single(ctx)
			require.NoError(t, err)

			got, err := q.ForUpdate().SkipLocked().All(ctx)
			require.NoError(t, err)
			require.Equal(t, []string{"Bob", "Carol"}, fixture.Names(got))
		})

		t.Run("ForShare allows other readers", func(t *testing.T) {
			tx, err := ant.Begin(ctx, db)
			require.NoError(t, err)

			defer tx.Rollback()

			_, err = tx.Users.Where(ant.Eq(&Users.ID, bob.ID)).ForShare().Single(ctx)
			require.NoError(t, err)

			_, err = q.Where(ant.Eq(&Users.ID, bob.ID)).ForShare().NoWait().All(ctx)
			require.NoError(t, err)
			_, err = q.Where(ant.Eq(&Users.ID, bob.ID)).ForUpdate().NoWait().All(ctx)
			require.Error(t, err, "but not writers")
		})

		// Each worker claims a row nobody else holds, so no job runs twice.
		t.Run("a job queue", func(t *testing.T) {
			const workers = 3

			var (
				wg      sync.WaitGroup
				holding sync.WaitGroup // every worker keeps its lock until all have one
				mu      sync.Mutex
				claimed = map[int64]int{}
			)
			holding.Add(workers)

			for range workers {
				wg.Go(func() {
					_ = db.Tx(ctx, func(tx *fixture.AppDB) error {
						defer holding.Wait()
						defer holding.Done()

						job, err := tx.Users.OrderBy(&Users.ID).ForUpdate().SkipLocked().First(ctx)
						if err != nil {
							return err
						}

						mu.Lock()
						claimed[job.ID]++
						mu.Unlock()

						return nil
					})
				})
			}

			wg.Wait()
			require.Len(t, claimed, workers)
		})
	})
}

// Tag adds a comment to the SQL, so slow queries are easy to find.
func TestTag(t *testing.T) {
	fixture.Run(t, func(t *testing.T, db *fixture.AppDB) {
		_, err := db.Users.Where().Tag("dashboard */ users").All(t.Context())
		require.NoError(t, err, "a comment cannot end early")
		_, err = db.Users.Where().Tag("dashboard /* users").All(t.Context())
		require.NoError(t, err, "or nest and never end")
		_, err = db.Users.Where().Tag("dashboard /*/ users").All(t.Context())
		require.NoError(t, err, "or recombine into a closer")
	})
}
