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

		// As Go's != does, and unlike SQL's <>: Alice and Bob have no bio, and
		// no bio is not "Other". Not(Eq(...)) is the same.
		t.Run("a NULL column matches a value", func(t *testing.T) {
			got, err := db.Users.Where(ant.Ne(&Users.Bio, new("Other"))).
				OrderBy(&Users.Name).
				All(ctx)
			require.NoError(t, err)
			require.Equal(t, []string{"Alice", "Bob", "Carol"}, fixture.Names(got))

			got, err = db.Users.Where(ant.Not(ant.Eq(&Users.Bio, new("Gopher")))).
				OrderBy(&Users.Name).
				All(ctx)
			require.NoError(t, err)
			require.Equal(t, []string{"Alice", "Bob"}, fixture.Names(got))
		})
	})
}
