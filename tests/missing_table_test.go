package tests

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/skudra-go/ant"
	"github.com/skudra-go/ant/internal/testdb"
)

// Ghost is a model whose table is never created.
type Ghost struct {
	ID   int64
	Name string
}

// GhostDB opens a context on a database that has no ghosts table.
type GhostDB struct {
	ant.Context
	Ghosts ant.Table[Ghost]
}

// A query or write on a table that does not exist yet says how tables are made,
// on every database: the drivers' own messages, relation "ghosts" does not
// exist, Table 'x.ghosts' doesn't exist, no such table and Invalid object name,
// said nothing of migrations or EnsureCreated. SQLState gives one code for it
// everywhere (#301).
func TestConformanceMissingTableSaysHowToCreateIt(t *testing.T) {
	testdb.ForEach(t, func(t *testing.T, target testdb.Target) {
		ctx := t.Context()

		db, err := ant.Open[GhostDB](ctx, target.Dialect, target.DSN)
		require.NoError(t, err)
		t.Cleanup(func() { db.Close() })

		_, read := db.Ghosts.All(ctx)
		write := db.Ghosts.Insert(ctx, &Ghost{Name: "a"})

		for _, err := range []error{read, write} {
			require.Error(t, err)
			require.ErrorContains(t, err, "ghosts")
			require.ErrorContains(t, err, "the table does not exist")
			require.ErrorContains(t, err, "EnsureCreated")
			require.Equal(t, "42P01", ant.SQLState(err))
		}
	})
}
