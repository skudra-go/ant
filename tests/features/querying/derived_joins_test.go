package querying

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/skudra-go/ant"
	"github.com/skudra-go/ant/tests/features/fixture"
)

// Pair is what the joins below return.
type Pair struct{ Name, Title string }

// JoinSub joins the results of a query, so they can be narrowed first. Probes of
// the joined model refer to its columns.
func TestJoinSub(t *testing.T) {
	fixture.Run(t, func(t *testing.T, db *fixture.AppDB) {
		ctx := t.Context()
		alice, bob, _ := fixture.Seed(t, db)
		require.NoError(t, db.Posts.Insert(ctx,
			&fixture.Post{UserID: alice.ID, Title: "live", Published: true},
			&fixture.Post{UserID: alice.ID, Title: "draft"},
			&fixture.Post{UserID: bob.ID, Title: "other", Published: true}))

		published := db.Posts.Where(ant.Eq(&Posts.Published, true))
		pairs, err := ant.JoinSub(
			db.Users.OrderBy(&Users.Name),
			published,
			ant.EqCol(&Users.ID, &Posts.UserID),
		).
			Select(func(u *fixture.User, p *fixture.Post) Pair { return Pair{u.Name, p.Title} }).
			All(ctx)
		require.NoError(t, err)
		require.Equal(t, []Pair{{"Alice", "live"}, {"Bob", "other"}}, pairs)

		t.Run("a left join keeps rows with no match", func(t *testing.T) {
			pairs, err := ant.LeftJoinSub(
				db.Users.Where(ant.Eq(&Users.Name, "Carol")),
				published,
				ant.EqCol(&Users.ID, &Posts.UserID),
			).
				Select(func(u *fixture.User, p *fixture.Post) Pair {
					require.Nil(t, p)

					return Pair{u.Name, ""}
				}).
				All(ctx)
			require.NoError(t, err)
			require.Equal(t, []Pair{{"Carol", ""}}, pairs)
		})
	})
}

// A lateral join runs once per row, so it can read the newest rows per parent,
// which a plain join cannot.
func TestJoinLateral(t *testing.T) {
	fixture.Run(t, func(t *testing.T, db *fixture.AppDB) {
		ctx := t.Context()
		alice, bob, _ := fixture.Seed(t, db)

		for _, title := range []string{"a1", "a2", "a3"} {
			require.NoError(t, db.Posts.Insert(ctx, &fixture.Post{UserID: alice.ID, Title: title}))
		}

		require.NoError(t, db.Posts.Insert(ctx, &fixture.Post{UserID: bob.ID, Title: "b1"}))

		newest := db.Posts.Where(ant.EqCol(&Posts.UserID, &Users.ID)).
			OrderByDesc(&Posts.ID).
			Limit(2)
		pairs, err := ant.JoinLateral(db.Users.OrderBy(&Users.Name), newest).
			Select(func(u *fixture.User, p *fixture.Post) Pair { return Pair{u.Name, p.Title} }).
			All(ctx)
		require.NoError(t, err)
		require.Equal(t, []Pair{{"Alice", "a3"}, {"Alice", "a2"}, {"Bob", "b1"}}, pairs)

		t.Run("a left lateral join keeps rows with none", func(t *testing.T) {
			pairs, err := ant.LeftJoinLateral(db.Users.Where(ant.Eq(&Users.Name, "Carol")), newest).
				Select(func(u *fixture.User, p *fixture.Post) Pair { return Pair{Name: u.Name} }).
				All(ctx)
			require.NoError(t, err)
			require.Equal(t, []Pair{{"Carol", ""}}, pairs)
		})
	})
}

// Wanted is a row held in memory.
type Wanted struct {
	UserID int64
	Label  string
}

// Wanteds is Wanted's probe.
var Wanteds = ant.Fields[Wanted]()

