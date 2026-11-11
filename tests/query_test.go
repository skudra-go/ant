package tests

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/skudra-go/ant"
	"github.com/skudra-go/ant/internal/testdb"
)

func TestWhereOrderByAll(t *testing.T) {
	testdb.ForEach(t, func(t *testing.T, target testdb.Target) {
		app := openApp(t, target)
		seedUsers(t, app, "Alice", 30, "Bob", 17)
		got, err := app.Users.Where(ant.Gt(&Users.Age, 18)).OrderBy(&Users.Name).All(t.Context())
		require.NoError(t, err)
		require.Equal(t, []string{"Alice"}, names(got))
	})
}

func TestConditions(t *testing.T) {
	cases := []struct {
		name string
		cond ant.Cond
		want []string
	}{
		{"Eq", ant.Eq(&Users.Name, "Bob"), []string{"Bob"}},
		{"Gt", ant.Gt(&Users.Age, 18), []string{"Alice", "Carol"}},
		{"Lt", ant.Lt(&Users.Age, 18), []string{"Bob"}},
		{"In", ant.In(&Users.Name, "Alice", "Carol"), []string{"Alice", "Carol"}},
		{"Like", ant.Like(&Users.Name, "A%"), []string{"Alice"}},
		{"IsNull", ant.IsNull(&Users.Bio), []string{"Alice", "Bob", "Carol"}},
		{"nil pointer", ant.Eq(&Users.Bio, nil), []string{"Alice", "Bob", "Carol"}},
		{"And", ant.And(ant.Gt(&Users.Age, 18), ant.Like(&Users.Name, "C%")), []string{"Carol"}},
		{
			"Or",
			ant.Or(ant.Lt(&Users.Age, 18), ant.Eq(&Users.Name, "Alice")),
			[]string{"Alice", "Bob"},
		},
		{"Not", ant.Not(ant.Eq(&Users.Name, "Bob")), []string{"Alice", "Carol"}},
		{"EqCol", ant.EqCol(&Users.Name, &Users.Email), []string{}},
	}

	testdb.ForEach(t, func(t *testing.T, target testdb.Target) {
		app := openApp(t, target)
		seedUsers(t, app, "Alice", 30, "Bob", 17, "Carol", 40)

		for _, c := range cases {
			t.Run(c.name, func(t *testing.T) {
				got, err := app.Users.Where(c.cond).OrderBy(&Users.Name).All(t.Context())
				require.NoError(t, err)
				require.Equal(t, c.want, names(got))
			})
		}
	})
}

func TestOrderLimitOffset(t *testing.T) {
	testdb.ForEach(t, func(t *testing.T, target testdb.Target) {
		app := openApp(t, target)
		seedUsers(t, app, "Alice", 30, "Bob", 17, "Carol", 40)
		got, err := app.Users.OrderByDesc(&Users.Age).Limit(1).Offset(1).All(t.Context())
		require.NoError(t, err)
		require.Equal(t, []string{"Alice"}, names(got))
	})
}

func TestOrderByTimestamp(t *testing.T) {
	testdb.ForEach(t, func(t *testing.T, target testdb.Target) {
		app := openApp(t, target)
		seedUsers(t, app, "Alice", 30)
		got, err := app.Users.OrderBy(&Users.CreatedAt).All(t.Context())
		require.NoError(t, err)
		require.Len(t, got, 1)
	})
}

func TestFirst(t *testing.T) {
	testdb.ForEach(t, func(t *testing.T, target testdb.Target) {
		app := openApp(t, target)
		seedUsers(t, app, "Alice", 30)
		user, err := app.Users.Where(ant.Eq(&Users.Name, "Alice")).First(t.Context())
		require.NoError(t, err)
		require.Equal(t, 30, user.Age)
		_, err = app.Users.Where(ant.Eq(&Users.Name, "Nobody")).First(t.Context())
		require.ErrorIs(t, err, ant.ErrNotFound)
	})
}

func TestEach(t *testing.T) {
	testdb.ForEach(t, func(t *testing.T, target testdb.Target) {
		app := openApp(t, target)
		alice := seedUsers(t, app, "Alice", 30)[0]

		for i := range 3 {
			require.NoError(t, app.Posts.Insert(t.Context(), &Post{
				UserID:    alice.ID,
				Title:     fmt.Sprintf("Post %d", i),
				Published: i > 0,
			}))
		}

		var seen []string

		published := app.Posts.Where(ant.Eq(&Posts.Published, true)).OrderBy(&Posts.Title)

		for post, err := range published.Each(t.Context()) {
			require.NoError(t, err)

			seen = append(seen, post.Title)
		}

		require.Equal(t, []string{"Post 1", "Post 2"}, seen)
	})
}
