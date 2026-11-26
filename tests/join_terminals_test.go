package tests

import (
	"bytes"
	"context"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/skudra-go/ant"
	"github.com/skudra-go/ant/internal/testdb"
)

// counted is what a join and a projection share with a query (#169).
type counted interface {
	Count(ctx context.Context) (int64, error)
	Exists(ctx context.Context) (bool, error)
	ToSQL() (string, []any, error)
}

type authorName struct {
	Name string
}

type postsPerAuthor struct {
	Name  string
	Posts int64
}

type joinTotal struct {
	N int64
}

// seedJoinRows makes alice with posts a1 (published) and a2, bob with b1
// (published), and carol with none.
func seedJoinRows(t *testing.T, app *AppDB) {
	t.Helper()

	users := seedUsers(t, app, "alice", 30, "bob", 25, "carol", 40)

	for _, p := range []*Post{
		{UserID: users[0].ID, Title: "a1", Published: true},
		{UserID: users[0].ID, Title: "a2"},
		{UserID: users[1].ID, Title: "b1", Published: true},
	} {
		require.NoError(t, app.Posts.Insert(t.Context(), p))
	}
}

// A join and a projection count, test for rows and show their SQL as a query
// does. Limit, Offset, Distinct and GroupBy decide the rows they count, and
// Distinct over a join whose sides share column names, such as id, works on
// every database (#169).
func TestConformanceJoinAndProjectionTerminals(t *testing.T) {
	testdb.ForEach(t, func(t *testing.T, target testdb.Target) {
		app := openApp(t, target)
		ctx := t.Context()
		seedJoinRows(t, app)

		A := ant.Fields[authorName]()
		P := ant.Fields[postsPerAuthor]()
		T := ant.Fields[joinTotal]()

		names := func(q ant.Selectable) *ant.Projection[authorName] {
			return ant.Project[authorName](q, ant.Into(&A.Name, &Users.Name))
		}

		perAuthor := func(g *ant.Grouped) *ant.Projection[postsPerAuthor] {
			return ant.Project[postsPerAuthor](
				g,
				ant.Into(&P.Name, &Users.Name),
				ant.IntoExpr(&P.Posts, ant.CountOf(&Posts.ID)),
			)
		}

		// An aggregate with no GroupBy makes one row of all of them, even of none.
		total := func(q ant.Selectable) *ant.Projection[joinTotal] {
			return ant.Project[joinTotal](q, ant.IntoExpr(&T.N, ant.CountOf(&Posts.ID)))
		}

		byAuthor := app.Users.LeftJoinMany(&Users.Posts).GroupBy(&Users.Name)
		joined := app.Posts.Join(&Posts.User)
		titles := joined.Select(func(p *Post, u *User) string { return u.Name + "/" + p.Title })

		published := app.Posts.Query().
			FromCTE("published", app.Posts.Where(ant.Eq(&Posts.Published, true))).
			Join(&Posts.User)

		cases := []struct {
			name   string
			q      counted
			count  int64
			exists bool
		}{
			{"join", joined, 3, true},
			{"join where", joined.Where(ant.Eq(&Posts.Published, true)), 2, true},
			{"join where none", joined.Where(ant.Eq(&Posts.Title, "none")), 0, false},
			{"join limit", joined.Limit(2), 2, true},
			{"join offset", joined.Offset(2), 1, true},
			{"join offset past", joined.Offset(3), 0, false},
			{"join distinct", joined.Distinct(), 3, true},
			{"join distinct offset", joined.Distinct().Offset(2), 1, true},
			{"left join", app.Users.LeftJoinMany(&Users.Posts), 4, true},
			{"join of a cte, distinct offset", published.Distinct().Offset(1), 1, true},
			{"join3", joined.JoinMany(&Users.Posts), 5, true},
			{"join3 limit", joined.JoinMany(&Users.Posts).Limit(4), 4, true},
			{"join4", joined.JoinMany(&Users.Posts).Join(&Posts.User), 5, true},
			{"join4 offset", joined.JoinMany(&Users.Posts).Join(&Posts.User).Offset(4), 1, true},
			{"select", titles, 3, true},
			{"select of a limit", joined.Limit(1).Select(postTitle), 1, true},
			{"project", names(joined), 3, true},
			{"project distinct", names(joined.Distinct()), 2, true},
			{"project distinct offset", names(joined.Distinct().Offset(1)), 1, true},
			{"project distinct offset past", names(joined.Distinct().Offset(2)), 0, false},
			{"project of a cte, distinct", names(published.Distinct()), 2, true},
			{
				"project of a cte, distinct offset past",
				names(published.Distinct().Offset(2)),
				0,
				false,
			},
			{"aggregate", total(joined), 1, true},
			{"aggregate of no rows", total(joined.Where(ant.Eq(&Posts.Title, "none"))), 1, true},
			{"aggregate offset past", total(joined.Offset(1)), 0, false},
			{"grouped", perAuthor(byAuthor), 3, true},
			{
				"grouped ordered limit",
				perAuthor(byAuthor.Order(ant.Col(&Users.Name).Desc()).Limit(2)),
				2,
				true,
			},
			{"grouped having", perAuthor(byAuthor.Having(ant.CountOf(&Posts.ID).Gt(1))), 1, true},
			{"grouped limit", perAuthor(byAuthor.Limit(2)), 2, true},
			{"grouped offset past", perAuthor(byAuthor.Offset(3)), 0, false},
		}

		for _, c := range cases {
			t.Run(c.name, func(t *testing.T) {
				n, err := c.q.Count(ctx)
				require.NoError(t, err)
				require.Equal(t, c.count, n)

				found, err := c.q.Exists(ctx)
				require.NoError(t, err)
				require.Equal(t, c.exists, found)

				text, _, err := c.q.ToSQL()
				require.NoError(t, err)
				require.Contains(t, text, "JOIN")
			})
		}

		t.Run("ToSQL is what Select runs", func(t *testing.T) {
			q := joined.Where(ant.Eq(&Users.Name, "bob"))

			text, args, err := q.ToSQL()
			require.NoError(t, err)
			require.Equal(t, []any{"bob"}, args)

			selected, selectedArgs, err := q.Select(postTitle).ToSQL()
			require.NoError(t, err)
			require.Equal(t, text, selected)
			require.Equal(t, args, selectedArgs)

			got, err := q.Select(postTitle).All(ctx)
			require.NoError(t, err)
			require.Equal(t, []string{"b1"}, got)
		})

		t.Run("a field of a model outside the join", func(t *testing.T) {
			type outside struct {
				ant.Base
				Name string
			}

			O := ant.Fields[outside]()
			q := joined.Where(ant.Eq(&O.Name, "x"))

			_, _, err := q.ToSQL()
			require.ErrorIs(t, err, ant.ErrWrongModel)

			_, err = q.Count(ctx)
			require.ErrorIs(t, err, ant.ErrWrongModel)

			_, err = q.Exists(ctx)
			require.ErrorIs(t, err, ant.ErrWrongModel)

			_, _, err = names(q).ToSQL()
			require.ErrorIs(t, err, ant.ErrWrongModel)
		})

		t.Run("a projection with no columns", func(t *testing.T) {
			empty := ant.Project[authorName](joined)

			_, _, err := empty.ToSQL()
			require.ErrorIs(t, err, ant.ErrNoColumns)

			_, err = empty.Count(ctx)
			require.ErrorIs(t, err, ant.ErrNoColumns)

			_, err = empty.Exists(ctx)
			require.ErrorIs(t, err, ant.ErrNoColumns)
		})
	})
}

