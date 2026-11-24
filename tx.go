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
	tx   *sql.Tx
	done bool
	// lost is set when the database ended the transaction on its own, as a
	// deadlock does on MySQL and SQL Server, which ant learns when returning to
	// a savepoint fails. Statements on it are refused, since database/sql would
	// run them outside any transaction, and it can only be rolled back.
	lost    error
	depth   int                     // nested savepoints so far, for naming
	undo    []undoStep              // tracker changes to settle when it ends
	pending []func(context.Context) // observers to run after commit
	// external marks a transaction from UseTx, which its caller ends, so ant
	// never learns whether it committed. open counts the savepoints ant has
	// open in it, whose end ant does see.
	external bool
	open     int
	// writeLocked says the transaction holds the database's write lock from its
	// start, so a row lock in it adds nothing (dialect.BeginLocker). It is unknown,
	// and false, for one from UseTx.
	writeLocked bool
	// ctx is the context the transaction began with, and stop stops the
	// rollback that waits for it to end. taken is the connection ant took for
	// the transaction, given back when it ends.
	ctx  context.Context
	stop func() bool
	// stopped closes once the rollback for an ended context has given the
	// connection back. waited means ended has dealt with that rollback.
	stopped chan struct{}
	waited  bool
	taken   *sql.Conn
	// tracked lists the entries the transaction's operations began tracking,
	// which a rollback forgets.
	tracked []trackedRange
}

// trackedRange is the entries one operation in a transaction began tracking:
// those numbered after from and by to. An operation that tracks holds its
// tracker's claim while it runs, so no other operation's entries fall between.
type trackedRange struct {
	tracker  *track.Tracker
	from, to uint64
}

// track notes that an operation began tracking the entries of tracker numbered
// after from and by to. Each operation keeps a range of its own, so a
// savepoint's rollback forgets exactly the operations made since it opened.
func (t *txState) track(tracker *track.Tracker, from, to uint64) {
	if to > from {
		t.tracked = append(t.tracked, trackedRange{tracker, from, to})
	}
}

