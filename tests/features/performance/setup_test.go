package performance

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/skudra-go/ant/tests/features/fixture"
)

type User = fixture.User

var Users = fixture.Users

// seed adds n users named u000, u001, and so on, in order.
func seed(t *testing.T, db *fixture.AppDB, n int) {
	t.Helper()

	users := make([]*User, n)
	for i := range users {
		name := fmt.Sprintf("u%03d", i)
		users[i] = &User{Name: name, Email: name, Age: i % 3}
	}

	_, err := db.Users.InsertMany(t.Context(), users)
	require.NoError(t, err)
}
