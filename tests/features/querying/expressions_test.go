package querying

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/skudra-go/ant"
	"github.com/skudra-go/ant/tests/features/fixture"
)

type label struct{ Text string }

var Labels = ant.Fields[label]()

// Expressions compute values in SQL, with their Go type checked.
func TestExpressions(t *testing.T) {
	fixture.Run(t, func(t *testing.T, db *fixture.AppDB) {
		fixture.Seed(t, db)
		ctx := t.Context()
		bob := db.Users.Where(ant.Eq(&Users.Name, "Bob"))
		text := func(t *testing.T, e ant.Expr[string]) string {
			t.Helper()

			got, err := ant.Project[label](bob, ant.IntoExpr(&Labels.Text, e)).First(ctx)
			require.NoError(t, err)

			return got.Text
		}

		t.Run("Case", func(t *testing.T) {
			kind := ant.Case[string]().When(ant.Lt(&Users.Age, 18), "minor").Else("adult")
			require.Equal(t, "minor", text(t, kind))
		})

		t.Run("Lower, Upper, Coalesce", func(t *testing.T) {
			require.Equal(t, "bob", text(t, ant.Lower(&Users.Name)))
			require.Equal(t, "BOB", text(t, ant.Upper(&Users.Name)))
		})

		t.Run("Func calls any database function", func(t *testing.T) {
			require.Equal(
				t,
				"B0b",
				text(t, ant.Func[string]("REPLACE", ant.Col(&Users.Name), "o", "0")),
			)
		})

		t.Run("SQL mixes columns and bound values", func(t *testing.T) {
			require.Equal(
				t,
				"Bob/x",
				text(t, ant.SQL[string]("? || ?", ant.Col(&Users.Name), "/x")),
			)
		})

		t.Run("a bound value is never text in the query", func(t *testing.T) {
			require.Equal(
				t,
				"'); DROP TABLE users; --",
				text(t, ant.SQL[string]("?::text", "'); DROP TABLE users; --")),
			)
		})

		t.Run("Collate", func(t *testing.T) {
			got, err := db.Users.Order(ant.Collate(&Users.Name, "C").Desc()).First(ctx)
			require.NoError(t, err)
			require.Equal(t, "Carol", got.Name)
		})

		t.Run("expressions in conditions", func(t *testing.T) {
			got, err := db.Users.Where(ant.Lower(&Users.Name).Eq("bob")).All(ctx)
			require.NoError(t, err)
			require.Len(t, got, 1)

			got, err = db.Users.Where(ant.Col(&Users.Age).Plus(1).Eq(18)).All(ctx)
			require.NoError(t, err)
			require.Len(t, got, 1)

			got, err = db.Users.Where(ant.Length(&Users.Name).Gte(5)).OrderBy(&Users.Name).All(ctx)
			require.NoError(t, err)
			require.Equal(t, []string{"Alice", "Carol"}, fixture.Names(got))
		})

		t.Run("SQLCond", func(t *testing.T) {
			got, err := db.Users.Where(ant.SQLCond("? % ? = 0", ant.Col(&Users.Age), 10)).
				OrderBy(&Users.Name).
				All(ctx)
			require.NoError(t, err)
			require.Equal(t, []string{"Alice", "Carol"}, fixture.Names(got))
		})

		t.Run("too many arguments for the fragment", func(t *testing.T) {
			_, err := db.Users.Where(ant.SQLCond("age > 1", 5)).All(ctx)
			require.ErrorContains(t, err, "unused")
		})

		t.Run("ILike ignores case", func(t *testing.T) {
			got, err := db.Users.Where(ant.ILike(&Users.Name, "ALICE")).All(ctx)
			require.NoError(t, err)
			require.Len(t, got, 1)
		})

		t.Run("AnyOf, AllOf, NoneOf look across columns", func(t *testing.T) {
			got, err := db.Users.Where(ant.AnyOf("Bob", &Users.Name, &Users.Email)).All(ctx)
			require.NoError(t, err)
			require.Len(t, got, 1)

			got, err = db.Users.Where(ant.AllOf("Bob", &Users.Name, &Users.Email)).All(ctx)
			require.NoError(t, err)
			require.Empty(t, got)

			got, err = db.Users.Where(ant.NoneOf("Bob", &Users.Name, &Users.Email)).All(ctx)
			require.NoError(t, err)
			require.Len(t, got, 2)
		})
	})
}

// A CASE branch can be a column, with WhenExpr and ElseExpr.
func TestCaseWithExpressions(t *testing.T) {
	fixture.Run(t, func(t *testing.T, db *fixture.AppDB) {
		ctx := t.Context()
		fixture.Seed(t, db)

		label := ant.Case[string]().
			WhenExpr(ant.Lt(&Users.Age, 18), ant.SQL[string]("'minor'")).
			ElseExpr(ant.Col(&Users.Name))

		type row struct{ Label string }

		r := ant.Fields[row]()
		rows, err := ant.Project[row](
			db.Users.OrderBy(&Users.Age),
			ant.IntoExpr(&r.Label, label),
		).All(ctx)
		require.NoError(t, err)
		require.Equal(t, []row{{"minor"}, {"Alice"}, {"Carol"}}, rows)
	})
}
