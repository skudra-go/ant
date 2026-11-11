package writing

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/skudra-go/ant"
	"github.com/skudra-go/ant/tests/features/fixture"
)

// Delete removes every matching row and reports how many it removed.
func TestDelete(t *testing.T) {
	fixture.Run(t, func(t *testing.T, db *fixture.AppDB) {
		_, bob, _ := fixture.Seed(t, db)
		ctx := t.Context()

		t.Run("no match removes nothing", func(t *testing.T) {
			n, err := db.Users.Where(ant.Eq(&Users.Name, "Nobody")).Delete(ctx)
			require.NoError(t, err)
			require.Zero(t, n)
		})

		t.Run("returns the rows removed", func(t *testing.T) {
			n, err := db.Users.Where(ant.Gt(&Users.Age, 18)).Delete(ctx)
			require.NoError(t, err)
			require.EqualValues(t, 2, n)

			left, err := db.Users.All(ctx)
			require.NoError(t, err)
			require.Equal(t, []string{"Bob"}, fixture.Names(left))
		})

		t.Run("a foreign key failure is the database's error", func(t *testing.T) {
			_, err := ant.Raw[any](
				db,
				`CREATE TABLE notes (user_id BIGINT REFERENCES users (id))`,
			).Exec(ctx)
			require.NoError(t, err)
			_, err = ant.Raw[any](db, `INSERT INTO notes VALUES ($1)`, bob.ID).Exec(ctx)
			require.NoError(t, err)

			_, err = db.Users.Where(ant.Eq(&Users.ID, bob.ID)).Delete(ctx)
			require.ErrorContains(t, err, "foreign key")
		})
	})
}