// Values treats rows in memory as a table, to join against a list.
func TestValues(t *testing.T) {
	fixture.Run(t, func(t *testing.T, db *fixture.AppDB) {
		ctx := t.Context()
		alice, bob, _ := fixture.Seed(t, db)
		wanted := ant.Values(db, Wanted{alice.ID, "first"}, Wanted{bob.ID, "second"})

		labels, err := wanted.OrderBy(&Wanteds.Label).All(ctx)
		require.NoError(t, err)
		require.Len(t, labels, 2)
		require.Equal(t, "first", labels[0].Label)

		pairs, err := ant.JoinSub(db.Users.OrderBy(&Users.Name), wanted, ant.EqCol(&Users.ID, &Wanteds.UserID)).
			Select(func(u *fixture.User, w *Wanted) Pair { return Pair{u.Name, w.Label} }).
			All(ctx)
		require.NoError(t, err)
		require.Equal(t, []Pair{{"Alice", "first"}, {"Bob", "second"}}, pairs)

		t.Run("no rows is an error, because SQL has no empty VALUES", func(t *testing.T) {
			_, err := ant.Values[Wanted](db).All(ctx)
			require.ErrorIs(t, err, ant.ErrNoRows)
		})

		// A bulk write finds the rows by key, so the key must be among the columns
		// Values writes, and a writeonly one is not.
		t.Run("Update and Delete find the rows by key", func(t *testing.T) {
			_, err := ant.Values(db, Token{ID: 1, Name: "a"}).
				Where(ant.Eq(&Tokens.Name, "a")).
				Delete(ctx)
			require.ErrorContains(t, err, "need it to select Token.ID")

			n, err := ant.Values(db, *bob).Where(ant.Eq(&Users.Name, "Bob")).Delete(ctx)
			require.NoError(t, err)
			require.EqualValues(t, 1, n)

			left, err := db.Users.OrderBy(&Users.Name).All(ctx)
			require.NoError(t, err)
			require.Equal(t, []string{"Alice", "Carol"}, fixture.Names(left))
		})
	})
}

// Token has a key that is written and never read.
type Token struct {
	ID   int64 `ant:"writeonly"`
	Name string
}

var Tokens = ant.Fields[Token]()

// InTuples compares several columns at once.
func TestInTuples(t *testing.T) {
	fixture.Run(t, func(t *testing.T, db *fixture.AppDB) {
		ctx := t.Context()
		fixture.Seed(t, db)

		columns := []ant.FieldRef{ant.F(&Users.Name), ant.F(&Users.Age)}

		users, err := db.Users.Where(ant.InTuples(columns, []any{"Alice", 30}, []any{"Bob", 99})).
			All(ctx)
		require.NoError(t, err)
		require.Equal(t, []string{"Alice"}, fixture.Names(users))

		t.Run("an empty list matches nothing", func(t *testing.T) {
			users, err := db.Users.Where(ant.InTuples(columns)).All(ctx)
			require.NoError(t, err)
			require.Empty(t, users)
		})

		t.Run("a tuple of the wrong size is an error", func(t *testing.T) {
			_, err := db.Users.Where(ant.InTuples(columns, []any{"Alice"})).All(ctx)
			require.ErrorContains(t, err, "1 values for 2 columns")
		})
	})
}

// Number is a row of a table-valued function.
type Number struct {
	N int64
}

// Numbers is Number's probe.
var Numbers = ant.Fields[Number]()

// FromFunc reads a function that returns rows, and queries build on it.
func TestFromFunc(t *testing.T) {
	fixture.Run(t, func(t *testing.T, db *fixture.AppDB) {
		numbers, err := ant.FromFunc[Number](
			db,
			"generate_series(?::bigint, ?::bigint) AS numbers(n)",
			1,
			5,
		).
			Where(ant.Gt(&Numbers.N, 3)).
			OrderBy(&Numbers.N).
			All(t.Context())
		require.NoError(t, err)
		require.Equal(t, []*Number{{4}, {5}}, numbers)
	})
}

// Hint writes an optimizer hint at the head of a statement, where pg_hint_plan
// reads it. To a database with no such extension, it is a comment.
func TestHint(t *testing.T) {
	fixture.Run(t, func(t *testing.T, db *fixture.AppDB) {
		q := db.Users.Query().Hint("SeqScan(users)")
		text, _, err := q.ToSQL()
		require.NoError(t, err)
		require.Contains(t, text, "/*+ SeqScan(users) */ SELECT")
		_, err = q.All(t.Context())
		require.NoError(t, err)

		t.Run("it cannot escape the comment", func(t *testing.T) {
			text, _, err := db.Users.Query().Hint("*/ DROP TABLE users; /*").ToSQL()
			require.NoError(t, err)
			require.NotContains(t, text, "*/ DROP")
		})

		t.Run("or recombine into a delimiter", func(t *testing.T) {
			text, _, err := db.Users.Query().Hint("/*/ UNION SELECT * FROM users; --").ToSQL()
			require.NoError(t, err)
			require.Equal(t, 1, strings.Count(text, "/*"), text)
			require.Equal(t, 1, strings.Count(text, "*/"), text)
		})
	})
}
