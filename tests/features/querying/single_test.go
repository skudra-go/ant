package querying

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/skudra-go/ant"
	"github.com/skudra-go/ant/tests/features/fixture"
)

// Single returns the one row that matches, and refuses anything else.
func TestSingle(t *testing.T) {
	fixture.Run(t, func(t *testing.T, db *fixture.AppDB) {
		fixture.Seed(t, db)
		ctx := t.Context()

		t.Run("one match", func(t *testing.T) {
			user, err := db.Users.Where(ant.Eq(&Users.Name, "Bob")).Single(ctx)
			require.NoError(t, err)
			require.Equal(t, 17, user.Age)
		})

		t.Run("no match", func(t *testing.T) {
			_, err := db.Users.Where(ant.Eq(&Users.Name, "Nobody")).Single(ctx)
			require.ErrorIs(t, err, ant.ErrNotFound)
		})

		t.Run("several matches", func(t *testing.T) {
			user, err := db.Users.Where(ant.Gt(&Users.Age, 18)).Single(ctx)
			require.ErrorIs(t, err, ant.ErrMultipleRows)
			require.Nil(t, user)
		})

		t.Run("a table can be asked directly", func(t *testing.T) {
			_, err := db.Users.Single(ctx)
			require.ErrorIs(t, err, ant.ErrMultipleRows)
		})
	})
}
