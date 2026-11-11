// Package fixture holds the models and setup shared by the feature examples.
package fixture

import (
	"context"
	"database/sql"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/skudra-go/ant"
	"github.com/skudra-go/ant/internal/testdb"
)

// User is the main example model.
type User struct {
	ant.Base
	Name  string
	Email string `ant:"unique"`
	Age   int
	Bio   *string
	Posts []Post
}

// Post is a model with a foreign key to User.
type Post struct {
	ant.Base
	UserID    int64
	Title     string
	Published bool
	User      *User
}

// AppDB is the example database context.
type AppDB struct {
	ant.Context
	Users ant.Table[User]
	Posts ant.Table[Post]
}

// Field probes for the example models.
var (
	Users = ant.Fields[User]()
	Posts = ant.Fields[Post]()
)

// Target is a database under test.
type Target = testdb.Target

// Each runs fn on PostgreSQL, because the features' tables are written in its
// DDL. tests/conformance_test.go covers what differs on the other databases.
func Each(t *testing.T, fn func(t *testing.T, target testdb.Target)) {
	t.Helper()
	testdb.ForOnly(t, []string{"postgres"}, fn)
}

// Open opens DB on target and closes it when the test ends. The examples read
// what the database holds, so tracking is off here unless a test asks for it.
func Open[DB any](t *testing.T, target testdb.Target, options ...ant.Option) *DB {
	t.Helper()

	db, err := ant.Open[DB](
		t.Context(),
		target.Dialect,
		target.DSN,
		append([]ant.Option{ant.WithTracking(false)}, options...)...)
	require.NoError(t, err)
	// Every test proves it left no connection in use, so a leaked rows or
	// transaction fails the test that caused it.
	h := any(db).(interface {
		Close() error
		DB() *sql.DB
	})

	t.Cleanup(func() {
		if inUse := h.DB().Stats().InUse; inUse != 0 {
			t.Errorf("%d connections still in use", inUse)
		}

		h.Close()
	})

	return db
}

// Run gives fn an AppDB with empty users and posts tables.
func Run(t *testing.T, fn func(t *testing.T, db *AppDB)) {
	t.Helper()

	Each(t, func(t *testing.T, target testdb.Target) {
		fn(t, Open[AppDB](t, target))
	})
}

// RunTracked is Run with change tracking on, as it is in applications.
func RunTracked(t *testing.T, fn func(t *testing.T, db *AppDB)) {
	t.Helper()

	Each(t, func(t *testing.T, target testdb.Target) {
		fn(t, Open[AppDB](t, target, ant.WithTracking(true)))
	})
}

// RunWith runs ddl, then gives fn an open DB. Use it for models with their own
// table.
func RunWith[DB any](t *testing.T, ddl string, fn func(t *testing.T, db *DB)) {
	t.Helper()
	runWith(t, ddl, fn)
}

// RunTrackedWith is RunWith with change tracking on.
func RunTrackedWith[DB any](t *testing.T, ddl string, fn func(t *testing.T, db *DB)) {
	t.Helper()
	runWith(t, ddl, fn, ant.WithTracking(true))
}

func runWith[DB any](
	t *testing.T,
	ddl string,
	fn func(t *testing.T, db *DB),
	options ...ant.Option,
) {
	t.Helper()

	Each(t, func(t *testing.T, target testdb.Target) {
		_, err := target.DB.ExecContext(t.Context(), ddl)
		require.NoError(t, err)
		fn(t, Open[DB](t, target, options...))
	})
}

// Seed inserts Alice (30, no bio), Bob (17, no bio), and Carol (40, with a bio)
// and returns them.
func Seed(t *testing.T, db *AppDB) (alice, bob, carol *User) {
	t.Helper()

	alice = user("Alice", 30, nil)
	bob = user("Bob", 17, nil)
	carol = user("Carol", 40, new("Gopher"))
	require.NoError(t, db.Users.Insert(t.Context(), alice, bob, carol))

	return alice, bob, carol
}

func user(name string, age int, bio *string) *User {
	return &User{
		Name:  name,
		Email: fmt.Sprintf("%s@example.com", name),
		Age:   age,
		Bio:   bio,
	}
}

// Names returns the names of users, in order, from []*User or []User.
func Names[S ~[]E, E any](users S) []string {
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

// HasChanges reports whether db has anything to save, failing the test on an
// error.
func HasChanges(t *testing.T, db ant.Handle) bool {
	t.Helper()

	changed, err := db.(interface {
		HasChanges(ctx context.Context) (bool, error)
	}).HasChanges(t.Context())
	require.NoError(t, err)

	return changed
}

// Entry returns an entity's tracking state, failing the test on an error.
func Entry[T any](t *testing.T, db ant.Handle, entity *T) *ant.EntityEntry[T] {
	t.Helper()

	entry, err := ant.Entry(t.Context(), db, entity)
	require.NoError(t, err)

	return entry
}

// Tracked returns the tracked entities of type T, failing the test on an error.
func Tracked[T any](t *testing.T, db ant.Handle) []*T {
	t.Helper()

	out, err := ant.Tracked[T](t.Context(), db)
	require.NoError(t, err)

	return out
}

// DebugTracked describes what db tracks, for a failure message.
func DebugTracked(t *testing.T, db ant.Handle) string {
	t.Helper()

	out, err := db.(interface {
		DebugTracked(ctx context.Context) (string, error)
	}).DebugTracked(t.Context())
	require.NoError(t, err)

	return out
}
