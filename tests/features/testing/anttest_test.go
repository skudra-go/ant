// Package testing shows package anttest, which tests of your own code use.
package testing

import (
	"embed"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/skudra-go/ant"
	"github.com/skudra-go/ant/anttest"
	"github.com/skudra-go/ant/tests/features/fixture"
)

//go:embed testdata
var data embed.FS

// Users is a factory. Each entity gets the next number, for what must be unique.
var Users = anttest.NewFactory(func(n int) *fixture.User {
	return &fixture.User{
		Name:  fmt.Sprintf("user %d", n),
		Email: fmt.Sprintf("user%d@example.com", n),
		Age:   30,
	}
})

// Posts is a factory for children.
var Posts = anttest.NewFactory(func(n int) *fixture.Post {
	return &fixture.Post{Title: fmt.Sprintf("post %d", n)}
})

// Isolate rolls a test back when it ends, so tests can share a database.
func TestIsolate(t *testing.T) {
	fixture.Run(t, func(t *testing.T, db *fixture.AppDB) {
		ctx := t.Context()
		t.Run("the test sees its own writes", func(t *testing.T) {
			tx := anttest.Isolate(t, db)
			require.NoError(t, tx.Users.Insert(ctx, Users.Build()))
			count, err := tx.Users.Count(ctx)
			require.NoError(t, err)
			require.EqualValues(t, 1, count)
		})

		count, err := db.Users.Count(ctx)
		require.NoError(t, err)
		require.Zero(t, count, "and they are gone afterwards")
	})
}

// Load inserts rows from JSON files named after their tables, parents first.
func TestFixtures(t *testing.T) {
	fixture.Run(t, func(t *testing.T, db *fixture.AppDB) {
		anttest.Load(t, db, data, "testdata/users.json", "testdata/posts.json")
		post, err := db.Posts.IncludeOne(&fixture.Posts.User).First(t.Context())
		require.NoError(t, err)
		require.Equal(t, "From a file", post.Title)
		require.Equal(t, "Ada", post.User.Name)
	})
}

// A factory makes valid entities, so a test says only what it cares about.
func TestFactory(t *testing.T) {
	fixture.Run(t, func(t *testing.T, db *fixture.AppDB) {
		ctx := t.Context()

		t.Run("Build touches no database", func(t *testing.T) {
			user := Users.Build()
			require.Zero(t, user.ID)
			require.Contains(t, user.Email, "@example.com")
		})

		t.Run("sequences keep values unique", func(t *testing.T) {
			many := Users.BuildMany(3)
			require.NotEqual(t, many[0].Email, many[1].Email)
		})

		t.Run("a state is a factory", func(t *testing.T) {
			retired := Users.With(func(u *fixture.User) { u.Age = 70 })
			named := retired.With(func(u *fixture.User) { u.Name = "Grace" })
			user := named.Create(t, &db.Users)
			require.NotZero(t, user.ID)
			require.Equal(t, 70, user.Age)
			require.Equal(t, 30, Users.Build().Age, "the base factory did not change")
		})

		t.Run("overrides apply last", func(t *testing.T) {
			user := Users.Create(t, &db.Users, func(u *fixture.User) { u.Age = 5 })
			require.Equal(t, 5, user.Age)
		})

		t.Run("related entities come along", func(t *testing.T) {
			user := Users.Create(t, &db.Users, func(u *fixture.User) {
				u.Posts = []fixture.Post{*Posts.Build(), *Posts.Build()}
			})
			count, err := db.Posts.Where(ant.Eq(&fixture.Posts.UserID, user.ID)).Count(ctx)
			require.NoError(t, err)
			require.EqualValues(t, 2, count)
		})

		t.Run("callbacks run after build and create", func(t *testing.T) {
			var order []string

			user := Users.
				AfterBuild(func(*fixture.User) { order = append(order, "built") }).
				AfterCreate(func(u *fixture.User) {
					require.NotZero(t, u.ID)

					order = append(order, "created")
				}).
				Create(t, &db.Users)
			require.NotZero(t, user.ID)
			require.Equal(t, []string{"built", "created"}, order)
		})

		t.Run("a recycled parent is shared", func(t *testing.T) {
			author := anttest.Recycle(Users, &db.Users)
			posts := Posts.With(func(p *fixture.Post) { p.UserID = author.Get(t).ID })
			created := posts.CreateMany(t, &db.Posts, 3)
			require.Equal(t, created[0].UserID, created[2].UserID)
		})
	})
}
