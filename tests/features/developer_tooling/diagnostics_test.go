package developer_tooling

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/skudra-go/ant"
	"github.com/skudra-go/ant/tests/features/fixture"
)

// DryRun runs real code in a transaction, shows every statement, and leaves
// nothing behind.
func TestDryRun(t *testing.T) {
	fixture.RunTracked(t, func(t *testing.T, db *fixture.AppDB) {
		ctx := t.Context()
		statements, err := ant.DryRun(ctx, db, func(tx *fixture.AppDB) error {
			return tx.Users.Insert(ctx, &fixture.User{Name: "Temp", Email: "temp@example.com"})
		})
		require.NoError(t, err)
		require.Len(t, statements, 1)
		require.Contains(t, statements[0].SQL, `INSERT INTO "users"`)

		count, err := db.Users.Count(ctx)
		require.NoError(t, err)
		require.Zero(t, count)

		t.Run("an error comes back, with the statements so far", func(t *testing.T) {
			wrong := errors.New("wrong")
			statements, err := ant.DryRun(ctx, db, func(tx *fixture.AppDB) error {
				_, err := tx.Users.Count(ctx)
				require.NoError(t, err)

				return wrong
			})
			require.ErrorIs(t, err, wrong)
			require.Len(t, statements, 1)
		})
	})
}

type alerts struct {
	mu  sync.Mutex
	got []ant.Alert
}

func (a *alerts) add(_ context.Context, alert ant.Alert) {
	a.mu.Lock()
	defer a.mu.Unlock()

	a.got = append(a.got, alert)
}

func (a *alerts) kinds() []string {
	a.mu.Lock()
	defer a.mu.Unlock()

	out := []string{}

	for _, alert := range a.got {
		out = append(out, alert.Kind)
	}

	return out
}

// Repeating one statement in a request looks like N+1, and Diagnostics says so.
func TestDiagnostics(t *testing.T) {
	fixture.Each(t, func(t *testing.T, target fixture.Target) {
		ctx := t.Context()
		found := &alerts{}
		db := fixture.Open[fixture.AppDB](t, target, ant.WithDiagnostics(ant.Diagnostics{
			RepeatLimit: 3,
			Budget:      time.Hour,
			Alert:       found.add,
		}))
		alice, _, _ := fixture.Seed(t, db)

		t.Run("repeats alert once, inside a scope", func(t *testing.T) {
			scope := ant.Watch(ctx)

			for range 6 {
				_, err := db.Users.Where(ant.Eq(&U.ID, alice.ID)).First(scope)
				require.NoError(t, err)
			}

			require.Equal(t, []string{"repeated"}, found.kinds())
		})

		t.Run("without a scope nothing counts", func(t *testing.T) {
			before := len(found.kinds())

			for range 6 {
				_, err := db.Users.Where(ant.Eq(&U.ID, alice.ID)).First(ctx)
				require.NoError(t, err)
			}

			require.Len(t, found.kinds(), before)
		})
	})
}

// A budget alerts on total time, which no single slow query shows.
func TestQueryBudget(t *testing.T) {
	fixture.Each(t, func(t *testing.T, target fixture.Target) {
		ctx := t.Context()
		found := &alerts{}
		db := fixture.Open[fixture.AppDB](
			t,
			target,
			ant.WithDiagnostics(ant.Diagnostics{Budget: time.Nanosecond, Alert: found.add}),
		)

		scope := ant.Watch(ctx)
		_, err := db.Users.Count(scope)
		require.NoError(t, err)
		_, err = db.Users.Count(scope)
		require.NoError(t, err)
		require.Equal(t, []string{"budget"}, found.kinds())
	})
}

// Joining two to-many relations multiplies rows, which is rarely what was meant.
func TestCartesianWarning(t *testing.T) {
	fixture.Each(t, func(t *testing.T, target fixture.Target) {
		ctx := t.Context()
		found := &alerts{}
		db := fixture.Open[fixture.AppDB](
			t,
			target,
			ant.WithDiagnostics(ant.Diagnostics{Alert: found.add}),
		)
		_, err := db.Users.JoinMany(&U.Posts).
			JoinMany(&U.Posts).
			Select(func(*fixture.User, *fixture.Post, *fixture.Post) int { return 0 }).
			All(ctx)
		require.NoError(t, err)
		require.Equal(t, []string{"cartesian"}, found.kinds())
	})
}

