package ant

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"
)

// Alert is a warning about how the database is being used.
type Alert struct {
	Kind    string // "budget", "repeated", or "cartesian"
	Message string
	SQL     string
}

// Diagnostics configures warnings about query patterns. They need a scope to count
// in, usually one web request: pass a context from Watch to the calls in it.
type Diagnostics struct {
	// Budget alerts when statements in a scope spend longer than this in total,
	// which a slow-query log cannot see if each one is fast.
	Budget time.Duration
	// RepeatLimit alerts when one statement runs more than this many times in a
	// scope, which is what an N+1 pattern looks like.
	RepeatLimit int
	// Alert receives warnings. The default logs them with slog.
	Alert func(ctx context.Context, a Alert)
}

// WithDiagnostics turns on warnings. They cost a little per statement, and only
// count in contexts from Watch. Joining two one-to-many relations in one query
// multiplies its rows, and that warns too, with or without a scope.
func WithDiagnostics(d Diagnostics) Option {
	return func(s *settings) {
		state := &diagnostics{config: d}
		s.diagnostics = state
		s.interceptors = append(s.interceptors, state)
	}
}

type watchKey struct{}

// watch counts one scope's statements.
type watch struct {
	mu      sync.Mutex
	total   time.Duration
	counts  map[string]int
	alerted map[string]bool
}

// Watch returns a context that counts statements for Diagnostics. Use one per
// request, in middleware.
func Watch(ctx context.Context) context.Context {
	return context.WithValue(
		ctx,
		watchKey{},
		&watch{counts: map[string]int{}, alerted: map[string]bool{}},
	)
}

// diagnostics is the Interceptor from WithDiagnostics.
type diagnostics struct {
	config Diagnostics
}

func (d *diagnostics) alert(ctx context.Context, a Alert) {
	if d.config.Alert != nil {
		d.config.Alert(ctx, a)

		return
	}

	slog.WarnContext(ctx, "ant: "+a.Message, "kind", a.Kind, "sql", a.SQL)
}

// Before implements Interceptor.
func (d *diagnostics) Before(ctx context.Context, _ *Statement) (context.Context, error) {
	return ctx, nil
}

// After implements Interceptor.
func (d *diagnostics) After(ctx context.Context, s *Statement, o Outcome) {
	w, ok := ctx.Value(watchKey{}).(*watch)
	if !ok {
		return
	}

	w.mu.Lock()
	w.total += o.Duration
	w.counts[s.SQL]++
	over := d.config.Budget > 0 && w.total > d.config.Budget && !w.alerted["budget"]

	repeated := d.config.RepeatLimit > 0 && w.counts[s.SQL] > d.config.RepeatLimit &&
		!w.alerted[s.SQL]

	if over {
		w.alerted["budget"] = true
	}

	if repeated {
		w.alerted[s.SQL] = true
	}

	total := w.total
	w.mu.Unlock()

	if over {
		d.alert(
			ctx,
			Alert{
				Kind: "budget",
				Message: fmt.Sprintf(
					"statements spent %s, over the budget of %s",
					total,
					d.config.Budget,
				),
				SQL: s.SQL,
			},
		)
	}

	if repeated {
		d.alert(
			ctx,
			Alert{
				Kind: "repeated",
				Message: fmt.Sprintf(
					"statement ran over %d times, which looks like N+1: consider Include",
					d.config.RepeatLimit,
				),
				SQL: s.SQL,
			},
		)
	}
}

// warnCartesian reports a query joining several to-many relations.
func (s *state) warnCartesian(ctx context.Context, joins int) {
	if s.settings.diagnostics != nil {
		s.settings.diagnostics.alert(
			ctx,
			Alert{
				Kind: "cartesian",
				Message: fmt.Sprintf(
					"query joins %d one-to-many relations, which multiplies its rows: consider Include",
					joins,
				),
			},
		)
	}
}

// DryRun runs fn in a transaction and rolls it back, returning every statement it
// ran. Nothing stays, though sequences move on, and whatever fn does outside the
// database still happens. It shows what a piece of code really does to a database,
// including what the unit of work decides to write.
func DryRun[DB any](ctx context.Context, db *DB, fn func(tx *DB) error) ([]Statement, error) {
	h, ok := any(db).(Handle)
	if !ok {
		return nil, ErrNoContext
	}

	parent := h.context().state
	if parent == nil {
		return nil, ErrNotOpen
	}

	recorded := &recording{}
	s := *parent
	s.settings.interceptors = append(
		append([]Interceptor(nil), s.settings.interceptors...),
		recorded,
	)

	t, err := s.begin(ctx, txOptions{attempts: 1})
	if err != nil {
		return nil, err
	}
	defer rollback(t.tx)

	tx, err := bindState[DB](s.child(t))
	if err != nil {
		return nil, err
	}

	if err := fn(tx); err != nil {
		t.abort(ctx)

		return recorded.statements, err
	}

	t.abort(ctx)

	return recorded.statements, nil
}

// recording is the Interceptor behind DryRun.
type recording struct {
	mu         sync.Mutex
	statements []Statement
}

// Before implements Interceptor.
func (r *recording) Before(ctx context.Context, s *Statement) (context.Context, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.statements = append(r.statements, Statement{SQL: s.SQL, Args: s.Args})

	return ctx, nil
}

// After implements Interceptor.
func (r *recording) After(context.Context, *Statement, Outcome) {}
