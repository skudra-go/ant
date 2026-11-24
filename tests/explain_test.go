package tests

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/skudra-go/ant"
	"github.com/skudra-go/ant/internal/testdb"
)

// Explain reads each database's plan, and plans the statement All runs, joins
// of IncludeJoined included (#110). SQL Server has no EXPLAIN.
func TestConformanceExplain(t *testing.T) {
	testdb.ForEach(t, func(t *testing.T, target testdb.Target) {
		ctx := t.Context()

		jrTables(t, target)

		db, err := ant.Open[JrDB](ctx, target.Dialect, target.DSN, ant.WithTracking(false))
		require.NoError(t, err)
		t.Cleanup(func() { db.Close() })

		q := db.JrNotes.Where(ant.Gt(&JrNotes.Weight, int64(2)))
		joined := q.IncludeJoined(&JrNotes.Essay)

		if target.Name == "sqlserver" {
			_, err := q.Explain(ctx)
			require.ErrorIs(t, err, ant.ErrUnsupported)
			require.ErrorContains(t, err, "EXPLAIN on sqlserver")

			return
		}

		plan, err := q.Explain(ctx)
		require.NoError(t, err)
		require.Contains(t, plan, "jr_notes")
		require.NotContains(t, plan, "ant_joined0")

		plan, err = joined.Explain(ctx)
		require.NoError(t, err)
		require.Contains(t, plan, "ant_joined0", "the plan of the statement with the join")

		text, args, err := joined.ToSQL()
		require.NoError(t, err)
		require.Contains(t, text, "ant_joined0")
		require.NotEmpty(t, args)
	})
}
