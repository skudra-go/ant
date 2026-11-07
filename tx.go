package ant

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math/rand/v2"
	"slices"
	"time"

	"github.com/skudra-go/ant/dialect"
	"github.com/skudra-go/ant/internal/exec"
	"github.com/skudra-go/ant/internal/track"
)

// txState is an open transaction, shared by nested Tx calls.
type txState struct {
	tx      *sql.Tx
	done    bool
	depth   int                     // nested savepoints so far, for naming
	undo    []undoStep              // tracker changes to settle when it ends
	pending []func(context.Context) // observers to run after commit
}

// undoStep is one tracker change the transaction settles: undone if it rolls
// back, kept if it commits. A session made inside a transaction has a tracker of
// its own, so each step names the tracker it changes.
type undoStep struct {
	tracker *track.Tracker
	change  track.Change
}

// record hands tracker changes to the transaction, which settles them when it
// ends. A change made outside a transaction stands at once, and is kept.
func (s *state) record(changes ...track.Change) {
	for _, c := range changes {
		if s.tx == nil {
			c.Keep()

			continue
		}

		s.tx.undo = append(s.tx.undo, undoStep{s.tracker, c})
	}
}

// committed keeps the tracker changes the transaction made and runs what was
// waiting for the commit.
func (t *txState) committed(ctx context.Context) {
	steps := t.undo
	t.undo = nil

	settle(ctx, steps, track.Change.Keep)

	for _, f := range t.pending {
		f(ctx)
	}

	t.pending = nil
}

// settle runs each step's change, by tracker, in the order given. It writes to
// tracked entries, so each tracker's run while it is claimed: at once if ctx's
// operation holds it, and otherwise through Tracker.Settle, which never waits.
func settle(ctx context.Context, steps []undoStep, run func(track.Change)) {
	var trackers []*track.Tracker

	for _, step := range steps {
		if !slices.Contains(trackers, step.tracker) {
			trackers = append(trackers, step.tracker)
		}
	}

	for _, tracker := range trackers {
		var work []track.Change

		for _, step := range steps {
			if step.tracker == tracker {
				work = append(work, step.change)
			}
		}

		do := func() {
			for _, c := range work {
				run(c)
			}
		}

		if holding(ctx, tracker) {
			do()
		} else {
			tracker.Settle(do)
		}
	}
}

// txOptions holds what TxOption configures.
type txOptions struct {
	sql      sql.TxOptions
	attempts int
}

// TxOption configures a transaction.
type TxOption func(*txOptions)

func resolveTx(options []TxOption) txOptions {
	o := txOptions{attempts: 1}

	for _, option := range options {
		option(&o)
	}

	return o
}

// Isolation sets the transaction isolation level.
func Isolation(level sql.IsolationLevel) TxOption {
	return func(o *txOptions) { o.sql.Isolation = level }
}

// ReadOnly makes the transaction read-only.
func ReadOnly() TxOption { return func(o *txOptions) { o.sql.ReadOnly = true } }

// child returns a context state bound to a transaction.
func (s *state) child(t *txState) *state {
	c := *s
	c.exec = exec.Wrap(t.tx, s.settings.interceptors, false, s.settings.detailed)
	c.tx = t

	return &c
}

// beginner starts transactions, on the pool or on one connection.
type beginner interface {
	BeginTx(ctx context.Context, options *sql.TxOptions) (*sql.Tx, error)
}

func (s *state) starter() beginner {
	if s.conn != nil {
		return s.conn
	}

	return s.db
}

func (s *state) begin(ctx context.Context, o txOptions) (*txState, error) {
	tx, err := s.starter().BeginTx(ctx, &o.sql)
	if err != nil {
		return nil, err
	}

	return &txState{tx: tx}, nil
}

// transact runs write. With several set, it uses a transaction of its own, or
// a savepoint in an open one, so that it either finishes or leaves no trace.
func (s *state) transact(ctx context.Context, several bool, write func(exec.Executor) error) error {
	if !several || !s.settings.autoTx {
		return write(s.exec)
	}

	if s.tx != nil {
		return s.savepoint(ctx, func() error { return write(s.exec) })
	}

	tx, err := s.starter().BeginTx(ctx, nil)
	if err != nil {
		return err
	}

	return finish(tx, write(exec.Wrap(tx, s.settings.interceptors, false, s.settings.detailed)))
}

