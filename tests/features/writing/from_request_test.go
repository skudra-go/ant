package writing

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/skudra-go/ant"
	"github.com/skudra-go/ant/tests/features/fixture"
)

// NewPost holds the fields a client may set when it creates a post.
type NewPost struct {
	Title string
}

// A body decoded straight into a model can set its key, its timestamps, and its
// relations, which Insert inserts too. Decode into an input struct of the fields
// the client may set, and copy them into the model.
func TestWriteFromARequest(t *testing.T) {
	fixture.Run(t, func(t *testing.T, db *fixture.AppDB) {
		alice, _, _ := fixture.Seed(t, db)
		ctx := t.Context()
		body := `{
			"Title": "Mine",
			"ID": 77,
			"UserID": 1,
			"User": {"Name": "Evil", "Email": "evil@example.com"}
		}`

		var in NewPost
		require.NoError(t, json.Unmarshal([]byte(body), &in))

		post := &fixture.Post{
			UserID: alice.ID, // from the session, not the body
			Title:  in.Title,
		}
		require.NoError(t, db.Posts.Insert(ctx, post))
		require.NotEqual(t, int64(77), post.ID)

		evil, err := db.Users.Where(ant.Eq(&Users.Email, "evil@example.com")).Count(ctx)
		require.NoError(t, err)
		require.Zero(t, evil)
	})
}
