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
	// external marks a transaction from UseTx, which its caller ends, so ant
	// never learns whether it committed. open counts the savepoints ant has
	// open in it, whose end ant does see.
	external bool
	open     int
	// ctx is the context the transaction began with, and stop stops the
	// rollback that waits for it to end. taken is the connection ant took for
	// the transaction, given back when it ends.
	ctx   context.Context
	stop  func() bool
	taken *sql.Conn
}

// sees reports whether ant will learn how the changes made in t now end: in a
// transaction of its own always, and in one from UseTx only inside a savepoint
// ant opened, which it releases or rolls back itself.
func (t *txState) sees() bool { return !t.external || t.open > 0 }

// undoStep is one tracker change the transaction settles: undone if it rolls
// back, kept if it commits. A session made inside a transaction has a tracker of
// its own, so each step names the tracker it changes.
type undoStep struct {
	tracker *track.Tracker
	change  track.Change
}

// record hands tracker changes to the transaction, which settles them when it
// ends. A change made outside a transaction stands at once, and is kept, as is
// one made where ant cannot see the transaction end: holding it would keep its
// history for good.
func (s *state) record(changes ...track.Change) {
	for _, c := range changes {
		if s.tx == nil || !s.tx.sees() {
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

// begin starts a transaction that ant ends itself. go-mssqldb, as of v1.11.2,
// rolls back with the context the transaction began with, and if that context
// has ended, it leaves a goroutine reading the connection after the connection
// went back to the pool, where the next statement on it races that goroutine.
// So the transaction begins on a context that does not end, on a connection
// taken with ctx, which still bounds the wait for one. Its statements run with
// their own contexts, and if ctx ends first, the transaction is rolled back
// from a goroutine of its own, as database/sql would do, but with a context the
// driver can finish on.
func (s *state) begin(ctx context.Context, o txOptions) (*txState, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	t := &txState{ctx: ctx}
	on := s.starter()

	if s.conn == nil {
		conn, err := s.db.Conn(ctx)
		if err != nil {
			return nil, err
		}

		t.taken, on = conn, conn
	}

	tx, err := on.BeginTx(context.WithoutCancel(ctx), &o.sql)
	if err != nil {
		t.giveBack()

		return nil, err
	}

	t.tx = tx

	t.stop = context.AfterFunc(ctx, func() {
		_ = tx.Rollback()

		t.giveBack()
	})

	return t, nil
}

// commit commits the transaction and gives back its connection. If the context
// it began with has ended, it rolls back instead, as database/sql does. A
// transaction from UseTx began elsewhere, and has neither.
func (t *txState) commit() error {
	defer t.giveBack()

	if t.ended() {
		return errors.Join(t.ctx.Err(), t.rollback())
	}

	return t.tx.Commit()
}

// ended stops the rollback that waits for the context the transaction began
// with, and reports whether that context has ended.
func (t *txState) ended() bool {
	if t.stop == nil {
		return false
	}

	t.stop()

	return t.ctx.Err() != nil
}

// rollback rolls the transaction back and gives back its connection. A
// transaction that is already over, as when its context ended, is not an error.
func (t *txState) rollback() error {
	defer t.giveBack()

	t.ended()

	if err := t.tx.Rollback(); err != nil && !errors.Is(err, sql.ErrTxDone) {
		return err
	}

	return nil
}

// giveBack returns the connection ant took for the transaction to the pool,
// once the transaction has let go of it. Doing so twice does nothing.
func (t *txState) giveBack() {
	if t.taken != nil {
		_ = t.taken.Close()
	}
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

	t, err := s.begin(ctx, txOptions{})
	if err != nil {
		return err
	}

	if err := write(
		exec.Wrap(t.tx, s.settings.interceptors, false, s.settings.detailed),
	); err != nil {
		return errors.Join(err, t.rollback())
	}

	return t.commit()
}

func (s *state) savepoint(ctx context.Context, body func() error) error {
	sp, err := s.openSavepoint(ctx)
	if err != nil {
		return err
	}

	if err := body(); err != nil {
		return errors.Join(err, s.rollbackSavepoint(ctx, sp))
	}

	return s.releaseSavepoint(ctx, sp)
}

// savepointState is a savepoint ant opened in a transaction: by a nested Tx or
// a write, which ends it when its body returns, or by Begin, whose context ends
// it with Commit or Rollback. mark and waiting are where the transaction's
// tracker changes and observers stood when it opened.
type savepointState struct {
	name    string
	mark    int
	waiting int
	done    bool
}

// openSavepoint opens a savepoint in s's transaction.
func (s *state) openSavepoint(ctx context.Context) (*savepointState, error) {
	root := s.tx
	root.depth++
	name := fmt.Sprintf("ant_sp_%d", root.depth)

	save, _, _ := dialect.SyntaxOf(s.dialect).SavepointCommands()
	if _, err := s.exec.ExecContext(ctx, save+name); err != nil {
		return nil, err
	}

	root.open++

	return &savepointState{
		name:    name,
		mark:    len(root.undo),
		waiting: len(root.pending),
	}, nil
}

// rollbackSavepoint returns the transaction to sp, and takes back the tracker
// changes and observers since it opened.
func (s *state) rollbackSavepoint(ctx context.Context, sp *savepointState) error {
	root := s.tx
	root.open--

	// A savepoint opened before this one and ended first has taken these back
	// already.
	root.rollbackTo(ctx, min(sp.mark, len(root.undo)))
	root.pending = root.pending[:min(sp.waiting, len(root.pending))]

	// The caller's context may be cancelled, and the transaction must still
	// return to the savepoint.
	_, back, _ := dialect.SyntaxOf(s.dialect).SavepointCommands()
	_, err := s.exec.ExecContext(context.WithoutCancel(ctx), back+sp.name)
	sp.done = true

	return err
}

// releaseSavepoint ends sp, keeping its changes in the transaction.
func (s *state) releaseSavepoint(ctx context.Context, sp *savepointState) error {
	root := s.tx
	root.open--

	if !root.sees() {
		// What the savepoint kept is now the caller's transaction's, whose end
		// ant does not see: its changes stand, and its observers never run.
		mark := min(sp.mark, len(root.undo))
		steps := slices.Clone(root.undo[mark:])
		root.undo = root.undo[:mark]
		root.pending = root.pending[:min(sp.waiting, len(root.pending))]

		settle(ctx, steps, track.Change.Keep)
	}

	_, _, release := dialect.SyntaxOf(s.dialect).SavepointCommands()
	if release == "" {
		sp.done = true

		return nil
	}

	_, err := s.exec.ExecContext(ctx, release+sp.name)
	sp.done = true

	return err
}

// savepointExec runs the statements of a context from Begin inside a
// transaction, and refuses them once its savepoint has ended, as a *sql.Tx does
// once it has, rather than running them in the outer transaction.
type savepointExec struct {
	exec.Executor
	sp *savepointState
}

func (e savepointExec) ExecContext(
	ctx context.Context,
	query string,
	args ...any,
) (sql.Result, error) {
	if e.sp.done {
		return nil, ErrTxDone
	}

	return e.Executor.ExecContext(ctx, query, args...)
}

func (e savepointExec) QueryContext(
	ctx context.Context,
	query string,
	args ...any,
) (*sql.Rows, error) {
	if e.sp.done {
		return nil, ErrTxDone
	}

	return e.Executor.QueryContext(ctx, query, args...)
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
// to run again, so it should not change anything outside the database. Before a
// retry, the tracker forgets what the failed attempt loaded or added, so the next
// attempt reads its rows afresh; what was tracked before the transaction stays,
// with any change made to it, so f should load what it changes.
func Attempts(n int) TxOption { return func(o *txOptions) { o.attempts = n } }

// Tx runs f in a transaction, passing a context of the same type bound to it.
// The transaction commits if f returns nil, and rolls back if it returns an
// error or panics. Inside a transaction, Tx uses a savepoint instead, so only
// the inner part rolls back. The context shares this one's tracker, and a
// rollback restores the tracker's view of what was saved, and takes back the
// keys, timestamps, and versions the saves wrote into tracked entities, so the
// next SaveChanges writes them again.
//
// A transaction is one connection, which runs one statement at a time, so use
// tx from one goroutine at a time, as with a *sql.Tx: goroutines that share it
// take turns. Give each goroutine a transaction of its own to work at once.
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
		mark := s.tracker.Mark()

		err := runTx(ctx, s, o, f)
		if err == nil || attempt >= o.attempts || !IsRetryable(err) {
			return err
		}

		// f runs again, and loads and adds what it needs again, so what this
		// attempt brought into the tracker goes: kept, the next attempt would
		// read the instances it changed, and insert what it added twice.
		s.tracker.ForgetSince(mark)

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
		return errors.Join(err, t.rollback())
	}

	defer func() {
		if recovered := recover(); recovered != nil {
			t.abort(ctx)
			_ = t.rollback()

			panic(recovered)
		}
	}()

	if err := f(db); err != nil {
		t.abort(ctx)

		return errors.Join(err, t.rollback())
	}

	t.done = true

	if err := t.commit(); err != nil {
		t.abort(ctx)

		return err
	}

	t.committed(ctx)

	return nil
}

// Begin starts a transaction and returns a context bound to it. End it with
// Commit or Rollback. Prefer Context.Tx, which cannot leave one open. Inside a
// transaction, Begin opens a savepoint instead, as a nested Tx does: Commit
// releases it, keeping its work in the outer transaction, and Rollback undoes
// only the work since Begin. The options apply to a new transaction only.
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
		return beginSavepoint[DB](ctx, h.context())
	}

	t, err := s.begin(ctx, resolveTx(options))
	if err != nil {
		return nil, err
	}

	tx, err := bindState[DB](s.child(t))
	if err != nil {
		return nil, errors.Join(err, t.rollback())
	}

	return tx, nil
}

