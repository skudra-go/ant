package relations

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/skudra-go/ant"
	"github.com/skudra-go/ant/tests/features/fixture"
)

type (
	User = fixture.User
	Post = fixture.Post
)

// byline is what the joins below project each row into.
type byline struct{ Author, Title string }

func toByline(p *Post, u *User) byline { return byline{u.Name, p.Title} }

// Join follows a relation. Select then shapes each joined row in Go.
func TestJoin(t *testing.T) {
	fixture.Run(t, func(t *testing.T, db *fixture.AppDB) {
		seed(t, db)
		ctx := t.Context()

		t.Run("through a relation", func(t *testing.T) {
			got, err := db.Posts.Join(&Posts.User).
				Where(ant.Eq(&Posts.Published, true)).
				OrderBy(&Posts.Title).
				Select(toByline).
				All(ctx)
			require.NoError(t, err)
			require.Equal(t, []byline{{"Bob", "Bob's post"}, {"Alice", "Hello"}}, got)
		})

		t.Run("conditions and ordering on either model", func(t *testing.T) {
			got, err := db.Posts.Join(&Posts.User).
				Where(ant.Eq(&Users.Name, "Alice")).
				OrderByDesc(&Posts.Title).
				Limit(1).
				Select(toByline).
				All(ctx)
			require.NoError(t, err)
			require.Equal(t, []byline{{"Alice", "Hello"}}, got)
		})

		t.Run("Where can come before Join", func(t *testing.T) {
			got, err := db.Posts.Where(ant.Eq(&Users.Name, "Bob")).
				Join(&Posts.User).
				Select(toByline).
				All(ctx)
			require.NoError(t, err)
			require.Len(t, got, 1)
		})

		t.Run("JoinMany gives one result per related row", func(t *testing.T) {
			got, err := db.Users.JoinMany(&Users.Posts).
				OrderBy(&Posts.Title).
				Select(func(u *User, p *Post) string { return u.Name + ":" + p.Title }).
				All(ctx)
			require.NoError(t, err)
			require.Equal(t, []string{"Bob:Bob's post", "Alice:Draft", "Alice:Hello"}, got)
		})

		t.Run("an inner join drops unmatched rows", func(t *testing.T) {
			got, err := db.Users.JoinMany(&Users.Posts).
				Where(ant.Eq(&Users.Name, "Carol")).
				Select(func(u *User, p *Post) string { return u.Name }).
				All(ctx)
			require.NoError(t, err)
			require.Empty(t, got)
		})

		t.Run("a left join keeps them, with a nil side", func(t *testing.T) {
			got, err := db.Users.LeftJoinMany(&Users.Posts).
				Where(ant.Eq(&Users.Name, "Carol")).
				Select(func(u *User, p *Post) bool { return p == nil }).
				All(ctx)
			require.NoError(t, err)
			require.Equal(t, []bool{true}, got)
		})

		t.Run("a NULL column is not an unmatched row", func(t *testing.T) {
			got, err := db.Posts.LeftJoin(&Posts.User).
				Where(ant.Eq(&Posts.Title, "Hello")).
				Select(func(p *Post, u *User) *string { return u.Bio }).
				All(ctx)
			require.NoError(t, err)
			require.Equal(t, []*string{nil}, got)
		})

		t.Run("First", func(t *testing.T) {
			got, err := db.Posts.Join(&Posts.User).OrderBy(&Posts.Title).Select(toByline).First(ctx)
			require.NoError(t, err)
			require.Equal(t, "Bob's post", got.Title)

			_, err = db.Posts.Join(&Posts.User).
				Where(ant.Eq(&Posts.Title, "None")).
				Select(toByline).
				First(ctx)
			require.ErrorIs(t, err, ant.ErrNotFound)
		})

		t.Run("Each streams", func(t *testing.T) {
			var seen []string

			rows := db.Posts.Join(&Posts.User).OrderBy(&Posts.Title).Select(toByline)

			for row, err := range rows.Each(ctx) {
				require.NoError(t, err)

				seen = append(seen, row.Title)
			}

			require.Equal(t, []string{"Bob's post", "Draft", "Hello"}, seen)
		})

		t.Run("a join can be a base for others", func(t *testing.T) {
			base := db.Posts.Join(&Posts.User).OrderBy(&Posts.Title)
			one, err := base.Where(ant.Eq(&Users.Name, "Bob")).Select(toByline).All(ctx)
			require.NoError(t, err)
			require.Len(t, one, 1)

			all, err := base.Select(toByline).All(ctx)
			require.NoError(t, err)
			require.Len(t, all, 3)
		})

		t.Run("a probe outside the join", func(t *testing.T) {
			type other struct{ ID int64 }

			o := ant.Fields[other]()
			_, err := db.Posts.Join(&Posts.User).
				//antvet:ignore another model's field, on purpose: the test checks the error
				Where(ant.Eq(&o.ID, int64(1))).
				Select(toByline).
				All(ctx)
			require.ErrorIs(t, err, ant.ErrWrongModel)
		})
	})
}

