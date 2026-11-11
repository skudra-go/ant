package querying

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/skudra-go/ant"
	"github.com/skudra-go/ant/tests/features/fixture"
)

// ant.In matches any of a list of values.
func TestIn(t *testing.T) {
	fixture.Run(t, func(t *testing.T, db *fixture.AppDB) {
		fixture.Seed(t, db)
		ctx := t.Context()

		t.Run("matches", func(t *testing.T) {
			got, err := db.Users.Where(ant.In(&Users.Name, "Alice", "Carol")).
				OrderBy(&Users.Name).
				All(ctx)
			require.NoError(t, err)
			require.Equal(t, []string{"Alice", "Carol"}, fixture.Names(got))
		})

		t.Run("a slice expands", func(t *testing.T) {
			ages := []int{17, 40}
			got, err := db.Users.Where(ant.In(&Users.Age, ages...)).OrderBy(&Users.Name).All(ctx)
			require.NoError(t, err)
			require.Equal(t, []string{"Bob", "Carol"}, fixture.Names(got))
		})

		t.Run("an empty list matches nothing", func(t *testing.T) {
			var none []int

			got, err := db.Users.Where(ant.In(&Users.Age, none...)).All(ctx)
			require.NoError(t, err)
			require.Empty(t, got)
		})

		t.Run("duplicates change nothing", func(t *testing.T) {
			got, err := db.Users.Where(ant.In(&Users.Name, "Bob", "Bob")).All(ctx)
			require.NoError(t, err)
			require.Len(t, got, 1)
		})
	})
}