// beginSavepoint opens a savepoint in c's transaction and returns a context
// bound to it.
func beginSavepoint[DB any](ctx context.Context, c *Context) (*DB, error) {
	if _, err := c.current(); err != nil {
		return nil, err
	}

	sp, err := c.state.openSavepoint(ctx)
	if err != nil {
		return nil, err
	}

	child := *c.state
	child.begun = sp
	child.exec = savepointExec{c.state.exec, sp}

	tx, err := bindState[DB](&child)
	if err != nil {
		return nil, errors.Join(err, child.rollbackSavepoint(ctx, sp))
	}

	return tx, nil
}

// UseTx returns a context bound to a transaction started outside ant, so that
// ant's queries can join it. The caller commits or rolls back tx, which ant does
// not see: the tracker takes what ant saves in it as saved, so after rolling tx
// back, reload or clear what it saved. A savepoint ant makes in it, through Tx,
// is ant's own, and its rollback is taken back in the tracker as usual.
// Observers wait for a commit that ant cannot see, so they do not run.
func UseTx[DB any](db *DB, tx *sql.Tx) (*DB, error) {
	h, ok := any(db).(Handle)
	if !ok {
		return nil, ErrNoContext
	}

	s := h.context().state
	if s == nil {
		return nil, ErrNotOpen
	}

	return bindState[DB](s.child(&txState{tx: tx, external: true}))
}