// finish commits on success and rolls back on failure.
func finish(tx *sql.Tx, err error) error {
	if err != nil {
		return errors.Join(err, rollback(tx))
	}

	return tx.Commit()
}

// rollback ends tx. A cancelled context ends a transaction by itself, so a
// transaction that is already over is not an error here.
func rollback(tx *sql.Tx) error {
	if err := tx.Rollback(); err != nil && !errors.Is(err, sql.ErrTxDone) {
		return err
	}

	return nil
}

func (s *state) savepoint(ctx context.Context, body func() error) error {
	root := s.tx
	root.depth++
	name := fmt.Sprintf("ant_sp_%d", root.depth)

	save, back, release := dialect.SyntaxOf(s.dialect).SavepointCommands()
	if _, err := s.exec.ExecContext(ctx, save+name); err != nil {
		return err
	}

	mark, waiting := len(root.undo), len(root.pending)

	if err := body(); err != nil {
		root.rollbackTo(ctx, mark)
		root.pending = root.pending[:waiting]
		// The caller's context may be cancelled, and the transaction must still
		// return to the savepoint.
		_, undo := s.exec.ExecContext(context.WithoutCancel(ctx), back+name)

		return errors.Join(err, undo)
	}

	if release == "" {
		return nil
	}

	_, err := s.exec.ExecContext(ctx, release+name)

	return err
}

// rollbackTo undoes the tracker changes made since mark, newest first.
func (t *txState) rollbackTo(ctx context.Context, mark int) {
	steps := slices.Clone(t.undo[mark:])
	t.undo = t.undo[:mark]

	slices.Reverse(steps)
	settle(ctx, steps, track.Change.Undo)
}

// abort forgets everything the transaction did.
func (t *txState) abort(ctx context.Context) {
	t.rollbackTo(ctx, 0)
	t.pending = nil
	t.done = true
}

// Attempts retries a transaction that fails from a deadlock or serialization
// conflict, up to n runs in all, waiting a little longer each time. f must be safe
// to run again, so it should not change anything outside the database.
func Attempts(n int) TxOption { return func(o *txOptions) { o.attempts = n } }

// Tx runs f in a transaction, passing a context of the same type bound to it.
// The transaction commits if f returns nil, and rolls back if it returns an
// error or panics. Inside a transaction, Tx uses a savepoint instead, so only
// the inner part rolls back. The context shares this one's tracker, and a
// rollback restores the tracker's view of what was saved.
func (c *Context) Tx[DB any](ctx context.Context, f func(tx *DB) error, options ...TxOption) error {
	s := c.state
	if s == nil {
		return ErrNotOpen
	}

	if _, ok := s.self.(*DB); !ok {
		return fmt.Errorf("%w: %T", ErrTxType, new(DB))
	}

	if s.tx != nil {
		return s.savepoint(ctx, func() error { return f(s.self.(*DB)) })
	}

	o := resolveTx(options)

	if len(options) == 0 && s.settings.retry.Attempts > 1 {
		o.attempts = s.settings.retry.Attempts
	}

	for attempt := 1; ; attempt++ {
		err := runTx(ctx, s, o, f)
		if err == nil || attempt >= o.attempts || !IsRetryable(err) {
			return err
		}

		if waited := backoff(ctx, attempt); waited != nil {
			return errors.Join(err, waited)
		}
	}
}

// backoff waits a little longer each attempt, with jitter, so that two conflicting
// transactions separate.
func backoff(ctx context.Context, attempt int) error {
	wait := time.Duration(attempt) * retryDelay

	wait += rand.N(wait)
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(wait):
		return nil
	}
}

// retryDelay is the base wait between transaction attempts.
const retryDelay = 5 * time.Millisecond

func runTx[DB any](ctx context.Context, s *state, o txOptions, f func(tx *DB) error) error {
	t, err := s.begin(ctx, o)
	if err != nil {
		return err
	}

	db, err := bindState[DB](s.child(t))
	if err != nil {
		return errors.Join(err, rollback(t.tx))
	}

	defer func() {
		if recovered := recover(); recovered != nil {
			t.abort(ctx)
			_ = rollback(t.tx)

			panic(recovered)
		}
	}()

	if err := f(db); err != nil {
		t.abort(ctx)

		return errors.Join(err, rollback(t.tx))
	}

	t.done = true

	if err := t.tx.Commit(); err != nil {
		t.abort(ctx)

		return err
	}

	t.committed(ctx)

	return nil
}

