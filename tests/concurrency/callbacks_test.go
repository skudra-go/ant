package concurrency

import (
	"bytes"
	"context"
	"database/sql"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/skudra-go/ant"
	"github.com/skudra-go/ant/internal/testdb"
	"github.com/skudra-go/ant/metrics"
)

// Hook counts, kept outside the entities, which each goroutine has its own of.
var (
	ticketInserts atomic.Int64
	ticketLoads   atomic.Int64
)

func (t *Ticket) BeforeInsert(context.Context) error {
	t.Title = strings.ToUpper(t.Title)

	return nil
}

func (t *Ticket) AfterInsert(context.Context) error {
	ticketInserts.Add(1)

	return nil
}

func (t *Ticket) AfterLoad(ctx context.Context) error {
	t.Loads++

	ticketLoads.Add(1)

	if hook := ticketHook.Load(); hook != nil {
		(*hook)(ctx)
	}

	return nil
}

// ticketHook, if set, runs in AfterLoad, with the load's context.
var ticketHook atomic.Pointer[func(context.Context)]

// Hooks run on the goroutine that writes or loads, once per entity, whether
// the workers share a context or each have a session.
func TestHooksOnManyGoroutines(t *testing.T) {
	each(t, func(t *testing.T, target testdb.Target) {
		db := shared(t, target)
		ctx := t.Context()

		const n, rounds = 12, 5

		ticketInserts.Store(0)
		ticketLoads.Store(0)

		errs := workers(n, func(w int) error {
			for i := range rounds {
				ticket := &Ticket{Title: fmt.Sprintf("ticket %d-%d", w, i)}

				if w%2 == 0 {
					if err := db.Tickets.Insert(ctx, ticket); err != nil {
						return err
					}
				} else {
					session := ant.Session(db, ant.WithTracking(true))
					if err := ant.Add(ctx, session, ticket); err != nil {
						return err
					}

					if err := session.SaveChanges(ctx); err != nil {
						return err
					}
				}

				if ticket.Title != strings.ToUpper(ticket.Title) {
					return fmt.Errorf("BeforeInsert did not run on %q", ticket.Title)
				}

				got, err := db.Tickets.Find(ctx, ticket.ID)
				if err != nil {
					return err
				}

				if got.Loads != 1 {
					return fmt.Errorf("AfterLoad ran %d times on one load", got.Loads)
				}
			}

			return nil
		})
		noErrors(t, errs)
		require.EqualValues(t, n*rounds, ticketInserts.Load())
		require.EqualValues(t, n*rounds, ticketLoads.Load())
	})
}

// Observers and events run for every committed write, on whichever goroutine
// made it, and an observer may be registered while other goroutines write.
func TestObserversAndEvents(t *testing.T) {
	each(t, func(t *testing.T, target testdb.Target) {
		var saving, saved, tracked atomic.Int64

		db := shared(t, target, ant.WithEvents(ant.Events{
			Saving:  func(context.Context, []ant.Change) { saving.Add(1) },
			Saved:   func(context.Context, []ant.Change) { saved.Add(1) },
			Tracked: func(string, any) { tracked.Add(1) },
		}))

		ctx := t.Context()

		const n, rounds = 8, 4

		var inserted, late atomic.Int64

		require.NoError(t, ant.Observe(db, ant.Observer[Account]{
			Inserted: func(context.Context, *Account) { inserted.Add(1) },
		}))

		errs := workers(n, func(w int) error {
			if w == 0 {
				// Registered while the others write: it sees some of their
				// inserts, and the registration itself must not race them.
				return ant.Observe(db, ant.Observer[Account]{
					Inserted: func(context.Context, *Account) { late.Add(1) },
				})
			}

			for i := range rounds {
				session := ant.Session(db, ant.WithTracking(true))
				if err := ant.Add(ctx,
					session,
					&Account{Owner: fmt.Sprintf("observed-%d-%d", w, i)},
				); err != nil {
					return err
				}

				if err := session.SaveChanges(ctx); err != nil {
					return err
				}
			}

			return nil
		})
		noErrors(t, errs)

		const writes = (n - 1) * rounds

		require.EqualValues(t, writes, inserted.Load())
		require.LessOrEqual(t, late.Load(), int64(writes))
		require.EqualValues(t, writes, saving.Load())
		require.EqualValues(t, writes, saved.Load())
		require.EqualValues(t, writes, tracked.Load())
	})
}

