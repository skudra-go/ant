package change_tracking

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/skudra-go/ant"
	"github.com/skudra-go/ant/tests/features/fixture"
)

func newUser(name string) *User {
	return &User{Name: name, Email: name + "@example.com", Age: 20}
}

// Add, Remove, Attach, and MarkModified set what the next SaveChanges does.
func TestStates(t *testing.T) {
	fixture.RunTracked(t, func(t *testing.T, db *AppDB) {
		ctx := t.Context()

		t.Run("Add inserts", func(t *testing.T) {
			session := ant.Session(db)
			user := newUser("added")
			require.NoError(t, ant.Add(ctx, session, user))
			require.Equal(t, ant.Added, fixture.Entry(t, session, user).State())
			require.True(t, fixture.HasChanges(t, session))

			require.NoError(t, session.SaveChanges(ctx))
			require.NotZero(t, user.ID)
			require.Equal(t, ant.Unchanged, fixture.Entry(t, session, user).State())

			found, err := session.Users.Find(ctx, user.ID)
			require.NoError(t, err)
			require.Same(t, user, found, "a saved entity joins the identity map")
		})

		t.Run("Insert tracks what it writes", func(t *testing.T) {
			session := ant.Session(db)
			user := newUser("inserted")
			require.NoError(t, session.Users.Insert(ctx, user))
			user.Age = 21

			require.NoError(t, session.SaveChanges(ctx))
			require.Equal(t, 21, stored(t, db, user.ID).Age)
		})

		t.Run("Remove deletes", func(t *testing.T) {
			session := ant.Session(db)
			user := newUser("removed")
			require.NoError(t, session.Users.Insert(ctx, user))
			require.NoError(t, ant.Remove(ctx, session, user))
			require.Equal(t, ant.Deleted, fixture.Entry(t, session, user).State())

			require.NoError(t, session.SaveChanges(ctx))
			_, err := db.Users.NoTracking().Find(ctx, user.ID)
			require.ErrorIs(t, err, ant.ErrNotFound)
			require.Equal(t, ant.Detached, fixture.Entry(t, session, user).State())
		})

		t.Run("Attach tracks without writing", func(t *testing.T) {
			user := newUser("attached")
			require.NoError(t, ant.Session(db).Users.Insert(ctx, user))

			session := ant.Session(db)

			disconnected := &User{
				Base:  ant.Base{ID: user.ID},
				Name:  "attached",
				Email: user.Email,
				Age:   20,
			}
			require.NoError(t, ant.Attach(ctx, session, disconnected))
			require.False(t, fixture.HasChanges(t, session))

			disconnected.Age = 22

			require.NoError(t, session.SaveChanges(ctx))
			require.Equal(t, 22, stored(t, db, user.ID).Age)
		})

		t.Run("MarkModified writes every field", func(t *testing.T) {
			user := newUser("marked")
			require.NoError(t, ant.Session(db).Users.Insert(ctx, user))

			session := ant.Session(db)
			disconnected := *user
			disconnected.Name = "renamed"
			require.NoError(t, ant.MarkModified(ctx, session, &disconnected))
			require.NoError(t, session.SaveChanges(ctx))
			require.Equal(t, "renamed", stored(t, db, user.ID).Name)
		})

		// A struct from a request body has no UpdatedAt, so a forced update
		// stamps it, as any update does, rather than saving the zero time.
		t.Run("MarkModified stamps UpdatedAt", func(t *testing.T) {
			user := newUser("stamped")
			require.NoError(t, ant.Session(db).Users.Insert(ctx, user))

			session := ant.Session(db)
			body := &User{Name: "from a body", Email: user.Email, Age: 21}
			body.ID = user.ID
			require.NoError(t, ant.MarkModified(ctx, session, body))
			require.NoError(t, session.SaveChanges(ctx))

			got := stored(t, db, user.ID)
			require.Equal(t, "from a body", got.Name)
			require.False(t, got.UpdatedAt.IsZero())
			require.False(t, got.UpdatedAt.Before(user.UpdatedAt))
			require.False(t, body.UpdatedAt.IsZero(), "the entity holds the stamp too")
			require.True(t, got.CreatedAt.Equal(user.CreatedAt), "CreatedAt is never updated")
		})

		t.Run("TrackGraph Modified stamps UpdatedAt", func(t *testing.T) {
			user := newUser("graphed")
			require.NoError(t, ant.Session(db).Users.Insert(ctx, user))

			session := ant.Session(db)
			body := &User{Name: "graph body", Email: user.Email, Age: 22}
			body.ID = user.ID

			require.NoError(t, ant.TrackGraph(ctx, session, body, func(ant.GraphNode) ant.State {
				return ant.Modified
			}))

			require.NoError(t, session.SaveChanges(ctx))

			got := stored(t, db, user.ID)
			require.Equal(t, "graph body", got.Name)
			require.False(t, got.UpdatedAt.IsZero())
		})

		t.Run("Detach forgets changes", func(t *testing.T) {
			session := ant.Session(db)
			user := newUser("detached")
			require.NoError(t, session.Users.Insert(ctx, user))
			user.Age = 99
			require.NoError(t, ant.Detach(ctx, session, user))
			require.False(t, fixture.HasChanges(t, session))
			require.NoError(t, session.SaveChanges(ctx))
			require.Equal(t, 20, stored(t, db, user.ID).Age)
		})

		t.Run("ClearTracked forgets everything", func(t *testing.T) {
			session := ant.Session(db)
			user := newUser("cleared")
			require.NoError(t, session.Users.Insert(ctx, user))
			user.Age = 99

			require.NoError(t, session.ClearTracked(ctx))
			require.False(t, fixture.HasChanges(t, session))
			require.Empty(t, fixture.Tracked[User](t, session))
		})

		t.Run("marking needs a key and an entity", func(t *testing.T) {
			type keyless struct{ Name string }

			session := ant.Session(db)
			require.ErrorIs(t, ant.Add(ctx, session, &keyless{}), ant.ErrNoKey)
			require.ErrorIs(t, ant.Remove[User](ctx, session, nil), ant.ErrNilEntity)
			require.ErrorIs(t, ant.Add(ctx, &AppDB{}, newUser("x")), ant.ErrNotOpen)
		})
	})
}

