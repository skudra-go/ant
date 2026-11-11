package querying

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/skudra-go/ant"
	"github.com/skudra-go/ant/tests/features/fixture"
)

// ant.Ne matches a column that differs from a value.
func TestNe(t *testing.T) {
	fixture.Run(t, func(t *testing.T, db *fixture.AppDB) {
		fixture.Seed(t, db)
		ctx := t.Context()

		t.Run("matches", func(t *testing.T) {
			got, err := db.Users.Where(ant.Ne(&Users.Name, "Bob")).OrderBy(&Users.Name).All(ctx)
			require.NoError(t, err)
			require.Equal(t, []string{"Alice", "Carol"}, fixture.Names(got))
		})

		t.Run("a nil pointer matches non-NULL", func(t *testing.T) {
			got, err := db.Users.Where(ant.Ne(&Users.Bio, nil)).All(ctx)
			require.NoError(t, err)
			require.Equal(t, []string{"Carol"}, fixture.Names(got))
		})

		// SQL rule: comparing NULL with a value is neither true nor false.
		t.Run("a NULL column never matches a value", func(t *testing.T) {
			got, err := db.Users.Where(ant.Ne(&Users.Bio, new("Other"))).All(ctx)
			require.NoError(t, err)
			require.Equal(t, []string{"Carol"}, fixture.Names(got))
		})
	})
}
