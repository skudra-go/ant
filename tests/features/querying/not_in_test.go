package querying

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/skudra-go/ant"
	"github.com/skudra-go/ant/tests/features/fixture"
)

// ant.NotIn matches none of a list of values.
func TestNotIn(t *testing.T) {
	fixture.Run(t, func(t *testing.T, db *fixture.AppDB) {
		fixture.Seed(t, db)
		ctx := t.Context()

		t.Run("matches", func(t *testing.T) {
			got, err := db.Users.Where(ant.NotIn(&Users.Name, "Alice", "Carol")).All(ctx)
			require.NoError(t, err)
			require.Equal(t, []string{"Bob"}, fixture.Names(got))
		})

		t.Run("an empty list matches everything", func(t *testing.T) {
			var none []string

			got, err := db.Users.Where(ant.NotIn(&Users.Name, none...)).All(ctx)
			require.NoError(t, err)
			require.Len(t, got, 3)
		})

		// SQL rule: NULL is neither in nor not in a list.
		t.Run("a NULL column does not match a non-empty list", func(t *testing.T) {
			got, err := db.Users.Where(ant.NotIn(&Users.Bio, new("Other"))).All(ctx)
			require.NoError(t, err)
			require.Equal(t, []string{"Carol"}, fixture.Names(got))
		})
	})
}
