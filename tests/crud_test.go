package tests

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/skudra-go/ant"
	"github.com/skudra-go/ant/internal/testdb"
)

func TestInsertAssignsKey(t *testing.T) {
	testdb.ForEach(t, func(t *testing.T, target testdb.Target) {
		app := openApp(t, target)
		alice := &User{Name: "Alice", Email: "alice@example.com", Age: 30}
		require.NoError(t, app.Users.Insert(t.Context(), alice))
		require.NotZero(t, alice.ID)
		require.Equal(t, 1, countRows(t, target.DB, "users"))
	})
}

func TestFind(t *testing.T) {
	testdb.ForEach(t, func(t *testing.T, target testdb.Target) {
		app := openApp(t, target)
		alice := seedUsers(t, app, "Alice", 30)[0]
		user, err := app.Users.Find(t.Context(), alice.ID)
		require.NoError(t, err)
		require.Equal(t, "Alice", user.Name)
	})
}

func TestFindNotFound(t *testing.T) {
	testdb.ForEach(t, func(t *testing.T, target testdb.Target) {
		app := openApp(t, target)
		_, err := app.Users.Find(t.Context(), int64(404))
		require.ErrorIs(t, err, ant.ErrNotFound)
	})
}

func TestBulkUpdate(t *testing.T) {
	testdb.ForEach(t, func(t *testing.T, target testdb.Target) {
		app := openApp(t, target)
		alice := seedUsers(t, app, "Alice", 30)[0]
		require.NoError(t, app.Posts.Insert(t.Context(), &Post{UserID: alice.ID, Title: "Hello"}))

		n, err := app.Posts.Where(ant.Eq(&Posts.UserID, alice.ID)).
			Update(t.Context(), ant.Set(&Posts.Published, true))
		require.NoError(t, err)
		require.EqualValues(t, 1, n)
	})
}

func TestBulkUpdateWritesZeroValues(t *testing.T) {
	testdb.ForEach(t, func(t *testing.T, target testdb.Target) {
		app := openApp(t, target)
		seedUsers(t, app, "Alice", 30)

		_, err := app.Users.Where(ant.Eq(&Users.Name, "Alice")).
			Update(t.Context(), ant.Set(&Users.Age, 0))
		require.NoError(t, err)
		got, err := app.Users.Where(ant.Eq(&Users.Age, 0)).All(t.Context())
		require.NoError(t, err)
		require.Equal(t, []string{"Alice"}, names(got))
	})
}

func TestBulkDelete(t *testing.T) {
	testdb.ForEach(t, func(t *testing.T, target testdb.Target) {
		app := openApp(t, target)
		alice := seedUsers(t, app, "Alice", 30)[0]
		require.NoError(t, app.Posts.Insert(
			t.Context(),
			&Post{UserID: alice.ID, Title: "Draft ideas"},
			&Post{UserID: alice.ID, Title: "Hello"},
		))

		n, err := app.Posts.Where(ant.Like(&Posts.Title, "Draft%")).Delete(t.Context())
		require.NoError(t, err)
		require.EqualValues(t, 1, n)
	})
}

func TestRawInto(t *testing.T) {
	testdb.ForEach(t, func(t *testing.T, target testdb.Target) {
		app := openApp(t, target)
		seedUsers(t, app, "Alice", 30, "Bob", 17)

		query := "SELECT * FROM users WHERE age > " + target.Dialect.Placeholder(1)
		rows, err := ant.Raw[User](app, query, 18).All(t.Context())
		require.NoError(t, err)
		require.Equal(t, []string{"Alice"}, names(rows))
	})
}

func TestRawExec(t *testing.T) {
	testdb.ForEach(t, func(t *testing.T, target testdb.Target) {
		app := openApp(t, target)
		seedUsers(t, app, "Alice", 30)

		query := "UPDATE users SET age = " + target.Dialect.Placeholder(1)
		n, err := ant.Raw[User](app, query, 31).Exec(t.Context())
		require.NoError(t, err)
		require.EqualValues(t, 1, n)
	})
}