// forgetTracked forgets the entries the transaction's operations began tracking
// since its range at index from: what they loaded, added, or reached through a
// relation, whose rows the rollback took back, so the next SaveChanges does not
// write them again (#145, #224). What was tracked before stays, with its
// changes, since the tracker cannot tell an edit made inside the transaction
// from one made before it. It writes to tracked entries, so it runs as settle
// does, after the reversal of the rollback.
func (t *txState) forgetTracked(ctx context.Context, from int) {
	ranges := slices.Clone(t.tracked[min(from, len(t.tracked)):])
	t.tracked = t.tracked[:min(from, len(t.tracked))]

	var trackers []*track.Tracker

	for _, r := range ranges {
		if !slices.Contains(trackers, r.tracker) {
			trackers = append(trackers, r.tracker)
		}
	}

	for _, tracker := range trackers {
		do := func() {
			for _, r := range ranges {
				if r.tracker == tracker {
					tracker.ForgetBetween(r.from, r.to)
				}
			}
		}

		if holding(ctx, tracker) {
			do()
		} else {
			tracker.Settle(do)
		}
	}
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

// record hands tracker changes to the transaction the write ran in, s's own or
// the one ctx is bound to, which settles them when it ends. A change made
// outside a transaction stands at once, and is kept, as is one made where ant
// cannot see the transaction end: holding it would keep its history for good.
func (s *state) record(ctx context.Context, changes ...track.Change) {
	t := s.txOf(ctx)

	for _, c := range changes {
		if t == nil || !t.sees() {
			c.Keep()

			continue
		}

		t.undo = append(t.undo, undoStep{s.tracker, c})
	}
}

// boundKey is the context.Context key for the transaction a ctx is bound to on
// one pool, by InTx or by ant for a hook. Each pool has its own, so a binding on
// one leaves another's in place.
type boundKey struct {
	pool *sql.DB
}

// binding is a transaction that a ctx carries to every context on its pool.
type binding struct {
	db      *sql.DB
	tx      *txState
	streams *streams
	exec    exec.Executor // runs on the transaction, with no interceptors
}

// InTx returns ctx bound to the transaction h is in. Any context on the same
// connection pool that is in no transaction of its own, such as the root from
// Open, a repository or service built with it, or a session, runs what it is
// given the result for in that transaction: its queries and writes, and its
// SaveChanges and Tx in a savepoint there. A transaction's end settles what
// they did to their trackers, as it does for the transaction's own context.
// Inside Tx, which passes on the caller's ctx, bind it for code that holds
// another context:
//
//	db.Tx(ctx, func(tx *AppDB) error {
//		ctx := ant.InTx(ctx, tx)
//
//		return accounts.Close(ctx, id) // holds db, runs in tx
//	})
//
// The ctx a hook gets is bound to the transaction of its write already. A
// context in a transaction of its own, or on a connection from UseConn, keeps
// to it. Given h in no transaction, InTx returns ctx bound to none, so what it
// runs goes to the pool, as for writing what must stay if the transaction rolls
// back. A ctx is bound to one transaction, which runs one statement at a time,
// so goroutines that share the ctx take turns, as with tx. Once the transaction
// ends, statements given the ctx fail with ErrTxDone or sql.ErrTxDone rather
// than run outside it (#144, #146).
func InTx(ctx context.Context, h Handle) context.Context {
	s := h.context().state

	switch {
	case s == nil:
		return ctx
	case s.tx == nil:
		return context.WithValue(ctx, boundKey{s.db}, (*binding)(nil))
	}

	return s.bound(ctx)
}

// bound returns ctx bound to the transaction s is in, or ctx outside one.
func (s *state) bound(ctx context.Context) context.Context {
	if s.tx == nil {
		return ctx
	}

	return s.bind(ctx, s.tx, s.streams, s.exec)
}

// bind returns ctx bound to t, which runs statements through e.
func (s *state) bind(
	ctx context.Context,
	t *txState,
	on *streams,
	e exec.Executor,
) context.Context {
	return context.WithValue(ctx, boundKey{s.db}, &binding{s.db, t, on, bare(e)})
}

// bare returns e without the interceptors and error detail it was wrapped in,
// for a context that wraps it in its own, keeping a savepoint's guard.
func bare(e exec.Executor) exec.Executor {
	switch e := e.(type) {
	case savepointExec:
		return savepointExec{bare(e.Executor), e.sp}
	case *exec.Plain:
		return e.Executor
	case *exec.Observed:
		return e.Executor
	}

	return e
}

// boundOn returns the transaction ctx is bound to, if it is one on pool.
func boundOn(ctx context.Context, pool *sql.DB) *binding {
	b, _ := ctx.Value(boundKey{pool}).(*binding)
	if b == nil || b.db != pool {
		return nil
	}

	return b
}

// boundIn returns the transaction ctx binds s to: one on s's pool, when s is in
// no transaction of its own and on no connection from UseConn.
func (s *state) boundIn(ctx context.Context) *binding {
	if s == nil || s.tx != nil || s.conn != nil {
		return nil
	}

	return boundOn(ctx, s.db)
}

// txOf returns the transaction s runs in for ctx: its own, the one ctx binds it
// to, or nil.
func (s *state) txOf(ctx context.Context) *txState {
	if s.tx != nil {
		return s.tx
	}

	if b := s.boundIn(ctx); b != nil {
		return b.tx
	}

	return nil
}

// streamsOf returns the count of Each loops on the connection s runs on for ctx.
func (s *state) streamsOf(ctx context.Context) *streams {
	if b := s.boundIn(ctx); b != nil {
		return b.streams
	}

	return s.streams
}

// joined returns s as a context of the transaction ctx binds it to, so that its
// writes take savepoints there and the transaction's end settles its tracker,
// or s itself. It keeps s's tracker, settings and interceptors.
func (s *state) joined(ctx context.Context) *state {
	b := s.boundIn(ctx)
	if b == nil {
		return s
	}

	c := *s
	c.tx, c.streams, c.begun = b.tx, b.streams, nil
	c.exec = exec.Wrap(b.exec, s.settings.interceptors, false, s.settings.detailed)

	return &c
}

// routed runs a context's statements on its pool, or in the transaction ctx is
// bound to when that is one on the same pool.
type routed struct {
	pool *sql.DB
	exec.Executor
}

func (r routed) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	if b := boundOn(ctx, r.pool); b != nil {
		return b.exec.ExecContext(ctx, query, args...)
	}

	return r.Executor.ExecContext(ctx, query, args...)
}

