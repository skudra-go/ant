package loading

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/skudra-go/ant"
	"github.com/skudra-go/ant/tests/features/fixture"
)

// Include loads a one-to-many relation along with the query.
func TestInclude(t *testing.T) {
	fixture.Run(t, func(t *testing.T, db *fixture.AppDB) {
		seed(t, db)
		ctx := t.Context()

		t.Run("every parent gets its rows", func(t *testing.T) {
			users, err := db.Users.Include(&Users.Posts).OrderBy(&Users.Name).All(ctx)
			require.NoError(t, err)
			require.ElementsMatch(t, []string{"A1", "A2", "A3"}, titles(users[0].Posts))
			require.Equal(t, []string{"B1"}, titles(users[1].Posts))
		})

		t.Run("none is an empty slice, not nil", func(t *testing.T) {
			carol, err := db.Users.Include(&Users.Posts).
				Where(ant.Eq(&Users.Name, "Carol")).
				Single(ctx)
			require.NoError(t, err)
			require.NotNil(t, carol.Posts)
			require.Empty(t, carol.Posts)
		})

		t.Run("nil means not loaded", func(t *testing.T) {
			carol, err := db.Users.Where(ant.Eq(&Users.Name, "Carol")).Single(ctx)
			require.NoError(t, err)
			require.Nil(t, carol.Posts)
		})

		t.Run("works with First, Single, and Find", func(t *testing.T) {
			first, err := db.Users.Include(&Users.Posts).
				Where(ant.Eq(&Users.Name, "Bob")).
				First(ctx)
			require.NoError(t, err)
			require.Len(t, first.Posts, 1)

			found, err := db.Users.Include(&Users.Posts).Find(ctx, first.ID)
			require.NoError(t, err)
			require.Len(t, found.Posts, 1)
		})

		t.Run("no parents runs no extra query", func(t *testing.T) {
			users, err := db.Users.Include(&Users.Posts).
				Where(ant.Eq(&Users.Name, "Nobody")).
				All(ctx)
			require.NoError(t, err)
			require.Empty(t, users)
		})

		t.Run("Each refuses, because rows are still streaming", func(t *testing.T) {
			for _, err := range db.Users.Include(&Users.Posts).Each(ctx) {
				require.ErrorIs(t, err, ant.ErrIncludeInEach)
			}
		})
	})
}

// IncludeOne loads a many-to-one or one-to-one relation.
func TestIncludeOne(t *testing.T) {
	fixture.Run(t, func(t *testing.T, db *fixture.AppDB) {
		seed(t, db)
		ctx := t.Context()

		t.Run("every row gets its parent", func(t *testing.T) {
			posts, err := db.Posts.IncludeOne(&Posts.User).OrderBy(&Posts.Title).All(ctx)
			require.NoError(t, err)
			require.Len(t, posts, 4)
			require.Equal(t, "Alice", posts[0].User.Name)
			require.Equal(t, "Bob", posts[3].User.Name)
		})

		t.Run("rows with one parent share it", func(t *testing.T) {
			posts, err := db.Posts.IncludeOne(&Posts.User).
				Where(ant.Like(&Posts.Title, "A%")).
				All(ctx)
			require.NoError(t, err)
			require.Same(t, posts[0].User, posts[1].User)
		})

		t.Run("a filter that excludes the parent leaves nil", func(t *testing.T) {
			post, err := db.Posts.Where(ant.Eq(&Posts.Title, "B1")).
				IncludeOne(&Posts.User, func(q *ant.Query[User]) *ant.Query[User] {
					return q.Where(ant.Eq(&Users.Name, "Alice"))
				}).Single(ctx)
			require.NoError(t, err)
			require.Nil(t, post.User)
		})
	})
}