// UseConn returns a context on one connection from database/sql, such as one
// holding a session setting or an advisory lock. Everything it does, including
// transactions, stays on conn, so use it from one goroutine at a time, as a
// transaction. It shares db's tracker. The caller closes conn.
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
	case c.state.tx.done, c.state.begun != nil && c.state.begun.done:
		return nil, ErrTxDone
	}

	return c.state.tx, nil
}

// Commit ends a transaction from Begin, keeping its changes. On a context from
// Begin inside a transaction, it releases Begin's savepoint, and the changes
// stand or fall with the outer transaction.
func (c *Context) Commit() error {
	t, err := c.current()
	if err != nil {
		return err
	}

	if sp := c.state.begun; sp != nil {
		return c.state.releaseSavepoint(context.Background(), sp)
	}

	t.done = true

	if err := t.commit(); err != nil {
		t.abort(context.Background())

		return err
	}

	t.committed(context.Background())

	return nil
}

// Rollback ends a transaction from Begin, discarding its changes. On a context
// from Begin inside a transaction, it rolls back to Begin's savepoint, and the
// outer transaction goes on.
func (c *Context) Rollback() error {
	t, err := c.current()
	if err != nil {
		return err
	}

	if sp := c.state.begun; sp != nil {
		return c.state.rollbackSavepoint(context.Background(), sp)
	}

	t.abort(context.Background())

	return t.rollback()
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
