package querying

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/skudra-go/ant"
	"github.com/skudra-go/ant/tests/features/fixture"
)

// ant.IsNull and ant.IsNotNull test a nullable column.
func TestIsNull(t *testing.T) {
	fixture.Run(t, func(t *testing.T, db *fixture.AppDB) {
		fixture.Seed(t, db)
		ctx := t.Context()

		t.Run("IsNull", func(t *testing.T) {
			got, err := db.Users.Where(ant.IsNull(&Users.Bio)).OrderBy(&Users.Name).All(ctx)
			require.NoError(t, err)
			require.Equal(t, []string{"Alice", "Bob"}, fixture.Names(got))
		})

		t.Run("IsNotNull", func(t *testing.T) {
			got, err := db.Users.Where(ant.IsNotNull(&Users.Bio)).All(ctx)
			require.NoError(t, err)
			require.Equal(t, []string{"Carol"}, fixture.Names(got))
		})

		t.Run("a column that is never NULL matches nothing", func(t *testing.T) {
			got, err := db.Users.Where(ant.IsNull(&Users.Name)).All(ctx)
			require.NoError(t, err)
			require.Empty(t, got)
		})
	})
}
