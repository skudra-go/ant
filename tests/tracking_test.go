package tests

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/skudra-go/ant"
	"github.com/skudra-go/ant/internal/testdb"
)

func TestSaveChanges(t *testing.T) {
	testdb.ForEach(t, func(t *testing.T, target testdb.Target) {
		app := openApp(t, target)
		alice := seedUsers(t, app, "Alice", 30)[0]
		user, err := app.Users.Find(t.Context(), alice.ID)
		require.NoError(t, err)

		user.Age++
		user.Bio = new("Gopher since 2012")

		require.NoError(t, app.SaveChanges(t.Context()))

		var (
			age int
			bio string
		)
		require.NoError(
			t,
			target.DB.QueryRowContext(t.Context(), "SELECT age, bio FROM users").Scan(&age, &bio),
		)

		require.Equal(t, 31, age)
		require.Equal(t, "Gopher since 2012", bio)
	})
}

func TestSaveChangesWritesZeroValues(t *testing.T) {
	testdb.ForEach(t, func(t *testing.T, target testdb.Target) {
		app := openApp(t, target)
		alice := seedUsers(t, app, "Alice", 30)[0]
		user, err := app.Users.Find(t.Context(), alice.ID)
		require.NoError(t, err)

		user.Age = 0

		require.NoError(t, app.SaveChanges(t.Context()))

		var age int
		require.NoError(
			t,
			target.DB.QueryRowContext(t.Context(), "SELECT age FROM users").Scan(&age),
		)

		require.Zero(t, age)
	})
}

func TestNoTrackingIsNotSaved(t *testing.T) {
	testdb.ForEach(t, func(t *testing.T, target testdb.Target) {
		app := openApp(t, target)
		seedUsers(t, app, "Alice", 30)
		user, err := app.Users.NoTracking().Where(ant.Eq(&Users.Name, "Alice")).First(t.Context())
		require.NoError(t, err)

		user.Age = 99

		require.NoError(t, app.SaveChanges(t.Context()))

		var age int
		require.NoError(
			t,
			target.DB.QueryRowContext(t.Context(), "SELECT age FROM users").Scan(&age),
		)

		require.Equal(t, 30, age)
	})
}

func TestInsertGraph(t *testing.T) {
	testdb.ForEach(t, func(t *testing.T, target testdb.Target) {
		app := openApp(t, target)

		alice := &User{
			Name:  "Alice",
			Email: "alice@example.com",
			Age:   30,
			Posts: []Post{{Title: "Hello"}, {Title: "Draft ideas"}},
		}
		require.NoError(t, app.Users.Insert(t.Context(), alice))
		require.Equal(t, alice.ID, alice.Posts[0].UserID)
		require.Equal(t, 2, countRows(t, target.DB, "posts"))
	})
}
