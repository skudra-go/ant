package change_tracking

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/skudra-go/ant"
	"github.com/skudra-go/ant/tests/features/fixture"
)

// A session holds one instance per row, so a post's author is the same *User
// that holds the post. The graph points back at itself, and encoding/json
// refuses it. Encode a NoTracking read, a DTO, or tag the back reference
// json:"-".
func TestEncodeATrackedGraph(t *testing.T) {
	fixture.Run(t, func(t *testing.T, db *AppDB) {
		alice, _, _ := fixture.Seed(t, db)
		ctx := t.Context()
		require.NoError(t, db.Posts.InsertOnly(ctx, &Post{UserID: alice.ID, Title: "A"}))

		authors := func(q *ant.Query[Post]) *ant.Query[Post] {
			return q.IncludeOne(&Posts.User)
		}
		session := ant.Session(db)

		t.Run("a tracked graph is a cycle", func(t *testing.T) {
			users, err := session.Users.Include(&Users.Posts, authors).Find(ctx, alice.ID)
			require.NoError(t, err)
			require.Same(t, users, users.Posts[0].User)

			_, err = json.Marshal(users)
			require.ErrorContains(t, err, "cycle")
		})

		t.Run("a NoTracking read is a tree", func(t *testing.T) {
			users, err := session.Users.NoTracking().Include(&Users.Posts, authors).Find(ctx, alice.ID)
			require.NoError(t, err)
			require.NotSame(t, users, users.Posts[0].User)

			_, err = json.Marshal(users)
			require.NoError(t, err)
		})
	})
}
