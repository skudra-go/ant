package benchmarks

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/skudra-go/ant"
	"github.com/skudra-go/ant/dialect/postgres"
)

// The budgets below are for whole operations on the fake driver, which makes
// them exact: the same count on every run and every machine. Each operation is
// measured at two sizes, and the difference gives what one more row costs,
// apart from what the operation costs however many rows it has. A budget
// includes what database/sql and the fake allocate, which is three per row
// read: the key and the two times, each put into an interface.
//
// They are what keeps the work of #138 done. Raise one only on purpose.

const (
	budgetSmall = 100
	budgetLarge = 1100
)

// perRow returns what run allocates for each row past the first, and for the
// operation apart from its rows.
func perRow(run func(size int)) (row, fixed float64) {
	small := testing.AllocsPerRun(10, func() { run(budgetSmall) })
	large := testing.AllocsPerRun(10, func() { run(budgetLarge) })

	row = (large - small) / (budgetLarge - budgetSmall)

	return row, small - row*budgetSmall
}

func TestRowAllocationBudgets(t *testing.T) {
	db, raw, f := fakeDB(t, ant.WithTracking(false))
	ctx := context.Background()

	tracked, err := ant.Wrap[AppDB](postgres.Dialect, raw)
	require.NoError(t, err)

	saved := map[int]*AppDB{}

	for _, size := range []int{budgetSmall, budgetLarge} {
		f.rows.Store(int64(size))

		saved[size] = ant.Session(tracked)

		_, err := saved[size].Users.All(ctx)
		require.NoError(t, err)
	}

	inserted := make([]*User, budgetLarge)

	cases := []struct {
		name  string
		row   float64 // for each row
		fixed float64 // for the operation
		run   func(size int)
	}{
		{"read", 3.1, 25, func(size int) {
			f.rows.Store(int64(size))

			out, err := db.Users.All(ctx)
			if err != nil || len(out) != size {
				t.Fatal(err, len(out))
			}
		}},
		// Tracking a row costs nothing that grows with the rows: entries and
		// snapshots come in blocks, and an integer key is held unboxed.
		{"read tracked", 3.1, 65, func(size int) {
			f.rows.Store(int64(size))

			out, err := ant.Session(tracked).Users.All(ctx)
			if err != nil || len(out) != size {
				t.Fatal(err, len(out))
			}
		}},
		{"stream", 3.1, 20, func(size int) {
			f.rows.Store(int64(size))

			n := 0

			for _, err := range db.Users.Each(ctx) {
				if err != nil {
					t.Fatal(err)
				}

				n++
			}

			if n != size {
				t.Fatal(n)
			}
		}},
		// Two queries, so twice the rows of the driver, and the key of each
		// parent and of each child.
		{"include", 9.1, 85, func(size int) {
			f.rows.Store(int64(size))

			out, err := db.Users.Include(&Users.Posts).All(ctx)
			if err != nil || len(out) != size || len(out[0].Posts) != 1 {
				t.Fatal(err, len(out))
			}
		}},
		// A save looks at every tracked entity and allocates for none of them.
		{"save with nothing changed", 0.01, 20, func(size int) {
			if err := saved[size].SaveChanges(ctx); err != nil {
				t.Fatal(err)
			}
		}},
		{"save with one changed", 0.01, 55, func(size int) {
			first, err := saved[size].Users.Find(ctx, int64(fakeFirstID))
			if err != nil {
				t.Fatal(err)
			}

			first.Age++

			if err := saved[size].SaveChanges(ctx); err != nil {
				t.Fatal(err)
			}
		}},
		// The entity itself, and its two times and two strings, each put into
		// an interface for the driver.
		{"insert many", 7.1, 60, func(size int) {
			for i := range size {
				inserted[i] = &User{Name: "x", Email: "x", Age: 1}
			}

			if _, err := db.Users.InsertMany(ctx, inserted[:size]); err != nil {
				t.Fatal(err)
			}
		}},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			row, fixed := perRow(c.run)
			t.Logf(
				"%.2f for each row, budget %.2f; %.0f apart from rows, budget %.0f",
				row,
				c.row,
				fixed,
				c.fixed,
			)

			require.LessOrEqual(t, row, c.row)
			require.LessOrEqual(t, fixed, c.fixed)
		})
	}
}

func TestQueryAllocationBudgets(t *testing.T) {
	db, raw, f := fakeDB(t, ant.WithTracking(false))
	ctx := context.Background()

	tracked, err := ant.Wrap[AppDB](postgres.Dialect, raw)
	require.NoError(t, err)

	f.rows.Store(1)

	cases := []struct {
		name   string
		budget float64
		run    func()
	}{
		{"find by key", 22, func() {
			if _, err := db.Users.Find(ctx, int64(500)); err != nil {
				t.Fatal(err)
			}
		}},
		{"first of a filtered and ordered query", 33, func() {
			if _, err := db.Users.Where(ant.Eq(&Users.Age, 30), ant.Like(&Users.Name, "user%")).
				OrderBy(&Users.Name).
				First(ctx); err != nil {
				t.Fatal(err)
			}
		}},
		{"first, tracked, in a new session", 46, func() {
			if _, err := ant.Session(tracked).
				Users.Where(ant.Eq(&Users.Age, 30)).
				First(ctx); err != nil {
				t.Fatal(err)
			}
		}},
		{"insert", 25, func() {
			if err := db.Users.Insert(ctx, &User{Name: "x", Email: "x", Age: 1}); err != nil {
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
