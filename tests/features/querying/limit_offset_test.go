package querying

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/skudra-go/ant"
	"github.com/skudra-go/ant/tests/features/fixture"
)

// Limit and Offset page through ordered results.
func TestLimitOffset(t *testing.T) {
	fixture.Run(t, func(t *testing.T, db *fixture.AppDB) {
		fixture.Seed(t, db)
		ctx := t.Context()
		byName := db.Users.OrderBy(&Users.Name)

		cases := []struct {
			name  string
			query *ant.Query[fixture.User]
			want  []string
		}{
			{"Limit", byName.Limit(2), []string{"Alice", "Bob"}},
			{"Offset", byName.Offset(2), []string{"Carol"}},
			{"both", byName.Limit(1).Offset(1), []string{"Bob"}},
			{"Limit past the end", byName.Limit(100), []string{"Alice", "Bob", "Carol"}},
			{"Offset past the end", byName.Offset(100), []string{}},
			{"Limit zero returns nothing", byName.Limit(0), []string{}},
			{"the last Limit wins", byName.Limit(1).Limit(2), []string{"Alice", "Bob"}},
		}

		for _, c := range cases {
			t.Run(c.name, func(t *testing.T) {
				got, err := c.query.All(ctx)
				require.NoError(t, err)
				require.Equal(t, c.want, fixture.Names(got))
			})
		}

		t.Run("a negative Limit is the database's error", func(t *testing.T) {
			_, err := byName.Limit(-1).All(ctx)
			require.Error(t, err)
		})
	})
}