func (r routed) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	if b := boundOn(ctx, r.pool); b != nil {
		return b.exec.QueryContext(ctx, query, args...)
	}

	return r.Executor.QueryContext(ctx, query, args...)
}

// committed keeps the tracker changes the transaction made and runs what was
// waiting for the commit.
func (t *txState) committed(ctx context.Context) {
	steps := t.undo
	t.undo, t.tracked = nil, nil

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
	sql sql.TxOptions
	// attempts is zero until Attempts sets it, which leaves the context's retry
	// policy in force.
	attempts int
}

// TxOption configures a transaction.
type TxOption func(*txOptions)

func resolveTx(options []TxOption) txOptions {
	var o txOptions

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

	// A transaction ant begins on a context from UseConn is on that connection,
	// so it shares its count of Each loops. Any other has a connection of its
	// own.
	if t.external || c.conn == nil {
		c.streams = newStreams(s.dialect)
	}

	c.exec = exec.Wrap(
		c.streams.guard(txExec{t, dialect.SyntaxOf(s.dialect).RetryEndsTx}),
		s.settings.interceptors,
		false,
		s.settings.detailed,
	)

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

	// On a context from UseConn, the connection may be streaming an Each loop's
	// rows, and could not begin.
	if err := s.streams.check(); err != nil {
		return nil, err
	}

	t := &txState{ctx: ctx, writeLocked: s.settings.beginLocks && !o.sql.ReadOnly}
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
	t.stopped = make(chan struct{})

	t.stop = context.AfterFunc(ctx, func() {
		defer close(t.stopped)

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
// with, and reports whether that context has ended. If that rollback has begun,
// it waits for it, so the connection is back in the pool when Tx returns.
func (t *txState) ended() bool {
	if t.stop == nil {
		return false
	}

	if !t.waited {
		t.waited = true

		if !t.stop() {
			<-t.stopped
		}
	}

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
// a savepoint in an open one, so that it either finishes or leaves no trace. A
// ctx bound to a transaction on s's pool puts it in that one.
func (s *state) transact(ctx context.Context, several bool, write func(exec.Executor) error) error {
	return s.transactHooked(
		ctx,
		several,
		false,
		func(_ context.Context, to exec.Executor) error { return write(to) },
	)
}

// transactHooked is transact for a write whose hooks run, if hooked: write gets
// ctx bound to the transaction it runs in, so that what a hook runs with it,
// through any context on the pool, sees the write and waits on none of its
// locks (#144).
func (s *state) transactHooked(
	ctx context.Context,
	several, hooked bool,
	write func(context.Context, exec.Executor) error,
) error {
	s = s.joined(ctx)

	inner := ctx

	if hooked {
		inner = s.bound(ctx)
	}

	if !several || !s.settings.autoTx {
		return write(inner, s.exec)
	}

	if s.tx != nil {
		return s.savepoint(ctx, func() error { return write(inner, s.exec) })
	}

	t, err := s.begin(ctx, txOptions{})
	if err != nil {
		return err
	}

	// The write runs as a Tx's would: a failure that ends the transaction stops
	// what follows. Hooks get the state a Tx gives, whose Each loops also guard
	// the connection; a write with none runs no loop, so it skips that copy.
	run := exec.Wrap(
		txExec{t, dialect.SyntaxOf(s.dialect).RetryEndsTx},
		s.settings.interceptors,
		false,
		s.settings.detailed,
	)

	if hooked {
		in := s.child(t)
		inner = in.bound(ctx)
		run = in.exec
	}

	// What a hook did through its bound ctx is settled with the write: the
	// tracker changes of its writes, and the entities it began tracking.
	if err := write(inner, run); err != nil {
		t.abort(ctx)

		return errors.Join(err, t.rollback())
	}

	if err := t.commit(); err != nil {
		t.abort(ctx)

		return err
	}

	t.committed(ctx)

	return nil
}

// nestedTx runs f in a savepoint, honouring the options it was given. A
// savepoint cannot carry options of its own: read-only is the one PostgreSQL
// allows to change inside a transaction, until the savepoint ends, and an
// isolation level cannot change there at all. Dropping either silently would
// remove a guard exactly when the caller believed it was adding one, so a
// database that cannot change read-only there returns ErrUnsupported.
func (s *state) nestedTx[DB any](ctx context.Context, f func(tx *DB) error, o txOptions) error {
	if o.sql.Isolation != 0 {
		return ErrNestedIsolation
	}

	if o.sql.ReadOnly && !dialect.Supports(s.dialect, dialect.ReadOnlyInTx) {
		return fmt.Errorf("%w: %s on %s", ErrUnsupported, dialect.ReadOnlyInTx, s.dialect.Name())
	}

	if !o.sql.ReadOnly {
		return s.savepoint(ctx, func() error { return f(s.self.(*DB)) })
	}

	return s.savepoint(ctx, func() error {
		if _, err := s.exec.ExecContext(ctx, "SET TRANSACTION READ ONLY"); err != nil {
			return err
		}

		return f(s.self.(*DB))
	})
}

// savepoint runs body in a savepoint ant opens in the transaction s is in.
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
// it with Commit or Rollback. mark, waiting, and tracked are where the
// transaction's tracker changes, observers, and tracked entries stood when it
// opened.
type savepointState struct {
	name    string
	mark    int
	waiting int
	tracked int
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
		tracked: len(root.tracked),
	}, nil
}

// rollbackSavepoint returns the transaction to sp, and takes back the tracker
// changes and observers since it opened, and forgets what it began tracking.
func (s *state) rollbackSavepoint(ctx context.Context, sp *savepointState) error {
	root := s.tx
	root.open--

	// A savepoint opened before this one and ended first has taken these back
	// already.
	root.rollbackTo(ctx, min(sp.mark, len(root.undo)))
	root.forgetTracked(ctx, sp.tracked)
	root.pending = root.pending[:min(sp.waiting, len(root.pending))]

	// The caller's context may be cancelled, and the transaction must still
	// return to the savepoint.
	_, back, _ := dialect.SyntaxOf(s.dialect).SavepointCommands()
	_, err := s.exec.ExecContext(context.WithoutCancel(ctx), back+sp.name)
	sp.done = true

	// The savepoint is gone only if the transaction is: a deadlock on MySQL or
	// SQL Server rolls the whole of it back.
	if err != nil {
		root.lose(ctx, err)

		return root.lost
	}

	return nil
}

// txExec runs statements in a transaction, and refuses them once the database
// has ended it. With ends set, a deadlock or serialization failure ends it, as
// on MySQL and SQL Server.
type txExec struct {
	t    *txState
	ends bool
}

func (e txExec) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	if e.t.lost != nil {
		return nil, e.t.lost
	}

	result, err := e.t.tx.ExecContext(ctx, query, args...)

	return result, e.check(ctx, err)
}

func (e txExec) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	if e.t.lost != nil {
		return nil, e.t.lost
	}

	rows, err := e.t.tx.QueryContext(ctx, query, args...)

	return rows, e.check(ctx, err)
}

// check records a failure that ended the transaction, and returns err.
func (e txExec) check(ctx context.Context, err error) error {
	if err != nil && e.ends && IsRetryable(err) {
		e.t.lose(ctx, err)
	}

	return err
}

// lose records that the database ended the transaction, and forgets what it
// did: its tracker changes are undone and its observers never run.
func (t *txState) lose(ctx context.Context, cause error) {
	if t.lost != nil {
		return
	}

	t.lost = fmt.Errorf("%w: the database ended it: %w", ErrTxDone, cause)
	t.rollbackTo(ctx, 0)
	t.pending = nil
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
		root.tracked = root.tracked[:min(sp.tracked, len(root.tracked))]

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

// abort forgets everything the transaction did, and what it began tracking.
func (t *txState) abort(ctx context.Context) {
	t.rollbackTo(ctx, 0)
	t.forgetTracked(ctx, 0)
	t.pending = nil
	t.done = true
}

// Attempts retries a transaction that fails from a deadlock or serialization
// conflict, up to n runs in all, waiting a little longer each time. f must be safe
// to run again, so it should not change anything outside the database. The
// rollback of a failed attempt forgets what it loaded or added, as every
// rollback does, so the next attempt reads its rows afresh; what was tracked
// before the transaction stays, with any change made to it, so f should load
// what it changes. For this call
// it replaces the policy of WithRetry, which the other options leave in force,
// and f runs once for an n below 2.
func Attempts(n int) TxOption { return func(o *txOptions) { o.attempts = max(n, 1) } }

// Tx runs f in a transaction, passing a context of the same type bound to it.
// The transaction commits if f returns nil, and rolls back if it returns an
// error or panics. Inside a transaction, Tx uses a savepoint instead, so only
// the inner part rolls back. There ReadOnly holds until the savepoint ends, and
// returns ErrUnsupported on a database that cannot change it once a
// transaction has begun; Isolation returns ErrNestedIsolation.
//
// The context shares this one's tracker. A rollback, or a panic, forgets every
// entity the transaction began tracking, by a load, an Add, or a relation, so
// the next SaveChanges does not write again what the rollback took back. An
// entity tracked before the transaction stays: the rollback restores the
// tracker's view of what was saved, and takes back the keys, timestamps, and
// versions the saves wrote into it, so its changes, including those made inside
// f, are pending again. Load inside f what f changes.
//
// A transaction is one connection, which runs one statement at a time, so use
// tx from one goroutine at a time, as with a *sql.Tx: goroutines that share it
// take turns. Give each goroutine a transaction of its own to work at once.
//
// f is given the caller's ctx, so code that holds another context, such as the
// root, runs outside the transaction unless given ant.InTx(ctx, tx). Given a
// ctx bound to a transaction already, Tx is a savepoint in that one.
func (c *Context) Tx[DB any](ctx context.Context, f func(tx *DB) error, options ...TxOption) error {
	s := c.state
	if s == nil {
		return ErrNotOpen
	}

	if _, ok := s.self.(*DB); !ok {
		return fmt.Errorf("%w: %T", ErrTxType, new(DB))
	}

	// Given a ctx bound to a transaction on its pool, Tx is a savepoint there,
	// with f given a context of that transaction (#146).
	if joined := s.joined(ctx); joined != s {
		if _, err := bindState[DB](joined); err != nil {
			return err
		}

		s = joined
	}

	if s.tx != nil {
		return s.nestedTx(ctx, f, resolveTx(options))
	}

	o := resolveTx(options)

	if o.attempts == 0 {
		o.attempts = max(s.settings.retry.Attempts, 1)
	}

	for attempt := 1; ; attempt++ {
		// The rollback of a failed attempt forgets what it brought into the
		// tracker, so f, run again, loads and adds afresh: kept, the next
		// attempt would read the instances it changed, and insert what it
		// added twice.
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

	if t.lost != nil {
		t.abort(ctx)

		return errors.Join(t.lost, t.rollback())
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

	// Given a ctx bound to a transaction on its pool, Begin opens a savepoint
	// there (#146).
	if joined := s.joined(ctx); joined != s {
		bound, err := bindState[DB](joined)
		if err != nil {
			return nil, err
		}

		return beginSavepoint[DB](ctx, any(bound).(Handle).context())
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
	s.streams = newStreams(s.dialect)
	s.exec = exec.Wrap(s.streams.guard(conn), s.settings.interceptors, false, s.settings.detailed)
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

	if t.lost != nil {
		t.abort(context.Background())

		return errors.Join(t.lost, t.rollback())
	}

	t.done = true

	if err := t.commit(); err != nil {
		t.abort(context.Background())

		return err
	}

	t.committed(context.Background())

	return nil
}

// Rollback ends a transaction from Begin, discarding its changes, and forgets the
// entities it began tracking, as a rollback of Tx does. On a context from Begin
// inside a transaction, it rolls back to Begin's savepoint, and the outer
// transaction goes on.
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
