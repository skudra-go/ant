package querying

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/skudra-go/ant"
	"github.com/skudra-go/ant/tests/features/fixture"
)

// ant.Eq matches a column equal to a value of the column's type.
func TestEq(t *testing.T) {
	fixture.Run(t, func(t *testing.T, db *fixture.AppDB) {
		fixture.Seed(t, db)
		ctx := t.Context()

		t.Run("matches", func(t *testing.T) {
			got, err := db.Users.Where(ant.Eq(&Users.Name, "Bob")).All(ctx)
			require.NoError(t, err)
			require.Equal(t, []string{"Bob"}, fixture.Names(got))
		})

		t.Run("no match is an empty result, not an error", func(t *testing.T) {
			got, err := db.Users.Where(ant.Eq(&Users.Name, "Nobody")).All(ctx)
			require.NoError(t, err)
			require.Empty(t, got)
		})

		t.Run("a zero value is a real value", func(t *testing.T) {
			got, err := db.Users.Where(ant.Eq(&Users.Age, 0)).All(ctx)
			require.NoError(t, err)
			require.Empty(t, got)
		})

		t.Run("several conditions must all match", func(t *testing.T) {
			got, err := db.Users.Where(ant.Eq(&Users.Name, "Bob"), ant.Eq(&Users.Age, 30)).All(ctx)
			require.NoError(t, err)
			require.Empty(t, got)
		})

		t.Run("repeated Where calls add up", func(t *testing.T) {
			got, err := db.Users.Where(ant.Eq(&Users.Name, "Bob")).
				Where(ant.Eq(&Users.Age, 17)).
				All(ctx)
			require.NoError(t, err)
			require.Equal(t, []string{"Bob"}, fixture.Names(got))
		})

		t.Run("a pointer column compares the value it points to", func(t *testing.T) {
			got, err := db.Users.Where(ant.Eq(&Users.Bio, new("Gopher"))).All(ctx)
			require.NoError(t, err)
			require.Equal(t, []string{"Carol"}, fixture.Names(got))
		})

		t.Run("a nil pointer matches NULL", func(t *testing.T) {
			got, err := db.Users.Where(ant.Eq(&Users.Bio, nil)).OrderBy(&Users.Name).All(ctx)
			require.NoError(t, err)
			require.Equal(t, []string{"Alice", "Bob"}, fixture.Names(got))
		})

		t.Run("values are bound, so text cannot change the query", func(t *testing.T) {
			got, err := db.Users.Where(ant.Eq(&Users.Name, "x' OR '1'='1")).All(ctx)
			require.NoError(t, err)
			require.Empty(t, got)
		})
	})
}
