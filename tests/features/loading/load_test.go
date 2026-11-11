package loading

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/skudra-go/ant"
	"github.com/skudra-go/ant/tests/features/fixture"
)

// Load fills relations on entities you already have.
func TestLoad(t *testing.T) {
	fixture.Run(t, func(t *testing.T, db *fixture.AppDB) {
		seed(t, db)
		ctx := t.Context()

		t.Run("several entities in one query", func(t *testing.T) {
			users, err := db.Users.NoTracking().OrderBy(&Users.Name).All(ctx)
			require.NoError(t, err)
			require.NoError(t, db.Users.Load(ctx, &Users.Posts, users...))
			require.Len(t, users[0].Posts, 3)
			require.Empty(t, users[2].Posts)
		})

		t.Run("one entity", func(t *testing.T) {
			bob, err := db.Users.NoTracking().Where(ant.Eq(&Users.Name, "Bob")).Single(ctx)
			require.NoError(t, err)
			require.NoError(t, db.Users.Load(ctx, &Users.Posts, bob))
			require.Len(t, bob.Posts, 1)
		})

		t.Run("a many-to-one relation", func(t *testing.T) {
			post, err := db.Posts.NoTracking().Where(ant.Eq(&Posts.Title, "B1")).Single(ctx)
			require.NoError(t, err)
			require.NoError(t, db.Posts.LoadOne(ctx, &Posts.User, post))
			require.Equal(t, "Bob", post.User.Name)
		})

		t.Run("a slice of values", func(t *testing.T) {
			users, err := ant.Raw[User](db, `SELECT * FROM users ORDER BY name`).All(ctx)
			require.NoError(t, err)
			require.NoError(t, db.Users.Load(ctx, &Users.Posts, ant.Pointers(users)...))
			require.Len(t, users[0].Posts, 3)
		})

		t.Run("loading again replaces the rows", func(t *testing.T) {
			bob, err := db.Users.NoTracking().
				Include(&Users.Posts).
				Where(ant.Eq(&Users.Name, "Bob")).
				Single(ctx)
			require.NoError(t, err)
			require.NoError(t, db.Users.Load(ctx, &Users.Posts, bob))
			require.Len(t, bob.Posts, 1)
		})

		t.Run("nothing to load", func(t *testing.T) {
			require.NoError(t, db.Users.Load(ctx, &Users.Posts))
		})

		t.Run("a nil entity", func(t *testing.T) {
			require.ErrorIs(t, db.Users.Load(ctx, &Users.Posts, nil), ant.ErrNilEntity)
		})
	})
}

// LoadMissing skips entities whose relation is already loaded.
func TestLoadMissing(t *testing.T) {
	fixture.Run(t, func(t *testing.T, db *fixture.AppDB) {
		seed(t, db)
		ctx := t.Context()
		users, err := db.Users.NoTracking().OrderBy(&Users.Name).All(ctx)
		require.NoError(t, err)

		kept := []Post{{Title: "Kept"}}
		users[0].Posts = kept

		require.NoError(t, db.Users.LoadMissing(ctx, &Users.Posts, users...))
		require.Equal(t, []string{"Kept"}, titles(users[0].Posts))
		require.Equal(t, []string{"B1"}, titles(users[1].Posts))
	})
}

// Related starts a query from one entity's relation.
func TestRelated(t *testing.T) {
	fixture.Run(t, func(t *testing.T, db *fixture.AppDB) {
		alice, _, carol := seed(t, db)
		ctx := t.Context()

		t.Run("a query you can refine", func(t *testing.T) {
			posts, err := ant.Related(db, alice, &Users.Posts).
				Where(ant.Eq(&Posts.Published, false)).
				OrderBy(&Posts.Title).
				All(ctx)
			require.NoError(t, err)
			require.Len(t, posts, 2)
			require.Equal(t, "A2", posts[0].Title)
		})

		t.Run("none", func(t *testing.T) {
			posts, err := ant.Related(db, carol, &Users.Posts).All(ctx)
			require.NoError(t, err)
			require.Empty(t, posts)
		})

		t.Run("many-to-one", func(t *testing.T) {
			post, err := db.Posts.Where(ant.Eq(&Posts.Title, "A1")).Single(ctx)
			require.NoError(t, err)
			author, err := ant.RelatedOne(db, post, &Posts.User).Single(ctx)
			require.NoError(t, err)
			require.Equal(t, "Alice", author.Name)
		})

		t.Run("it counts as a condition for bulk writes", func(t *testing.T) {
			n, err := ant.Related(db, carol, &Users.Posts).Delete(ctx)
			require.NoError(t, err)
			require.Zero(t, n)
		})

		t.Run("a nil entity", func(t *testing.T) {
			_, err := ant.Related[User, Post](db, nil, &Users.Posts).All(ctx)
			require.ErrorIs(t, err, ant.ErrNilEntity)
		})

		t.Run("a relation of another model", func(t *testing.T) {
			_, err := ant.RelatedOne(db, alice, &Posts.User).All(ctx)
			require.ErrorIs(t, err, ant.ErrWrongModel)
		})
	})
}
