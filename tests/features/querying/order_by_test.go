package querying

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/skudra-go/ant"
	"github.com/skudra-go/ant/tests/features/fixture"
)

// OrderBy sets the order, and ThenBy breaks ties.
func TestOrderBy(t *testing.T) {
	fixture.Run(t, func(t *testing.T, db *fixture.AppDB) {
		ctx := t.Context()

		for _, u := range []fixture.User{
			{Name: "Bea", Email: "1", Age: 30},
			{Name: "Abe", Email: "2", Age: 30},
			{Name: "Cal", Email: "3", Age: 20},
		} {
			require.NoError(t, db.Users.Insert(ctx, &u))
		}

		cases := []struct {
			name  string
			query *ant.Query[fixture.User]
			want  []string
		}{
			{"ascending", db.Users.OrderBy(&Users.Name), []string{"Abe", "Bea", "Cal"}},
			{"descending", db.Users.OrderByDesc(&Users.Name), []string{"Cal", "Bea", "Abe"}},
			{
				"ThenBy breaks ties",
				db.Users.OrderBy(&Users.Age).ThenBy(&Users.Name),
				[]string{"Cal", "Abe", "Bea"},
			},
			{
				"ThenByDesc breaks ties",
				db.Users.OrderBy(&Users.Age).ThenByDesc(&Users.Name),
				[]string{"Cal", "Bea", "Abe"},
			},
			{
				"a later OrderBy replaces earlier ones",
				db.Users.OrderBy(&Users.Age).OrderBy(&Users.Name),
				[]string{"Abe", "Bea", "Cal"},
			},
		}

		for _, c := range cases {
			t.Run(c.name, func(t *testing.T) {
				got, err := c.query.All(ctx)
				require.NoError(t, err)
				require.Equal(t, c.want, fixture.Names(got))
			})
		}

		// A list endpoint takes the column to sort by from the request. A map of
		// FieldRef, which holds a column of any type, is the list it may name.
		t.Run("a column chosen at run time", func(t *testing.T) {
			sorts := map[string]ant.FieldRef{
				"name": ant.F(&Users.Name),
				"age":  ant.F(&Users.Age),
			}

			by, desc := "age", true // from the request
			ref, ok := sorts[by]
			require.True(t, ok, "refuse a column that isn't in the map")

			order := ref.Asc()

			if desc {
				order = ref.Desc()
			}

			got, err := db.Users.Order(order, sorts["name"].Asc()).All(ctx)
			require.NoError(t, err)
			require.Equal(t, []string{"Abe", "Bea", "Cal"}, fixture.Names(got))

			_, err = db.Users.Order(sorts["email"].Desc()).All(ctx)
			require.ErrorIs(t, err, ant.ErrNotProbe, "a lookup that misses gives the zero FieldRef")

			_, err = db.Users.Order(ant.F(new(int)).Asc()).All(ctx)
			require.ErrorIs(t, err, ant.ErrNotProbe, "a FieldRef of no column")
		})

		t.Run("ThenBy needs an OrderBy", func(t *testing.T) {
			_, err := db.Users.Where(ant.Gt(&Users.Age, 0)).ThenBy(&Users.Name).All(ctx)
			require.ErrorIs(t, err, ant.ErrThenWithoutOrder)
		})

		t.Run("a probe of another model is an error", func(t *testing.T) {
			//antvet:ignore another model's field, on purpose: the test checks the error
			_, err := db.Users.OrderBy(&Posts.Title).All(ctx)
			require.ErrorIs(t, err, ant.ErrWrongModel)
		})
	})
}