// Entry shows what the tracker knows about one entity.
func TestEntry(t *testing.T) {
	fixture.RunTracked(t, func(t *testing.T, db *AppDB) {
		_, bob, _ := fixture.Seed(t, db)

		t.Run("after a change", func(t *testing.T) {
			bob.Age = 18
			entry := fixture.Entry(t, db, bob)
			require.Equal(t, ant.Modified, entry.State())
			require.Equal(t, []string{"Age"}, entry.Changed())
			require.True(t, ant.IsDirty(entry, &U.Age))
			require.False(t, ant.IsDirty(entry, &U.Name))

			original, err := ant.Original(entry, &U.Age)
			require.NoError(t, err)
			require.Equal(t, 17, original)
		})

		t.Run("Tracked lists a model", func(t *testing.T) {
			require.Len(t, fixture.Tracked[User](t, db), 3)
		})

		t.Run("DebugTracked describes it", func(t *testing.T) {
			require.Contains(t, fixture.DebugTracked(t, db), "User")
			require.Contains(t, fixture.DebugTracked(t, db), "Modified Age")
		})

		t.Run("an untracked entity", func(t *testing.T) {
			entry := fixture.Entry(t, db, &User{})
			require.Equal(t, ant.Detached, entry.State())
			require.Empty(t, entry.Changed())
			_, err := ant.Original(entry, &U.Age)
			require.ErrorIs(t, err, ant.ErrNotTracked)
		})
	})
}

// A key identifies the row, so changing it is refused rather than writing to
// whichever row has the new key.
func TestChangedKey(t *testing.T) {
	fixture.RunTracked(t, func(t *testing.T, db *AppDB) {
		alice, bob, _ := fixture.Seed(t, db)
		ctx := t.Context()
		before := stored(t, db, bob.ID).Age

		alice.ID = bob.ID
		alice.Age = 99

		require.ErrorIs(t, db.SaveChanges(ctx), ant.ErrKeyChanged)
		require.Equal(t, before, stored(t, db, bob.ID).Age)
	})
}

// A row that vanished is an error, and nothing else is saved.
func TestConcurrency(t *testing.T) {
	fixture.RunTracked(t, func(t *testing.T, db *AppDB) {
		alice, bob, _ := fixture.Seed(t, db)
		ctx := t.Context()

		_, err := db.Users.Where(ant.Eq(&U.ID, bob.ID)).Delete(ctx)
		require.NoError(t, err)

		alice.Age = 44
		bob.Age = 45

		require.ErrorIs(t, db.SaveChanges(ctx), ant.ErrConcurrency)
		require.Equal(t, 30, stored(t, db, alice.ID).Age, "the whole save rolled back")
		require.True(t, fixture.HasChanges(t, db), "the changes are still pending")

		require.NoError(t, ant.Detach(ctx, db, bob))
		require.NoError(t, db.SaveChanges(ctx), "a retry can succeed")
		require.Equal(t, 44, stored(t, db, alice.ID).Age)
	})
}

// A failed save leaves entities ready to save again.
func TestRetryAfterFailure(t *testing.T) {
	fixture.RunTracked(t, func(t *testing.T, db *AppDB) {
		ctx := t.Context()
		session := ant.Session(db)
		first, clash := newUser("first"), &User{Name: "clash", Email: "first@example.com"}
		require.NoError(t, ant.Add(ctx, session, first))
		require.NoError(t, ant.Add(ctx, session, clash))

		require.ErrorContains(t, session.SaveChanges(ctx), "duplicate key")
		require.Zero(t, first.ID, "keys from the rolled back insert are cleared")

		clash.Email = "fixed@example.com"

		require.NoError(t, session.SaveChanges(ctx))
		require.NotZero(t, first.ID)
		require.NotZero(t, clash.ID)
	})
}
