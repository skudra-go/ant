package change_tracking

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/skudra-go/ant"
	"github.com/skudra-go/ant/tests/features/fixture"
)

// Change what you loaded, then SaveChanges writes only the differences.
func TestSaveChanges(t *testing.T) {
	fixture.RunTracked(t, func(t *testing.T, db *AppDB) {
		alice, bob, carol := fixture.Seed(t, db)
		ctx := t.Context()

		t.Run("a field", func(t *testing.T) {
			user, err := db.Users.Find(ctx, alice.ID)
			require.NoError(t, err)

			user.Age++

			require.NoError(t, db.SaveChanges(ctx))
			require.Equal(t, 31, stored(t, db, alice.ID).Age)
		})

		t.Run("zero values are real values", func(t *testing.T) {
			bob.Age = 0
			bob.Email = ""

			require.NoError(t, db.SaveChanges(ctx))
			got := stored(t, db, bob.ID)
			require.Zero(t, got.Age)
			require.Empty(t, got.Email)
		})

		t.Run("a pointer set to nil writes NULL", func(t *testing.T) {
			carol.Bio = nil

			require.NoError(t, db.SaveChanges(ctx))
			require.Nil(t, stored(t, db, carol.ID).Bio)
		})

		t.Run("a write through a pointer is seen", func(t *testing.T) {
			alice.Bio = new("first")

			require.NoError(t, db.SaveChanges(ctx))

			*alice.Bio = "second"

			require.NoError(t, db.SaveChanges(ctx))
			require.Equal(t, "second", *stored(t, db, alice.ID).Bio)
		})

		t.Run("changes through range work, because All returns pointers", func(t *testing.T) {
			users, err := db.Users.All(ctx)
			require.NoError(t, err)

			for _, u := range users {
				u.Age = 50
			}

			require.NoError(t, db.SaveChanges(ctx))
			require.Equal(t, 50, stored(t, db, bob.ID).Age)
		})

		t.Run("only changed columns are written", func(t *testing.T) {
			// Another writer changes a column this context also loaded.
			_, err := db.Users.Where(ant.Eq(&U.ID, alice.ID)).Update(ctx, ant.Set(&U.Name, "Ally"))
			require.NoError(t, err)

			alice.Age = 51

			require.NoError(t, db.SaveChanges(ctx))
			got := stored(t, db, alice.ID)
			require.Equal(t, "Ally", got.Name, "SaveChanges must not write the stale name back")
			require.Equal(t, 51, got.Age)
		})

		t.Run("nothing to save is a no-op", func(t *testing.T) {
			require.False(t, fixture.HasChanges(t, db))
			require.NoError(t, db.SaveChanges(ctx))
		})

		t.Run("saving twice writes once", func(t *testing.T) {
			alice.Age = 60

			require.True(t, fixture.HasChanges(t, db))
			require.NoError(t, db.SaveChanges(ctx))
			require.False(t, fixture.HasChanges(t, db))
		})
	})
}

// NoTracking opts one query out. Changes to its results are not saved.
func TestNoTracking(t *testing.T) {
	fixture.RunTracked(t, func(t *testing.T, db *AppDB) {
		fixture.Seed(t, db)
		ctx := t.Context()
		session := ant.Session(db)

		user, err := session.Users.NoTracking().Where(ant.Eq(&U.Name, "Bob")).Single(ctx)
		require.NoError(t, err)

		user.Age = 99

		require.NoError(t, session.SaveChanges(ctx))
		require.Equal(t, 17, stored(t, db, user.ID).Age)
		require.Equal(t, ant.Detached, fixture.Entry(t, session, user).State())
	})
}

// The order of NoTracking and Include does not matter.
func TestNoTrackingCoversIncludes(t *testing.T) {
	fixture.RunTracked(t, func(t *testing.T, db *AppDB) {
		_, _ = loaded(t, db)
		ctx := t.Context()

		for name, q := range map[string]func(*AppDB) *ant.Query[User]{
			"NoTracking first": func(s *AppDB) *ant.Query[User] {
				return s.Users.NoTracking().Include(&U.Posts)
			},
			"NoTracking last": func(s *AppDB) *ant.Query[User] {
				return s.Users.Include(&U.Posts).NoTracking()
			},
		} {
			t.Run(name, func(t *testing.T) {
				session := ant.Session(db)
				users, err := q(session).Where(ant.Eq(&U.Name, "Alice")).All(ctx)
				require.NoError(t, err)
				require.NotEmpty(t, users[0].Posts)
				require.Empty(t, fixture.Tracked[Post](t, session))
			})
		}
	})
}

// A partial row is never tracked, because its zero fields are not real values.
func TestPartialRowsAreNotTracked(t *testing.T) {
	fixture.RunTracked(t, func(t *testing.T, db *AppDB) {
		fixture.Seed(t, db)
		ctx := t.Context()
		session := ant.Session(db)

		partial, err := session.Users.Where(ant.Eq(&U.Name, "Carol")).
			Column(&U.ID).
			Column(&U.Name).
			Single(ctx)
		require.NoError(t, err)
		require.Equal(t, ant.Detached, fixture.Entry(t, session, partial).State())

		full, err := session.Users.Find(ctx, partial.ID)
		require.NoError(t, err)
		require.Equal(t, 40, full.Age, "the partial row must not stand in for the full one")
	})
}
