package loading

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/skudra-go/ant/tests/features/fixture"
)

type (
	User = fixture.User
	Post = fixture.Post
)

var (
	Users = fixture.Users
	Posts = fixture.Posts
)

// seed adds Alice with posts A1, A2, and A3, Bob with B1, and Carol with none.
// Only A1 and B1 are published.
func seed(t *testing.T, db *fixture.AppDB) (alice, bob, carol *User) {
	t.Helper()
	alice, bob, carol = fixture.Seed(t, db)
	require.NoError(t, db.Posts.Insert(t.Context(),
		&Post{UserID: alice.ID, Title: "A1", Published: true},
		&Post{UserID: alice.ID, Title: "A2"},
		&Post{UserID: alice.ID, Title: "A3"},
		&Post{UserID: bob.ID, Title: "B1", Published: true}))

	return alice, bob, carol
}

func titles(posts []Post) []string {
	out := make([]string, len(posts))

	for i, p := range posts {
		out[i] = p.Title
	}

	return out
}
