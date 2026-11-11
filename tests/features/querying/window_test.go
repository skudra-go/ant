package querying

import (
	"database/sql"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/skudra-go/ant"
	"github.com/skudra-go/ant/tests/features/fixture"
)

type ranked struct {
	Name     string
	Position int64
	Running  int
	Previous sql.Null[int]
}

var Rankeds = ant.Fields[ranked]()

// Window functions compute over related rows without collapsing them.
func TestWindowFunctions(t *testing.T) {
	fixture.Run(t, func(t *testing.T, db *fixture.AppDB) {
		ctx := t.Context()

		for _, u := range []fixture.User{
			{Name: "a", Email: "a", Age: 10},
			{Name: "b", Email: "b", Age: 20},
			{Name: "c", Email: "c", Age: 20},
			{Name: "d", Email: "d", Age: 30},
		} {
			require.NoError(t, db.Users.Insert(ctx, &u))
		}

		byName := db.Users.OrderBy(&Users.Name)
		ascending := ant.OrderedBy(ant.Col(&Users.Age).Asc(), ant.Col(&Users.Name).Asc())

		positions := func(t *testing.T, e ant.Expr[int64]) []int64 {
			t.Helper()

			got, err := ant.Project[ranked](byName, ant.IntoExpr(&Rankeds.Position, e)).All(ctx)
			require.NoError(t, err)

			out := make([]int64, len(got))

			for i, r := range got {
				out[i] = r.Position
			}

			return out
		}

		t.Run("RowNumber", func(t *testing.T) {
			require.Equal(t, []int64{1, 2, 3, 4}, positions(t, ant.RowNumber().Over(ascending)))
		})

		t.Run("Rank leaves gaps after ties", func(t *testing.T) {
			w := ant.OrderedBy(ant.Col(&Users.Age).Asc())
			require.Equal(t, []int64{1, 2, 2, 4}, positions(t, ant.Rank().Over(w)))
		})

		t.Run("DenseRank does not", func(t *testing.T) {
			w := ant.OrderedBy(ant.Col(&Users.Age).Asc())
			require.Equal(t, []int64{1, 2, 2, 3}, positions(t, ant.DenseRank().Over(w)))
		})

		t.Run("PartitionBy restarts per group", func(t *testing.T) {
			w := ant.PartitionBy(&Users.Age).OrderBy(ant.Col(&Users.Name).Asc())
			require.Equal(t, []int64{1, 1, 2, 1}, positions(t, ant.RowNumber().Over(w)))
		})

		t.Run("an aggregate as a window", func(t *testing.T) {
			got, err := ant.Project[ranked](
				byName,
				ant.IntoExpr(&Rankeds.Running, ant.Sum(&Users.Age).Over(ascending)),
			).All(ctx)
			require.NoError(t, err)
			require.Equal(t, 80, got[3].Running)
		})

		t.Run("Lag reads the previous row, NULL for the first", func(t *testing.T) {
			got, err := ant.Project[ranked](
				byName,
				ant.IntoExpr(&Rankeds.Previous, ant.Lag(&Users.Age).Over(ascending)),
			).All(ctx)
			require.NoError(t, err)
			require.False(t, got[0].Previous.Valid)
			require.Equal(t, 10, got[1].Previous.V)
		})

		t.Run("Over needs a function", func(t *testing.T) {
			_, err := ant.Project[ranked](
				byName,
				ant.IntoExpr(&Rankeds.Running, ant.Col(&Users.Age).Over(ascending)),
			).All(ctx)
			require.Error(t, err)
		})
	})
}