// lockedWriter makes a writer safe for a logger that goroutines share.
type lockedWriter struct {
	mu sync.Mutex
	w  io.Writer
}

func (l *lockedWriter) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()

	return l.w.Write(p)
}

// counting counts statements by worker, as a tracing interceptor would keep
// spans, and marks each with the worker in its context.
type counting struct {
	mu    sync.Mutex
	byKey map[any]int
}

type (
	workerKey struct{}
	beforeKey struct{}
)

func (c *counting) Before(ctx context.Context, _ *ant.Statement) (context.Context, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.byKey[ctx.Value(workerKey{})]++

	return context.WithValue(ctx, beforeKey{}, true), nil
}

func (c *counting) After(ctx context.Context, _ *ant.Statement, _ ant.Outcome) {
	if ctx.Value(beforeKey{}) == nil {
		panic("After was not given the context Before returned")
	}
}

// Interceptors see every statement from every goroutine: the logger, metrics,
// diagnostics with a scope per request, and one of the application's own.
func TestInterceptorsOnManyGoroutines(t *testing.T) {
	each(t, func(t *testing.T, target testdb.Target) {
		var (
			out    bytes.Buffer
			alerts atomic.Int64
		)

		m := metrics.New()
		c := &counting{byKey: map[any]int{}}
		db := shared(
			t,
			target,
			ant.WithLogger(slog.New(slog.NewTextHandler(
				&lockedWriter{w: &out},
				&slog.HandlerOptions{Level: slog.LevelDebug},
			))),
			ant.WithInterceptor(m),
			ant.WithInterceptor(c),
			ant.WithDiagnostics(ant.Diagnostics{
				RepeatLimit: 3,
				Alert:       func(context.Context, ant.Alert) { alerts.Add(1) },
			}),
		)
		b := seedBank(t, db, 4)

		const n, rounds = 10, 5

		errs := workers(n, func(w int) error {
			// One request: a scope that its own goroutines share.
			ctx := ant.Watch(context.WithValue(t.Context(), workerKey{}, w))

			inner := workers(3, func(int) error {
				for range rounds {
					if _, err := db.Accounts.Find(ctx, b.accounts[w%4].ID); err != nil {
						return err
					}
				}

				return nil
			})

			for _, err := range inner {
				if err != nil {
					return err
				}
			}

			statements, err := ant.DryRun(ctx, db, func(tx *BankDB) error {
				return tx.Accounts.Insert(ctx, &Account{Owner: fmt.Sprintf("dry-%d", w)})
			})
			if err != nil {
				return err
			}

			if len(statements) == 0 {
				return fmt.Errorf("DryRun recorded no statements")
			}

			return nil
		})
		noErrors(t, errs)

		count, err := db.Accounts.Where(ant.Like(&A.Owner, "dry-%")).Count(t.Context())
		require.NoError(t, err)
		require.Zero(t, count, "a dry run left a row")

		c.mu.Lock()
		defer c.mu.Unlock()

		for w := range n {
			require.GreaterOrEqual(t, c.byKey[w], 3*rounds, "worker %d", w)
		}

		var selects uint64

		for _, s := range m.Snapshot() {
			if s.Kind == "SELECT" {
				selects = s.Count
			}
		}

		require.GreaterOrEqual(t, selects, uint64(n*3*rounds))
		require.EqualValues(t, n, alerts.Load(), "one N+1 alert per request scope")
		require.NotEmpty(t, out.String())
	})
}

// Models first used by many goroutines at once are built once: every goroutine
// gets the same probe, the same table, and the same SQL. These types are used
// nowhere else, so the test is their first use.
type firstUser struct {
	ID     int64
	Name   string
	Orders []firstOrder
}

type firstOrder struct {
	ID          int64
	FirstUserID int64
	Total       int64
	FirstUser   *firstUser
}

