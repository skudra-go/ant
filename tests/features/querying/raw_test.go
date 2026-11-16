package querying

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/skudra-go/ant"
	"github.com/skudra-go/ant/tests/features/fixture"
)

// ant.Raw runs your own SQL and scans into the same structs.
func TestRaw(t *testing.T) {
	fixture.Run(t, func(t *testing.T, db *fixture.AppDB) {
		fixture.Seed(t, db)
		ctx := t.Context()

		t.Run("into a model", func(t *testing.T) {
			got, err := ant.Raw[fixture.User](
				db,
				`SELECT * FROM users WHERE age > $1 ORDER BY name`,
				18,
			).All(ctx)
			require.NoError(t, err)
			require.Equal(t, []string{"Alice", "Carol"}, fixture.Names(got))
		})

		t.Run("no rows is an empty slice", func(t *testing.T) {
			got, err := ant.Raw[fixture.User](db, `SELECT * FROM users WHERE age > 100`).All(ctx)
			require.NoError(t, err)
			require.Empty(t, got)
		})

		t.Run("into any struct, by column name", func(t *testing.T) {
			type summary struct {
				Name    string
				YearsOn int
			}

			got, err := ant.Raw[summary](
				db,
				`SELECT name, age + 1 AS years_on FROM users WHERE name = 'Bob'`,
			).First(ctx)
			require.NoError(t, err)
			require.Equal(t, summary{"Bob", 18}, *got)
		})

		t.Run("columns with no field are ignored", func(t *testing.T) {
			got, err := ant.Raw[fixture.User](
				db,
				`SELECT name, 1 AS extra FROM users WHERE name = 'Bob'`,
			).First(ctx)
			require.NoError(t, err)
			require.Equal(t, "Bob", got.Name)
		})

		t.Run("fields with no column stay zero", func(t *testing.T) {
			got, err := ant.Raw[fixture.User](
				db,
				`SELECT name FROM users WHERE name = 'Bob'`,
			).First(ctx)
			require.NoError(t, err)
			require.Zero(t, got.Age)
		})

		// Both tables have an id, so the last column would give the user the
		// post's id, and an update by it would reach the wrong row.
		t.Run("two columns for one field are an error", func(t *testing.T) {
			alice, err := db.Users.Where(ant.Eq(&Users.Name, "Alice")).First(ctx)
			require.NoError(t, err)
			require.NoError(t, db.Posts.Insert(ctx, &fixture.Post{UserID: alice.ID, Title: "t"}))

			const joined = `SELECT * FROM users u JOIN posts p ON p.user_id = u.id`

			_, err = ant.Raw[fixture.User](db, joined).All(ctx)
			require.ErrorContains(t, err, `result columns 1 and 8 are both "id", for User.Base.ID`)

			_, err = ant.Raw[fixture.User](db, joined).First(ctx)
			require.ErrorContains(t, err, "alias one of them")

			got, err := ant.Raw[fixture.User](
				db,
				`SELECT u.*, p.title FROM users u JOIN posts p ON p.user_id = u.id`,
			).
				All(ctx)
			require.NoError(t, err, "each column once is fine")
			require.Equal(t, alice.ID, got[0].ID)
		})

		t.Run("First with no rows", func(t *testing.T) {
			_, err := ant.Raw[fixture.User](db, `SELECT * FROM users WHERE age > 100`).First(ctx)
			require.ErrorIs(t, err, ant.ErrNotFound)
		})

		t.Run("invalid SQL is the database's error", func(t *testing.T) {
			_, err := ant.Raw[fixture.User](db, `SELEC`).All(ctx)
			require.ErrorContains(t, err, "syntax error")
		})

		t.Run("the wrong number of arguments is an error", func(t *testing.T) {
			_, err := ant.Raw[fixture.User](db, `SELECT * FROM users WHERE age > $1`).All(ctx)
			require.Error(t, err)
		})

		t.Run("a handle that is not open", func(t *testing.T) {
			_, err := ant.Raw[fixture.User](&fixture.AppDB{}, `SELECT 1`).All(ctx)
			require.ErrorIs(t, err, ant.ErrNotOpen)
			_, err = ant.Raw[fixture.User](nil, `SELECT 1`).All(ctx)
			require.ErrorIs(t, err, ant.ErrNotOpen)
		})
	})
}
