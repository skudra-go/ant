package tests

import (
	"database/sql"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/skudra-go/ant"
	"github.com/skudra-go/ant/internal/testdb"
)

type User struct {
	ant.Base
	Name  string
	Email string `ant:"unique"`
	Age   int
	Bio   *string
	Posts []Post
}

type Post struct {
	ant.Base
	UserID    int64
	Title     string
	Published bool
	User      *User
}

type AppDB struct {
	ant.Context
	Users ant.Table[User]
	Posts ant.Table[Post]
}

func (User) Configure(b *ant.ModelBuilder[User]) {
	b.Table("users")
	b.Index(&Users.Email).Unique()
	b.HasMany(&Users.Posts).ForeignKey(&Posts.UserID).OnDelete(ant.Cascade)
}

var (
	Users = ant.Fields[User]()
	Posts = ant.Fields[Post]()
)

var _ ant.Entity[User] = User{}

func openApp(t *testing.T, target testdb.Target) *AppDB {
	t.Helper()
	app, err := ant.Open[AppDB](t.Context(), target.Dialect, target.DSN)
	require.NoError(t, err)
	t.Cleanup(func() { app.Close() })

	return app
}

// seedUsers inserts users from alternating name and age arguments.
func seedUsers(t *testing.T, app *AppDB, pairs ...any) []*User {
	t.Helper()

	var users []*User

	for i := 0; i < len(pairs); i += 2 {
		name := pairs[i].(string)
		u := &User{Name: name, Email: fmt.Sprintf("%s@example.com", name), Age: pairs[i+1].(int)}
		require.NoError(t, app.Users.Insert(t.Context(), u))
		users = append(users, u)
	}

	return users
}

func names[S ~[]E, E any](users S) []string {
	out := make([]string, 0, len(users))

	for _, u := range users {
		switch u := any(u).(type) {
		case *User:
			out = append(out, u.Name)
		case User:
			out = append(out, u.Name)
		}
	}

	return out
}

func titles(posts []Post) []string {
	out := make([]string, 0, len(posts))

	for _, p := range posts {
		out = append(out, p.Title)
	}

	return out
}

// countRows reads through database/sql, independently of ant.
func countRows(t *testing.T, db *sql.DB, table string) int {
	t.Helper()

	var n int
	require.NoError(t, db.QueryRowContext(t.Context(), "SELECT COUNT(*) FROM "+table).Scan(&n))

	return n
}
