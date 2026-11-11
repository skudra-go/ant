package change_tracking

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/skudra-go/ant"
	"github.com/skudra-go/ant/tests/features/fixture"
)

type (
	User  = fixture.User
	Post  = fixture.Post
	AppDB = fixture.AppDB
)

var (
	Users = fixture.Users
	Posts = fixture.Posts
)

// stored reads a user straight from the database, past the tracker.
func stored(t *testing.T, db *AppDB, id int64) *User {
	t.Helper()
	user, err := db.Users.NoTracking().Find(t.Context(), id)
	require.NoError(t, err)

	return user
}

// storedTitles reads a user's post titles straight from the database.
func storedTitles(t *testing.T, db *AppDB, id int64) []string {
	t.Helper()

	posts, err := db.Posts.NoTracking().
		Where(ant.Eq(&Posts.UserID, id)).
		OrderBy(&Posts.Title).
		All(t.Context())
	require.NoError(t, err)

	out := make([]string, len(posts))

	for i, p := range posts {
		out[i] = p.Title
	}

	return out
}

// loaded returns Alice from a fresh session, with her posts A, B, and C.
func loaded(t *testing.T, db *AppDB) (*AppDB, *User) {
	t.Helper()
	ctx := t.Context()
	alice, _, _ := fixture.Seed(t, db)
	require.NoError(t, db.Posts.InsertOnly(ctx,
		&Post{UserID: alice.ID, Title: "A"},
		&Post{UserID: alice.ID, Title: "B"},
		&Post{UserID: alice.ID, Title: "C"}))

	session := ant.Session(db)
	user, err := session.Users.Include(&Users.Posts, func(q *ant.Query[Post]) *ant.Query[Post] {
		return q.OrderBy(&Posts.Title)
	}).Find(ctx, alice.ID)
	require.NoError(t, err)

	return session, user
}
