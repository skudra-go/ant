package querying

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/skudra-go/ant"
	"github.com/skudra-go/ant/tests/features/fixture"
)

// All runs a query and returns every row.
func TestAll(t *testing.T) {
	fixture.Run(t, func(t *testing.T, db *fixture.AppDB) {
		ctx := t.Context()

		t.Run("no rows is an empty slice, not nil", func(t *testing.T) {
			got, err := db.Users.All(ctx)
			require.NoError(t, err)
			require.NotNil(t, got)
			require.Empty(t, got)
		})

		fixture.Seed(t, db)

		t.Run("a table returns every row", func(t *testing.T) {
			got, err := db.Users.All(ctx)
			require.NoError(t, err)
			require.Len(t, got, 3)
		})

		t.Run("every column is filled", func(t *testing.T) {
			got, err := db.Users.Where(ant.Eq(&Users.Name, "Carol")).All(ctx)
			require.NoError(t, err)
			require.NotZero(t, got[0].ID)
			require.Equal(t, "Carol@example.com", got[0].Email)
			require.Equal(t, 40, got[0].Age)
			require.Equal(t, "Gopher", *got[0].Bio)
		})

		t.Run("relations stay empty unless included", func(t *testing.T) {
			got, err := db.Users.All(ctx)
			require.NoError(t, err)
			require.Nil(t, got[0].Posts)
		})
	})
}
