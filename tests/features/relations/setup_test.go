package relations

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/skudra-go/ant"
	"github.com/skudra-go/ant/tests/features/fixture"
)

var (
	Users = fixture.Users
	Posts = fixture.Posts
)

// seed adds Alice with two posts, Bob with one, and Carol with none.
func seed(t *testing.T, db *fixture.AppDB) (alice, bob, carol *fixture.User) {
	t.Helper()
	alice, bob, carol = fixture.Seed(t, db)
	require.NoError(t, db.Posts.Insert(t.Context(),
		&fixture.Post{UserID: alice.ID, Title: "Hello", Published: true},
		&fixture.Post{UserID: alice.ID, Title: "Draft"},
		&fixture.Post{UserID: bob.ID, Title: "Bob's post", Published: true}))

	return alice, bob, carol
}

// titles is shorthand for a single-column query in the tests below.
func titles(t *testing.T, db *fixture.AppDB, q *ant.Query[fixture.Post]) []string {
	t.Helper()
	posts, err := q.All(t.Context())
	require.NoError(t, err)

	out := make([]string, len(posts))

	for i, p := range posts {
		out[i] = p.Title
	}

	return out
}