// JoinOn joins on any condition, with no relation needed.
func TestJoinOn(t *testing.T) {
	fixture.Run(t, func(t *testing.T, db *fixture.AppDB) {
		seed(t, db)
		ctx := t.Context()
		sameUser := ant.EqCol(&Users.ID, &Posts.UserID)

		pair := func(u *User, p *Post) string {
			switch {
			case u == nil:
				return "?:" + p.Title
			case p == nil:
				return u.Name + ":?"
			}

			return u.Name + ":" + p.Title
		}

		t.Run("inner", func(t *testing.T) {
			got, err := db.Users.JoinOn[Post](
				sameUser,
				ant.Eq(&Posts.Published, true),
			).OrderBy(&Posts.Title).
				Select(pair).
				All(ctx)
			require.NoError(t, err)
			require.Equal(t, []string{"Bob:Bob's post", "Alice:Hello"}, got)
		})

		t.Run("left", func(t *testing.T) {
			got, err := db.Users.LeftJoinOn[Post](
				sameUser,
				ant.Eq(&Posts.Title, "Hello"),
			).OrderBy(&Users.Name).
				Select(pair).
				All(ctx)
			require.NoError(t, err)
			require.Equal(t, []string{"Alice:Hello", "Bob:?", "Carol:?"}, got)
		})

		t.Run("right", func(t *testing.T) {
			got, err := db.Users.RightJoinOn[Post](
				ant.EqCol(&Users.ID, &Posts.UserID),
				ant.Eq(&Users.Name, "Bob"),
			).OrderBy(&Posts.Title).
				Select(pair).
				All(ctx)
			require.NoError(t, err)
			require.Equal(t, []string{"Bob:Bob's post", "?:Draft", "?:Hello"}, got)
		})

		t.Run("full", func(t *testing.T) {
			got, err := db.Users.FullJoinOn[Post](
				sameUser,
				ant.Eq(&Users.Name, "Bob"),
			).Where(ant.Eq(&Posts.Title, "Draft")).
				Select(pair).
				All(ctx)
			require.NoError(t, err)
			require.Equal(t, []string{"?:Draft"}, got)
		})

		t.Run("cross", func(t *testing.T) {
			got, err := db.Users.CrossJoin[Post]().Select(pair).All(ctx)
			require.NoError(t, err)
			require.Len(t, got, 9)
		})

		t.Run("JoinUsing joins on a column both tables have", func(t *testing.T) {
			got, err := db.Users.Query().JoinUsing[Post](
				&Users.ID,
			).Select(func(u *User, p *Post) bool { return true }).
				All(ctx)
			require.NoError(t, err)
			require.NotEmpty(t, got)
		})

		t.Run("a bad condition is reported", func(t *testing.T) {
			x := int64(1)
			//antvet:ignore not a probe, on purpose: the test checks the error
			_, err := db.Users.JoinOn[Post](ant.EqCol(&Users.ID, &x)).Select(pair).All(ctx)
			require.ErrorIs(t, err, ant.ErrNotProbe)
		})
	})
}