// Begin starts a transaction and returns a context bound to it. End it with
// Commit or Rollback. Prefer Context.Tx, which cannot leave one open.
func Begin[DB any](ctx context.Context, db *DB, options ...TxOption) (*DB, error) {
	h, ok := any(db).(Handle)
	if !ok {
		return nil, ErrNoContext
	}

	s := h.context().state

	switch {
	case s == nil:
		return nil, ErrNotOpen
	case s.tx != nil:
		return db, nil
	}

	t, err := s.begin(ctx, resolveTx(options))
	if err != nil {
		return nil, err
	}

	tx, err := bindState[DB](s.child(t))
	if err != nil {
		return nil, errors.Join(err, rollback(t.tx))
	}

	return tx, nil
}

// UseTx returns a context bound to a transaction started outside ant, so that
// ant's queries can join it. The caller commits or rolls back tx. Observers wait
// for a commit that ant cannot see, so they do not run.
func UseTx[DB any](db *DB, tx *sql.Tx) (*DB, error) {
	h, ok := any(db).(Handle)
	if !ok {
		return nil, ErrNoContext
	}

	s := h.context().state
	if s == nil {
		return nil, ErrNotOpen
	}

	return bindState[DB](s.child(&txState{tx: tx}))
}

// UseConn returns a context on one connection from database/sql, such as one
// holding a session setting or an advisory lock. Everything it does, including
// transactions, stays on conn. It shares db's tracker. The caller closes conn.
func UseConn[DB any](db *DB, conn *sql.Conn) (*DB, error) {
	h, ok := any(db).(Handle)
	if !ok {
		return nil, ErrNoContext
	}

	parent := h.context().state
	if parent == nil {
		return nil, ErrNotOpen
	}

	s := *parent
	s.conn = conn
	s.exec = exec.Wrap(conn, s.settings.interceptors, false, s.settings.detailed)
	s.replicas = nil

	return bindState[DB](&s)
}

// CurrentTx returns the transaction a context is bound to, or nil. Pass it to
// UseTx to share one transaction between contexts of different types.
func CurrentTx(h Handle) *sql.Tx {
	if h == nil || h.context().state == nil || h.context().state.tx == nil {
		return nil
	}

	return h.context().state.tx.tx
}

func (c *Context) current() (*txState, error) {
	switch {
	case c.state == nil:
		return nil, ErrNotOpen
	case c.state.tx == nil:
		return nil, ErrNotInTx
	case c.state.tx.done:
		return nil, ErrTxDone
	}

	return c.state.tx, nil
}

// Commit ends a transaction from Begin, keeping its changes.
func (c *Context) Commit() error {
	t, err := c.current()
	if err != nil {
		return err
	}

	t.done = true

	if err := t.tx.Commit(); err != nil {
		t.abort(context.Background())

		return err
	}

	t.committed(context.Background())

	return nil
}

// Rollback ends a transaction from Begin, discarding its changes.
func (c *Context) Rollback() error {
	t, err := c.current()
	if err != nil {
		return err
	}

	t.abort(context.Background())

	return rollback(t.tx)
}

// Savepoint marks a point to return to with RollbackTo.
func (c *Context) Savepoint(ctx context.Context, name string) error {
	save, _, _ := dialect.SyntaxOf(c.state.dialect).SavepointCommands()

	return c.savepointCommand(ctx, save, name)
}

// RollbackTo undoes the statements since a savepoint. The tracker is not
// rewound, so reload entities whose changes it undid.
func (c *Context) RollbackTo(ctx context.Context, name string) error {
	_, back, _ := dialect.SyntaxOf(c.state.dialect).SavepointCommands()

	return c.savepointCommand(ctx, back, name)
}

// Release forgets a savepoint, keeping its changes.
func (c *Context) Release(ctx context.Context, name string) error {
	_, _, release := dialect.SyntaxOf(c.state.dialect).SavepointCommands()
	if release == "" {
		return nil
	}

	return c.savepointCommand(ctx, release, name)
}

func (c *Context) savepointCommand(ctx context.Context, command, name string) error {
	if _, err := c.current(); err != nil {
		return err
	}

	_, err := c.state.exec.ExecContext(ctx, command+c.state.dialect.Quote(name))

	return err
}
