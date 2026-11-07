package loading

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/skudra-go/ant"
	"github.com/skudra-go/ant/tests/features/fixture"
)

// Parent and Child have an optional relation.
type Parent struct {
	ID   int64
	Name string
}

// Child may have no parent.
type Child struct {
	ID       int64
	Name     string
	ParentID *int64
	Parent   *Parent
}

// ChainDB holds them.
type ChainDB struct {
	ant.Context
	Children ant.Table[Child]
	Parents  ant.Table[Parent]
}

// CH is Child's probe.
var CH = ant.Fields[Child]()

const chainDDL = `
	CREATE TABLE parents (id BIGSERIAL PRIMARY KEY, name TEXT NOT NULL);
	CREATE TABLE childs (id BIGSERIAL PRIMARY KEY, name TEXT NOT NULL, parent_id BIGINT REFERENCES parents (id))`

// IncludeJoined reads a parent in the same statement, through a left join, instead
// of the extra one IncludeOne runs.
func TestIncludeJoined(t *testing.T) {
	fixture.Run(t, func(t *testing.T, db *fixture.AppDB) {
		ctx := t.Context()
		alice, _, _ := fixture.Seed(t, db)
		require.NoError(
			t,
			db.Posts.Insert(
				ctx,
				&fixture.Post{UserID: alice.ID, Title: "one"},
				&fixture.Post{UserID: alice.ID, Title: "two"},
			),
		)

		posts, err := db.Posts.IncludeJoined(&P.User).OrderBy(&P.Title).All(ctx)
		require.NoError(t, err)
		require.Len(t, posts, 2)
		require.Equal(t, "Alice", posts[0].User.Name)
		require.Equal(t, "one", posts[0].Title)

		t.Run("the join is in one statement", func(t *testing.T) {
			text, _, err := db.Posts.IncludeJoined(&P.User).ToSQL()
			require.NoError(t, err)
			require.Contains(t, text, `LEFT JOIN "users" AS "ant_joined0"`)
		})

		t.Run("it works with First and conditions", func(t *testing.T) {
			post, err := db.Posts.IncludeJoined(&P.User).Where(ant.Eq(&P.Title, "two")).First(ctx)
			require.NoError(t, err)
			require.Equal(t, alice.Email, post.User.Email)
		})
	})
}

// A parent the join did not find stays nil, and the row survives.
func TestIncludeJoinedWithNoParent(t *testing.T) {
	fixture.RunWith(t, chainDDL, func(t *testing.T, db *ChainDB) {
		ctx := t.Context()
		require.NoError(t, db.Children.Insert(ctx, &Child{Name: "orphan"}))
		children, err := db.Children.IncludeJoined(&CH.Parent).All(ctx)
		require.NoError(t, err)
		require.Len(t, children, 1)
		require.Nil(t, children[0].Parent)
	})
}

// Tracked rows work as with IncludeOne: the parent is tracked too, and changing it
// is saved.
func TestIncludeJoinedTracked(t *testing.T) {
	fixture.RunTracked(t, func(t *testing.T, db *fixture.AppDB) {
		ctx := t.Context()
		alice, _, _ := fixture.Seed(t, db)
		require.NoError(t, db.Posts.Insert(ctx, &fixture.Post{UserID: alice.ID, Title: "one"}))
		require.NoError(t, db.ClearTracked(ctx))
		post, err := db.Posts.IncludeJoined(&P.User).First(ctx)
		require.NoError(t, err)

		post.User.Age = 50

		require.NoError(t, db.SaveChanges(ctx))

		var age int
		require.NoError(
			t,
			db.DB().
				QueryRowContext(ctx, "SELECT age FROM users WHERE id = $1", alice.ID).
				Scan(&age),
		)

		require.Equal(t, 50, age)
	})
}

// NoTrackingResolved gives one row one instance, with no tracker to pay for.
func TestNoTrackingResolved(t *testing.T) {
	fixture.Run(t, func(t *testing.T, db *fixture.AppDB) {
		ctx := t.Context()
		alice, bob, _ := fixture.Seed(t, db)
		both := db.Users.Where(ant.In(&U.ID, alice.ID, bob.ID))
		twice := both.UnionAll(both).OrderBy(&U.Name)

		plain, err := twice.All(ctx)
		require.NoError(t, err)
		require.NotSame(t, plain[0], plain[1])

		resolved, err := twice.NoTrackingResolved().All(ctx)
		require.NoError(t, err)
		require.Same(t, resolved[0], resolved[1])
		require.NotSame(t, resolved[1], resolved[2])
	})
}