// A query error names the statement on request, and keeps the driver's error.
func TestDetailedErrors(t *testing.T) {
	fixture.Each(t, func(t *testing.T, target fixture.Target) {
		ctx := t.Context()

		t.Run("FailedSQL reads it from any query error", func(t *testing.T) {
			db := fixture.Open[fixture.AppDB](t, target)
			alice, _, _ := fixture.Seed(t, db)
			err := db.Users.Insert(ctx, &fixture.User{Name: "Dup", Email: alice.Email})
			require.True(t, ant.IsUniqueViolation(err), "the driver's error is still there")
			require.Contains(t, ant.FailedSQL(err), `INSERT INTO "users"`)
			require.NotContains(t, err.Error(), "INSERT", "not in the message unless asked")
		})

		t.Run("WithDetailedErrors adds it to the message", func(t *testing.T) {
			db := fixture.Open[fixture.AppDB](t, target, ant.WithDetailedErrors())
			_, err := ant.Raw[int](db, "SELECT nothing_here").All(ctx)
			require.ErrorContains(t, err, "[sql: SELECT nothing_here]")
		})
	})
}

// Debug shows one query's SQL and values, for a moment at a desk.
func TestDebugQuery(t *testing.T) {
	fixture.Run(t, func(t *testing.T, db *fixture.AppDB) {
		var logs bytes.Buffer

		previous := slog.Default()

		slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
		defer slog.SetDefault(previous)

		_, err := db.Users.Where(ant.Eq(&U.Name, "Alice")).Debug().All(t.Context())
		require.NoError(t, err)
		require.Contains(t, logs.String(), "SELECT")
		require.Contains(t, logs.String(), "Alice")
	})
}

// Events watch the unit of work as a whole.
func TestEvents(t *testing.T) {
	fixture.Each(t, func(t *testing.T, target fixture.Target) {
		ctx := t.Context()

		var (
			mu  sync.Mutex
			log []string
		)

		note := func(s string) {
			mu.Lock()
			defer mu.Unlock()

			log = append(log, s)
		}
		db := fixture.Open[fixture.AppDB](
			t,
			target,
			ant.WithTracking(true),
			ant.WithEvents(ant.Events{
				Saving:     func(_ context.Context, c []ant.Change) { note("saving") },
				Saved:      func(_ context.Context, c []ant.Change) { note("saved") },
				SaveFailed: func(context.Context, error) { note("failed") },
				Tracked:    func(model string, _ any) { note("tracked " + model) },
				StateChanged: func(_ string, _ any, from, to ant.State) {
					note(from.String() + " to " + to.String())
				},
			}),
		)
		alice, _, _ := fixture.Seed(t, db)
		require.Contains(t, log, "saving")
		require.Contains(t, log, "saved")
		log = nil

		loaded, err := db.Users.Find(ctx, alice.ID)
		require.NoError(t, err)
		require.Empty(t, log, "the same instance was already tracked")
		require.NoError(t, ant.Remove(ctx, db, loaded))
		require.Equal(t, []string{"Unchanged to Deleted"}, log)
		log = nil

		loaded.Email = ""

		require.NoError(t, db.SaveChanges(ctx))
		require.Equal(t, []string{"saving", "saved"}, log)

		t.Run("a failed save says so", func(t *testing.T) {
			log = nil

			require.NoError(
				t,
				ant.Add(ctx, db, &fixture.User{Name: "A", Email: "same@example.com"}),
			)

			require.NoError(
				t,
				ant.Add(ctx, db, &fixture.User{Name: "B", Email: "same@example.com"}),
			)

			require.Error(t, db.SaveChanges(ctx))
			require.Contains(t, log, "failed")
			require.NoError(t, db.ClearTracked(ctx))
		})
	})
}
