package querying

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/skudra-go/ant"
	"github.com/skudra-go/ant/tests/features/fixture"
)

// Gt, Gte, Lt, and Lte work on any ordered type, including time.Time.
func TestComparisons(t *testing.T) {
	fixture.Run(t, func(t *testing.T, db *fixture.AppDB) {
		fixture.Seed(t, db)
		ctx := t.Context()

		cases := []struct {
			name string
			cond ant.Cond
			want []string
		}{
			{"Gt excludes the value", ant.Gt(&Users.Age, 30), []string{"Carol"}},
			{"Gte includes the value", ant.Gte(&Users.Age, 30), []string{"Alice", "Carol"}},
			{"Lt excludes the value", ant.Lt(&Users.Age, 30), []string{"Bob"}},
			{"Lte includes the value", ant.Lte(&Users.Age, 30), []string{"Alice", "Bob"}},
			{"strings compare too", ant.Gt(&Users.Name, "Bob"), []string{"Carol"}},
		}

		for _, c := range cases {
			t.Run(c.name, func(t *testing.T) {
				got, err := db.Users.Where(c.cond).OrderBy(&Users.Name).All(ctx)
				require.NoError(t, err)
				require.Equal(t, c.want, fixture.Names(got))
			})
		}

		t.Run("time.Time", func(t *testing.T) {
			got, err := db.Users.Where(ant.Lt(&Users.CreatedAt, time.Now())).All(ctx)
			require.NoError(t, err)
			require.Len(t, got, 3)
		})
	})
}
