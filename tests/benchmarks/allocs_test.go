package benchmarks

import (
	"database/sql"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/skudra-go/ant"
	"github.com/skudra-go/ant/dialect/postgres"
)

// The budgets below fail a test if building a query gets more expensive, which is
// how an accidental allocation in a hot path gets noticed. Raise one only on
// purpose. They need no database: nothing here connects.

func unconnected(t testing.TB) *AppDB {
	t.Helper()

	pool, err := sql.Open("pgx", "postgres://nobody@127.0.0.1:1/none")
	require.NoError(t, err)
	t.Cleanup(func() { pool.Close() })

	db, err := ant.Wrap[AppDB](postgres.Dialect, pool, ant.WithTracking(false))
	require.NoError(t, err)

	return db
}

func TestAllocationBudgets(t *testing.T) {
	db := unconnected(t)

	cases := []struct {
		name   string
		budget float64
		run    func()
	}{
		{"a condition", 5, func() { _ = ant.Eq(&Users.Age, 18) }},
		{"building a query", 35, func() {
			_ = db.Users.Where(ant.Eq(&Users.Age, 18), ant.Like(&Users.Name, "A%")).
				OrderBy(&Users.Name).
				Limit(10)
		}},
		{"building and rendering a query", 85, func() {
			q := db.Users.Where(ant.Eq(&Users.Age, 18), ant.Like(&Users.Name, "A%")).
				OrderBy(&Users.Name).
				Limit(10)

			if _, _, err := q.ToSQL(); err != nil {
				t.Fatal(err)
			}
		}},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := testing.AllocsPerRun(200, c.run)
			t.Logf("%.0f allocations, budget %.0f", got, c.budget)
			require.LessOrEqual(t, got, c.budget)
		})
	}
}

// BenchmarkBuildQuery measures building and rendering with no database.
func BenchmarkBuildQuery(b *testing.B) {
	db := unconnected(b)
	b.ReportAllocs()

	for b.Loop() {
		q := db.Users.Where(ant.Eq(&Users.Age, 18), ant.Like(&Users.Name, "A%")).
			OrderBy(&Users.Name).
			Limit(10)

		if _, _, err := q.ToSQL(); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkProbeResolution measures turning a field pointer into its column,
// which every condition does.
func BenchmarkProbeResolution(b *testing.B) {
	b.ReportAllocs()

	for b.Loop() {
		_ = ant.F(&Users.Age)
	}
}
