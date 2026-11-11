package querying

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/skudra-go/ant"
	"github.com/skudra-go/ant/tests/features/fixture"
)

// When adds to a query only if a runtime condition holds.
func TestWhen(t *testing.T) {
	fixture.Run(t, func(t *testing.T, db *fixture.AppDB) {
		fixture.Seed(t, db)
		ctx := t.Context()

		search := func(name string, adultsOnly bool) ([]*fixture.User, error) {
			return db.Users.
				OrderBy(&Users.Name).
				When(name != "", func(q *ant.Query[fixture.User]) *ant.Query[fixture.User] {
					return q.Where(ant.Eq(&Users.Name, name))
				}).
				When(adultsOnly, func(q *ant.Query[fixture.User]) *ant.Query[fixture.User] {
					return q.Where(ant.Gte(&Users.Age, 18))
				}).
				All(ctx)
		}

		t.Run("false leaves the query alone", func(t *testing.T) {
			got, err := search("", false)
			require.NoError(t, err)
			require.Len(t, got, 3)
		})

		t.Run("true applies the function", func(t *testing.T) {
			got, err := search("", true)
			require.NoError(t, err)
			require.Equal(t, []string{"Alice", "Carol"}, fixture.Names(got))
		})

		t.Run("both apply together", func(t *testing.T) {
			got, err := search("Bob", true)
			require.NoError(t, err)
			require.Empty(t, got)
		})
	})
}
