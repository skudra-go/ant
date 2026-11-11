package querying

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/skudra-go/ant"
	"github.com/skudra-go/ant/tests/features/fixture"
)

// ant.And, ant.Or, and ant.Not combine conditions.
func TestLogic(t *testing.T) {
	fixture.Run(t, func(t *testing.T, db *fixture.AppDB) {
		fixture.Seed(t, db)
		ctx := t.Context()

		cases := []struct {
			name string
			cond ant.Cond
			want []string
		}{
			{
				"And",
				ant.And(ant.Gt(&Users.Age, 18), ant.Like(&Users.Name, "C%")),
				[]string{"Carol"},
			},
			{
				"Or",
				ant.Or(ant.Lt(&Users.Age, 18), ant.Eq(&Users.Name, "Alice")),
				[]string{"Alice", "Bob"},
			},
			{"Not", ant.Not(ant.Eq(&Users.Name, "Bob")), []string{"Alice", "Carol"}},
			{
				"nesting keeps its grouping",
				ant.And(
					ant.Gt(&Users.Age, 18),
					ant.Or(ant.Eq(&Users.Name, "Alice"), ant.Eq(&Users.Name, "Bob")),
				),
				[]string{"Alice"},
			},
			{
				"Not of a group",
				ant.Not(ant.Or(ant.Eq(&Users.Name, "Alice"), ant.Eq(&Users.Name, "Bob"))),
				[]string{"Carol"},
			},
			{"one condition", ant.Or(ant.Eq(&Users.Name, "Bob")), []string{"Bob"}},
			{"an empty And matches everything", ant.And(), []string{"Alice", "Bob", "Carol"}},
			{"an empty Or matches nothing", ant.Or(), []string{}},
		}

		for _, c := range cases {
			t.Run(c.name, func(t *testing.T) {
				got, err := db.Users.Where(c.cond).OrderBy(&Users.Name).All(ctx)
				require.NoError(t, err)
				require.Equal(t, c.want, fixture.Names(got))
			})
		}

		t.Run("an error inside a group is reported", func(t *testing.T) {
			age := 1
			//antvet:ignore not a probe, on purpose: the test checks the error
			_, err := db.Users.Where(ant.Not(ant.Or(ant.Gt(&age, 0)))).All(ctx)
			require.ErrorIs(t, err, ant.ErrNotProbe)
		})
	})
}
