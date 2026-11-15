package tests

import (
	"embed"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/skudra-go/ant/anttest"
	"github.com/skudra-go/ant/internal/testdb"
)

// Times are written without T and Z, which MySQL and MariaDB do not read.
//
//go:embed testdata/anttest
var fixtures embed.FS

// Load writes plain INSERTs, quoted the way the context's database quotes.
func TestConformanceAnttestLoad(t *testing.T) {
	testdb.ForEach(t, func(t *testing.T, target testdb.Target) {
		if target.Dialect.Name() == "sqlserver" {
			// The files give ids, which an IDENTITY column refuses until
			// IDENTITY_INSERT is on for the table.
			t.Skip("Load cannot write explicit keys on SQL Server")
		}

		// In a transaction, which is how Isolate hands a context over.
		app := anttest.Isolate(t, openApp(t, target))

		anttest.Load(
			t,
			app,
			fixtures,
			"testdata/anttest/users.json",
			"testdata/anttest/posts.json",
		)

		post, err := app.Posts.IncludeOne(&Posts.User).First(t.Context())
		require.NoError(t, err)
		require.Equal(t, "From a file", post.Title)
		require.True(t, post.Published)
		require.Equal(t, "Ada", post.User.Name)
		require.Equal(t, 36, post.User.Age)
		require.Equal(t, 2026, post.User.CreatedAt.UTC().Year())
	})
}
