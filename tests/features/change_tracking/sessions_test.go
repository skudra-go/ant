package change_tracking

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/skudra-go/ant"
	"github.com/skudra-go/ant/tests/features/fixture"
)

// A session is a cheap context with its own tracker, for one unit of work such
// as one web request.
func TestSessions(t *testing.T) {
	fixture.RunTracked(t, func(t *testing.T, db *AppDB) {
		alice, _, _ := fixture.Seed(t, db)
		ctx := t.Context()

		t.Run("each saves only its own changes", func(t *testing.T) {
			one, two := ant.Session(db), ant.Session(db)
			a, err := one.Users.Find(ctx, alice.ID)
			require.NoError(t, err)

			a.Age = 31
			b, err := two.Users.Find(ctx, alice.ID)
			require.NoError(t, err)

			b.Name = "Ally"

			require.NoError(t, two.SaveChanges(ctx))
			require.Equal(t, 30, stored(t, db, alice.ID).Age, "one has not saved")
			require.NoError(t, one.SaveChanges(ctx))

			got := stored(t, db, alice.ID)
			require.Equal(t, 31, got.Age)
			require.Equal(t, "Ally", got.Name)
		})

		t.Run("many at once", func(t *testing.T) {
			var wg sync.WaitGroup

			errs := make(chan error, 20)

			for i := range 20 {
				wg.Go(func() {
					session := ant.Session(db)

					user := &User{Name: "worker", Email: string(rune('a'+i)) + "@w.example"}
					if err := session.Users.Insert(ctx, user); err != nil {
						errs <- err

						return
					}

					user.Age = i

					errs <- session.SaveChanges(ctx)
				})
			}

			wg.Wait()
			close(errs)

			for err := range errs {
				require.NoError(t, err)
			}
		})

		t.Run(
			"closing a session closes the shared pool, so close the root only",
			func(t *testing.T) {
				require.Same(t, db.DB(), ant.Session(db).DB())
			},
		)
	})
}

// The context Open returns does not track, so handlers can share it: nothing
// grows, and no request sees another's edits. Saving from it is a mistake, so
// SaveChanges and the helpers that give it work say so rather than save nothing.
func TestTrackingOff(t *testing.T) {
	fixture.Run(t, func(t *testing.T, db *AppDB) {
		alice, _, _ := fixture.Seed(t, db)
		ctx := t.Context()
		user, err := db.Users.Find(ctx, alice.ID)
		require.NoError(t, err)

		user.Age = 99

		require.False(t, fixture.HasChanges(t, db))
		require.Empty(t, fixture.Tracked[User](t, db))
		require.ErrorIs(t, db.SaveChanges(ctx), ant.ErrNoTracking)
		require.ErrorIs(t, ant.Add(ctx, db, &User{Name: "new"}), ant.ErrNoTracking)
		require.ErrorIs(t, ant.Remove(ctx, db, user), ant.ErrNoTracking)
		require.Equal(t, 30, stored(t, db, alice.ID).Age)

		// Direct writes need no tracker.
		_, err = db.Users.Where(ant.Eq(&Users.ID, alice.ID)).Update(ctx, ant.Set(&Users.Age, 99))
		require.NoError(t, err)
		require.Equal(t, 99, stored(t, db, alice.ID).Age)
	})
}

// A session is the unit of work: it tracks, whatever the context it came from
// does, so a service handed the shared context can always save through one.
func TestSessionWithItsOwnTracking(t *testing.T) {
	fixture.Run(t, func(t *testing.T, db *fixture.AppDB) {
		ctx := t.Context()
		alice, _, _ := fixture.Seed(t, db)

		t.Run("a session tracks on a context that does not", func(t *testing.T) {
			session := ant.Session(db)
			user, err := session.Users.Find(ctx, alice.ID)
			require.NoError(t, err)

			user.Age = 31

			require.NoError(t, session.SaveChanges(ctx))
			require.Equal(t, 31, stored(t, db, alice.ID).Age)
		})

		t.Run("WithTracking(false) turns it off for this session alone", func(t *testing.T) {
			session := ant.Session(db, ant.WithTracking(false))
			user, err := session.Users.Find(ctx, alice.ID)
			require.NoError(t, err)

			user.Age = 50

			require.ErrorIs(t, session.SaveChanges(ctx), ant.ErrNoTracking)
			require.Equal(t, 31, stored(t, db, alice.ID).Age)
		})
	})
}
