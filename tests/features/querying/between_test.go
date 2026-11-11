package querying

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/skudra-go/ant"
	"github.com/skudra-go/ant/tests/features/fixture"
)

// ant.Between matches an inclusive range.
func TestBetween(t *testing.T) {
	fixture.Run(t, func(t *testing.T, db *fixture.AppDB) {
		fixture.Seed(t, db)
		ctx := t.Context()

		t.Run("both ends are included", func(t *testing.T) {
			got, err := db.Users.Where(ant.Between(&Users.Age, 17, 30)).
				OrderBy(&Users.Name).
				All(ctx)
			require.NoError(t, err)
			require.Equal(t, []string{"Alice", "Bob"}, fixture.Names(got))
		})

		t.Run("low above high matches nothing", func(t *testing.T) {
			got, err := db.Users.Where(ant.Between(&Users.Age, 30, 17)).All(ctx)
			require.NoError(t, err)
			require.Empty(t, got)
		})

		t.Run("Not excludes the range", func(t *testing.T) {
			got, err := db.Users.Where(ant.Not(ant.Between(&Users.Age, 17, 30))).All(ctx)
			require.NoError(t, err)
			require.Equal(t, []string{"Carol"}, fixture.Names(got))
		})
	})
}