type firstDB struct {
	ant.Context
	Users  ant.Table[firstUser]
	Orders ant.Table[firstOrder]
}

func TestModelsFirstUsedAtOnce(t *testing.T) {
	each(t, func(t *testing.T, target testdb.Target) {
		type result struct {
			probe *firstUser
			table string
			text  string
		}

		results := make([]result, 32)

		errs := workers(len(results), func(w int) error {
			var (
				u  = ant.Fields[firstUser]()
				o  = ant.Fields[firstOrder]()
				db *firstDB
			)

			if w%2 == 0 {
				var err error
				if db, err = ant.Open[firstDB](
					t.Context(),
					target.Dialect,
					target.DSN,
				); err != nil {
					return err
				}

				defer db.Close()
			} else {
				db = ant.Session(must(ant.Wrap[firstDB](target.Dialect, target.DB)))
			}

			_, table, err := ant.TableOf[firstOrder]()
			if err != nil {
				return err
			}

			text, _, err := db.Users.Where(ant.Eq(&u.Name, "x")).
				Where(ant.Has(&u.Orders, ant.Gt(&o.Total, int64(1)))).
				Include(&u.Orders).
				ToSQL()
			if err != nil {
				return err
			}

			results[w] = result{u, table, text}

			return nil
		})
		noErrors(t, errs)

		for _, r := range results[1:] {
			require.Same(t, results[0].probe, r.probe)
			require.Equal(t, results[0].table, r.table)
			require.Equal(t, results[0].text, r.text)
		}
	})
}

func must[T any](v T, err error) T {
	if err != nil {
		panic(err)
	}

	return v
}

// marked records which pool each worker's statements went to.
type marked struct {
	mu   sync.Mutex
	seen map[any][]bool // worker to Replica flags, in order
}

func (m *marked) Before(ctx context.Context, s *ant.Statement) (context.Context, error) {
	if w := ctx.Value(workerKey{}); w != nil && strings.HasPrefix(s.SQL, "SELECT") {
		m.mu.Lock()
		m.seen[w] = append(m.seen[w], s.Replica)
		m.mu.Unlock()
	}

	return ctx, nil
}

func (*marked) After(context.Context, *ant.Statement, ant.Outcome) {}

// Reads spread over the replicas from every goroutine, and a session that wrote
// reads from the primary from then on, while the others keep to the replicas.
func TestReplicasOnManyGoroutines(t *testing.T) {
	each(t, func(t *testing.T, target testdb.Target) {
		replicas := make([]*sql.DB, 2)

		for i := range replicas {
			pool, err := sql.Open(target.Dialect.DriverName(), target.DSN)
			require.NoError(t, err)

			t.Cleanup(func() { pool.Close() })

			replicas[i] = pool
		}

		m := &marked{seen: map[any][]bool{}}
		db := shared(
			t,
			target,
			ant.WithReplicas(replicas...),
			ant.WithStickyWrites(time.Hour),
			ant.WithInterceptor(m),
		)
		b := seedBank(t, db, 4)

		const n = 12

		errs := workers(n, func(w int) error {
			ctx := context.WithValue(t.Context(), workerKey{}, w)
			session := ant.Session(db)

			for i := range 6 {
				if w%3 == 0 && i == 3 {
					_, err := session.Accounts.Where(ant.Eq(&A.ID, b.accounts[w%4].ID)).
						Update(ctx, ant.Inc(&A.Balance, int64(0)))
					if err != nil {
						return err
					}
				}

				if _, err := session.Accounts.Find(ctx, b.accounts[i%4].ID); err != nil {
					return err
				}
			}

			return nil
		})
		noErrors(t, errs)

		m.mu.Lock()
		defer m.mu.Unlock()

		for w := range n {
			want := []bool{true, true, true, true, true, true}

			if w%3 == 0 {
				want = []bool{true, true, true, false, false, false}
			}

			require.Equal(t, want, m.seen[w], "worker %d: true is a replica", w)
		}

		for i, pool := range replicas {
			require.Positive(t, pool.Stats().OpenConnections, "replica %d was never read", i)
		}
	})
}
