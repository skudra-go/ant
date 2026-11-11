package tests

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/skudra-go/ant"
	"github.com/skudra-go/ant/internal/testdb"
)

func seedGraph(t *testing.T, app *AppDB) {
	t.Helper()

	alice := &User{Name: "Alice", Email: "alice@example.com", Age: 30}
	require.NoError(t, app.Users.Insert(t.Context(), alice))

	require.NoError(t, app.Posts.Insert(t.Context(),
		&Post{UserID: alice.ID, Title: "Hello", Published: true},
		&Post{UserID: alice.ID, Title: "Draft ideas"}))
}

func TestInclude(t *testing.T) {
	testdb.ForEach(t, func(t *testing.T, target testdb.Target) {
		app := openApp(t, target)
		seedGraph(t, app)

		adults, err := app.Users.
			Where(ant.Gt(&Users.Age, 18)).
			OrderBy(&Users.Name).
			Include(&Users.Posts).
			All(t.Context())
		require.NoError(t, err)
		require.Len(t, adults, 1)
		require.ElementsMatch(t, []string{"Hello", "Draft ideas"}, titles(adults[0].Posts))
	})
}

func TestIncludeOne(t *testing.T) {
	testdb.ForEach(t, func(t *testing.T, target testdb.Target) {
		app := openApp(t, target)
		seedGraph(t, app)
		posts, err := app.Posts.IncludeOne(&Posts.User).All(t.Context())
		require.NoError(t, err)
		require.Len(t, posts, 2)
		require.Equal(t, "Alice", posts[0].User.Name)
	})
}

func TestNotIncludedRelationStaysEmpty(t *testing.T) {
	testdb.ForEach(t, func(t *testing.T, target testdb.Target) {
		app := openApp(t, target)
		seedGraph(t, app)
		users, err := app.Users.All(t.Context())
		require.NoError(t, err)
		require.Len(t, users, 1)
		require.Empty(t, users[0].Posts)
	})
}

func TestJoinSelect(t *testing.T) {
	testdb.ForEach(t, func(t *testing.T, target testdb.Target) {
		app := openApp(t, target)
		seedGraph(t, app)

		type Summary struct{ Author, Title string }

		summaries, err := app.Posts.
			Join(&Posts.User).
			Where(ant.Eq(&Posts.Published, true)).
			Select(func(p *Post, u *User) Summary { return Summary{u.Name, p.Title} }).
			All(t.Context())
		require.NoError(t, err)
		require.Equal(t, []Summary{{"Alice", "Hello"}}, summaries)
	})
}
