package querying

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/skudra-go/ant"
	"github.com/skudra-go/ant/tests/features/fixture"
)

// Builder methods return a new query, so one query can be a base for others.
func TestQueryReuse(t *testing.T) {
	fixture.Run(t, func(t *testing.T, db *fixture.AppDB) {
		fixture.Seed(t, db)
		ctx := t.Context()
		adults := db.Users.Where(ant.Gt(&Users.Age, 18)).OrderBy(&Users.Name)

		alice, err := adults.Where(ant.Eq(&Users.Name, "Alice")).All(ctx)
		require.NoError(t, err)
		require.Equal(t, []string{"Alice"}, fixture.Names(alice))

		carol, err := adults.Where(ant.Eq(&Users.Name, "Carol")).All(ctx)
		require.NoError(t, err)
		require.Equal(t, []string{"Carol"}, fixture.Names(carol))

		// The base query is unchanged, and can run any number of times.
		for range 2 {
			everyone, err := adults.All(ctx)
			require.NoError(t, err)
			require.Equal(t, []string{"Alice", "Carol"}, fixture.Names(everyone))
		}
	})
}
