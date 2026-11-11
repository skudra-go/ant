package querying

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/skudra-go/ant"
	"github.com/skudra-go/ant/tests/features/fixture"
)

// FromRaw starts from your own SQL and keeps building with typed methods.
func TestFromRaw(t *testing.T) {
	fixture.Run(t, func(t *testing.T, db *fixture.AppDB) {
		fixture.Seed(t, db)
		ctx := t.Context()
		adults := ant.FromRaw[fixture.User](db, `SELECT * FROM users WHERE age > ?`, 18)

		t.Run("conditions, ordering, and paging apply", func(t *testing.T) {
			got, err := adults.Where(ant.Lt(&Users.Age, 35)).OrderBy(&Users.Name).All(ctx)
			require.NoError(t, err)
			require.Equal(t, []string{"Alice"}, fixture.Names(got))
		})

		t.Run("Count", func(t *testing.T) {
			n, err := adults.Count(ctx)
			require.NoError(t, err)
			require.EqualValues(t, 2, n)
		})

		t.Run("a raw query error comes from the database", func(t *testing.T) {
			_, err := ant.FromRaw[fixture.User](db, `SELECT * FROM missing`).All(ctx)
			require.ErrorContains(t, err, "missing")
		})
	})
}
