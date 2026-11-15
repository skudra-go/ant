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

		// The databases disagree on what a negative limit means, so none sees one.
		t.Run("a negative Limit or Offset is refused", func(t *testing.T) {
			type row struct {
				Age int
			}

			R := ant.Fields[row]()
			name := func(u *fixture.User, _ *fixture.Post) string { return u.Name }

			for name, run := range map[string]func() error{
				"Limit": func() error {
					_, err := byName.Limit(-1).All(ctx)

					return err
				},
				"Offset": func() error {
					_, err := byName.Offset(-1).All(ctx)

					return err
				},
				"Table.Limit": func() error {
					_, err := db.Users.Limit(-1).Count(ctx)

					return err
				},
				"Table.Offset": func() error {
					_, _, err := db.Users.Offset(-5).ToSQL()

					return err
				},
				"a join's Limit": func() error {
					_, err := db.Users.JoinMany(&Users.Posts).Limit(-1).Select(name).All(ctx)

					return err
				},
				"a join's Offset": func() error {
					_, err := db.Users.JoinMany(&Users.Posts).Offset(-1).Select(name).All(ctx)

					return err
				},
				"a grouped Limit": func() error {
					_, err := ant.Project[row](
						db.Users.GroupBy(&Users.Age).Limit(-1),
						ant.Into(&R.Age, &Users.Age),
					).All(ctx)

					return err
				},
				"a grouped Offset": func() error {
					_, err := ant.Project[row](
						db.Users.GroupBy(&Users.Age).Offset(-1),
						ant.Into(&R.Age, &Users.Age),
					).All(ctx)

					return err
				},
			} {
				require.ErrorContains(t, run(), "cannot be negative", name)
			}
		})
	})
}
