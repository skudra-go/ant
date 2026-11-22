package relations

import (
	"database/sql"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/skudra-go/ant"
	"github.com/skudra-go/ant/tests/features/fixture"
)

type Summary struct {
	Author string
	Title  string
}

var Summaries = ant.Fields[Summary]()

// Project selects only the mapped columns, where Select fetches whole rows.
func TestProject(t *testing.T) {
	fixture.Run(t, func(t *testing.T, db *fixture.AppDB) {
		seed(t, db)
		ctx := t.Context()

		t.Run("from a join", func(t *testing.T) {
			got, err := ant.Project[Summary](
				db.Posts.Join(&Posts.User).
					Where(ant.Eq(&Posts.Published, true)).
					OrderBy(&Posts.Title),
				ant.Into(&Summaries.Author, &Users.Name),
				ant.Into(&Summaries.Title, &Posts.Title),
			).All(ctx)
			require.NoError(t, err)
			require.Equal(t, []Summary{{"Bob", "Bob's post"}, {"Alice", "Hello"}}, got)
		})

		t.Run("from one table", func(t *testing.T) {
			got, err := ant.Project[Summary](
				db.Users.Where(ant.Eq(&Users.Name, "Bob")),
				ant.Into(&Summaries.Author, &Users.Name),
			).First(ctx)
			require.NoError(t, err)
			require.Equal(t, Summary{Author: "Bob"}, *got)
		})

		t.Run("an expression", func(t *testing.T) {
			got, err := ant.Project[Summary](
				db.Users.Where(ant.Eq(&Users.Name, "Bob")),
				ant.IntoExpr(
					&Summaries.Author,
					ant.SQL[string]("upper(?)", ant.Col(&Users.Name)),
				),
			).First(ctx)
			require.NoError(t, err)
			require.Equal(t, "BOB", got.Author)
		})

		t.Run("no mappings", func(t *testing.T) {
			_, err := ant.Project[Summary](db.Users.Where()).All(ctx)
			require.ErrorIs(t, err, ant.ErrNoColumns)
		})

		t.Run("dst must be a probe of the result type", func(t *testing.T) {
			_, err := ant.Project[Summary](
				db.Users.Where(),
				ant.Into(&Users.Name, &Users.Name),
			).All(ctx)
			require.ErrorIs(t, err, ant.ErrWrongModel)
		})

		t.Run("src must be in the query", func(t *testing.T) {
			_, err := ant.Project[Summary](
				db.Users.Where(),
				ant.Into(&Summaries.Title, &Posts.Title),
			).All(ctx)
			require.ErrorIs(t, err, ant.ErrWrongModel)
		})

		t.Run("a NULL needs a nullable field", func(t *testing.T) {
			type row struct{ Bio sql.Null[string] }

			R := ant.Fields[row]()
			got, err := ant.Project[row](
				db.Users.Where(ant.Eq(&Users.Name, "Bob")),
				ant.IntoExpr(
					&R.Bio,
					ant.SQL[sql.Null[string]]("?", ant.Col(&Users.Bio)),
				),
			).First(ctx)
			require.NoError(t, err)
			require.False(t, got.Bio.Valid)
		})
	})
}

// Shelf is a user with their posts' newest title and number, if they have any.
type Shelf struct {
	Author string
	Title  string          // "" for an author with no post
	Newest sql.Null[int64] // not valid for an author with no post
	Count  int64
}

var Shelves = ant.Fields[Shelf]()

// A left join's side, and an aggregate over no rows, are NULL. A plain field
// reads it as its zero value, as MaxOf does, and ant.Nullable keeps it, to tell
// "no post" from a stored zero.
func TestProjectNull(t *testing.T) {
	fixture.Run(t, func(t *testing.T, db *fixture.AppDB) {
		alice, _, _ := fixture.Seed(t, db)
		ctx := t.Context()
		post := &fixture.Post{UserID: alice.ID, Title: "Hello"}
		require.NoError(t, db.Posts.Insert(ctx, post))

		got, err := ant.Project[Shelf](
			db.Users.Where(ant.In(&Users.Name, "Alice", "Bob")).
				LeftJoinMany(&Users.Posts).
				GroupBy(&Users.Name),
			ant.Into(&Shelves.Author, &Users.Name),
			ant.IntoExpr(&Shelves.Title, ant.Max(&Posts.Title)),
			ant.IntoExpr(&Shelves.Newest, ant.Nullable(ant.Max(&Posts.ID))),
			ant.IntoExpr(&Shelves.Count, ant.CountOf(&Posts.ID)),
		).
			All(ctx)
		require.NoError(t, err)

		require.ElementsMatch(
			t,
			[]Shelf{
				{
					Author: "Alice",
					Title:  "Hello",
					Newest: sql.Null[int64]{V: post.ID, Valid: true},
					Count:  1,
				},
				{Author: "Bob"},
			},
			got,
		)
	})
}
