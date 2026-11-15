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

// Column and Omit choose what the query reads. An include works from the
// column that links the two models, so the query reads that one as well.
func TestIncludeWithChosenColumns(t *testing.T) {
	fixture.Run(t, func(t *testing.T, db *fixture.AppDB) {
		seed(t, db)
		ctx := t.Context()

		counts := func(t *testing.T, users []*User) []int {
			t.Helper()

			out := make([]int, len(users))

			for i, u := range users {
				require.NotEmpty(t, u.Name)
				out[i] = len(u.Posts)
			}

			return out
		}

		t.Run("Column without the key", func(t *testing.T) {
			users, err := db.Users.Column(&Users.Name).
				Include(&Users.Posts).
				OrderBy(&Users.Name).
				All(ctx)
			require.NoError(t, err)
			require.Equal(t, []int{3, 1, 0}, counts(t, users))
			require.Empty(t, users[0].Email, "only the link column is added")
		})

		t.Run("Column after Include", func(t *testing.T) {
			users, err := db.Users.Include(&Users.Posts).
				OrderBy(&Users.Name).
				Column(&Users.Name).
				All(ctx)
			require.NoError(t, err)
			require.Equal(t, []int{3, 1, 0}, counts(t, users))
		})

		t.Run("Omit the key", func(t *testing.T) {
			users, err := db.Users.Omit(&Users.ID).
				Include(&Users.Posts).
				OrderBy(&Users.Name).
				All(ctx)
			require.NoError(t, err)
			require.Equal(t, []int{3, 1, 0}, counts(t, users))
		})

		t.Run("First, Page and Chunk", func(t *testing.T) {
			q := db.Users.Column(&Users.Name).Include(&Users.Posts).OrderBy(&Users.Name)

			first, err := q.First(ctx)
			require.NoError(t, err)
			require.Len(t, first.Posts, 3)

			page, err := q.Page(ctx, 1, 2)
			require.NoError(t, err)
			require.Equal(t, []int{3, 1}, counts(t, page.Items))

			require.NoError(t, q.Chunk(ctx, 1, func(users []*User) error {
				require.NotNil(t, users[0].Posts)

				if users[0].Name == "Alice" {
					require.Len(t, users[0].Posts, 3)
				}

				return nil
			}))
		})

		t.Run("IncludeOne without the foreign key", func(t *testing.T) {
			posts, err := db.Posts.Omit(&Posts.UserID).
				IncludeOne(&Posts.User).
				OrderBy(&Posts.Title).
				All(ctx)
			require.NoError(t, err)
			require.Equal(t, "Alice", posts[0].User.Name)
			require.Equal(t, "Bob", posts[3].User.Name)

			posts, err = db.Posts.Column(&Posts.Title).
				IncludeOne(&Posts.User).
				OrderBy(&Posts.Title).
				All(ctx)
			require.NoError(t, err)
			require.Equal(t, "Bob", posts[3].User.Name)
		})

		t.Run("in a nested include", func(t *testing.T) {
			users, err := db.Users.Include(&Users.Posts, func(q *ant.Query[Post]) *ant.Query[Post] {
				return q.Column(&Posts.Title).IncludeOne(&Posts.User)
			}).
				Where(ant.Eq(&Users.Name, "Bob")).
				All(ctx)
			require.NoError(t, err)
			require.Len(t, users[0].Posts, 1)
			require.Equal(t, "Bob", users[0].Posts[0].User.Name)
		})
	})
}