// Exists on a query with Distinct and Offset skips distinct rows: not the one
// row DISTINCT leaves of a constant, and not every row, as MySQL, MariaDB and
// SQLite read an EXISTS subquery without its DISTINCT (#169).
func TestConformanceExistsDistinctOffset(t *testing.T) {
	testdb.ForEach(t, func(t *testing.T, target testdb.Target) {
		app := openApp(t, target)
		ctx := t.Context()
		seedUsers(t, app, "alice", 30, "bob", 30, "carol", 40)

		ages := app.Users.Column(&Users.Age).Distinct()

		n, err := ages.Count(ctx)
		require.NoError(t, err)
		require.EqualValues(t, 2, n, "two distinct ages")

		found, err := ages.Offset(1).Exists(ctx)
		require.NoError(t, err)
		require.True(t, found, "the second of two distinct ages")

		found, err = ages.Offset(2).Exists(ctx)
		require.NoError(t, err)
		require.False(t, found, "past two distinct ages, though three rows")
	})
}

func postTitle(p *Post, _ *User) string { return p.Title }

// Debug logs every statement a query runs, and a join or a projection keeps it,
// whether it was set before the join or after (#169).
func TestConformanceDebugCarriesThroughJoins(t *testing.T) {
	testdb.Serial(t)

	testdb.ForEach(t, func(t *testing.T, target testdb.Target) {
		app := openApp(t, target)
		ctx := t.Context()
		seedJoinRows(t, app)

		var logs bytes.Buffer

		previous := slog.Default()

		slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
		defer slog.SetDefault(previous)

		A := ant.Fields[authorName]()
		bob := ant.Eq(&Users.Name, "bob")

		runs := []struct {
			name string
			run  func() error
		}{
			{"Debug before the join", func() error {
				_, err := app.Posts.Query().
					Debug().
					Join(&Posts.User).
					Where(bob).
					Select(postTitle).
					All(ctx)

				return err
			}},
			{"Debug on the join", func() error {
				_, err := app.Posts.Join(&Posts.User).Where(bob).Debug().Select(postTitle).All(ctx)

				return err
			}},
			{"Debug on a join3", func() error {
				_, err := app.Posts.Join(&Posts.User).
					JoinMany(&Users.Posts).
					Where(bob).
					Debug().
					Count(ctx)

				return err
			}},
			{"Debug on a join4", func() error {
				_, err := app.Posts.Join(&Posts.User).
					JoinMany(&Users.Posts).
					Join(&Posts.User).
					Where(bob).
					Debug().
					Exists(ctx)

				return err
			}},
			{"Debug on a projection", func() error {
				_, err := ant.Project[authorName](
					app.Posts.Join(&Posts.User).Where(bob),
					ant.Into(&A.Name, &Users.Name),
				).
					Debug().
					First(ctx)

				return err
			}},
			{"Count of a join", func() error {
				_, err := app.Posts.Join(&Posts.User).Where(bob).Debug().Count(ctx)

				return err
			}},
			{"Count of a query", func() error {
				_, err := app.Users.Where(bob).Debug().Count(ctx)

				return err
			}},
			{"Exists of a query", func() error {
				_, err := app.Users.Where(bob).Debug().Exists(ctx)

				return err
			}},
			{"Each of a query", func() error {
				for _, err := range app.Users.Where(bob).Debug().Each(ctx) {
					if err != nil {
						return err
					}
				}

				return nil
			}},
			{"Pluck of a query", func() error {
				_, err := ant.Pluck(ctx, app.Users.Where(bob).Debug(), &Users.Age)

				return err
			}},
			{"SumOf of a query", func() error {
				_, err := ant.SumOf(ctx, app.Users.Where(bob).Debug(), &Users.Age)

				return err
			}},
		}

		for _, r := range runs {
			t.Run(r.name, func(t *testing.T) {
				logs.Reset()
				require.NoError(t, r.run())
				require.Contains(t, logs.String(), "ant: query")
				require.Contains(t, logs.String(), "bob")
			})
		}

		t.Run("no Debug, no log", func(t *testing.T) {
			logs.Reset()

			_, err := app.Posts.Join(&Posts.User).Where(bob).Count(ctx)
			require.NoError(t, err)
			require.Empty(t, logs.String())
		})
	})
}
