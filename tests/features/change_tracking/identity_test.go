package change_tracking

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/skudra-go/ant"
	"github.com/skudra-go/ant/tests/features/fixture"
)

// One context has one instance per row.
func TestIdentity(t *testing.T) {
	fixture.RunTracked(t, func(t *testing.T, db *AppDB) {
		alice, _, _ := fixture.Seed(t, db)
		ctx := t.Context()
		session := ant.Session(db)

		first, err := session.Users.Find(ctx, alice.ID)
		require.NoError(t, err)

		t.Run("Find returns the same instance", func(t *testing.T) {
			again, err := session.Users.Find(ctx, alice.ID)
			require.NoError(t, err)
			require.Same(t, first, again)
		})

		t.Run("queries return it too", func(t *testing.T) {
			users, err := session.Users.Where(ant.Eq(&U.ID, alice.ID)).All(ctx)
			require.NoError(t, err)
			require.Same(t, first, users[0])
		})

		t.Run("unsaved changes survive a later query", func(t *testing.T) {
			first.Age = 70
			again, err := session.Users.Where(ant.Eq(&U.ID, alice.ID)).Single(ctx)
			require.NoError(t, err)
			require.Equal(t, 70, again.Age)
			first.Age = 30
		})

		t.Run("related rows use it", func(t *testing.T) {
			require.NoError(t, session.Posts.InsertOnly(ctx, &Post{UserID: alice.ID, Title: "x"}))
			post, err := session.Posts.IncludeOne(&P.User).First(ctx)
			require.NoError(t, err)
			require.Same(t, first, post.User)
		})

		t.Run("Reload picks up database changes", func(t *testing.T) {
			_, err := session.Users.Where(ant.Eq(&U.ID, alice.ID)).Update(ctx, ant.Set(&U.Age, 41))
			require.NoError(t, err)
			require.Equal(t, 30, first.Age, "a bulk update does not touch tracked entities")
			require.NoError(t, session.Users.Reload(ctx, first))
			require.Equal(t, 41, first.Age)

			require.False(
				t,
				fixture.HasChanges(t, session),
				"the reloaded values are the new baseline",
			)
		})

		t.Run("sessions are separate", func(t *testing.T) {
			other, err := ant.Session(db).Users.Find(ctx, alice.ID)
			require.NoError(t, err)
			require.NotSame(t, first, other)
		})

		t.Run("each row in a relation slice is tracked once", func(t *testing.T) {
			fresh := ant.Session(db)
			owner, err := fresh.Users.Include(&U.Posts).Find(ctx, alice.ID)
			require.NoError(t, err)
			require.Len(t, fixture.Tracked[Post](t, fresh), len(owner.Posts))

			require.Same(
				t,
				&owner.Posts[0],
				fixture.Tracked[Post](t, fresh)[0],
				"the tracked instance is the slice element",
			)
		})

		t.Run("a row in a relation slice is a second instance, and both save", func(t *testing.T) {
			post, err := session.Posts.First(ctx)
			require.NoError(t, err)
			owner, err := session.Users.Include(&U.Posts).Find(ctx, alice.ID)
			require.NoError(t, err)

			post.Title = "from the pointer"
			owner.Posts[0].Published = true

			require.NoError(t, session.SaveChanges(ctx))

			got, err := db.Posts.NoTracking().Find(ctx, post.ID)
			require.NoError(t, err)
			require.Equal(t, "from the pointer", got.Title)
			require.True(t, got.Published)
		})
	})
}
