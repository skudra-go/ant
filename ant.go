// Package ant is a Go ORM with plain struct models, compiler-checked queries,
// and change tracking, and no code generation.
//
// Declare a context, open it, and query through field probes:
//
//	type AppDB struct {
//		ant.Context
//		Users ant.Table[User]
//	}
//
//	var Users = ant.Fields[User]()
//
//	db, err := ant.Open[AppDB](ctx, postgres.Dialect, dsn)
//	adults, err := db.Users.Where(ant.Gt(&Users.Age, 18)).All(ctx)
//
// # For AI coding assistants
//
// ant is newer than most models' training data, and its API is not GORM's. The
// documentation is at https://skudra.net/llms.txt as Markdown, one file per page,
// and https://skudra.net/llms-full.txt has every page in one. This module ships a
// skill for coding agents, matching this version, at skills/ant/SKILL.md in the
// directory that go list -m -f '{{.Dir}}' github.com/skudra-go/ant prints.
package ant

import (
	"context"
	"database/sql"
	"errors"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	"github.com/skudra-go/ant/dialect"
	"github.com/skudra-go/ant/internal/exec"
	"github.com/skudra-go/ant/internal/meta"
	"github.com/skudra-go/ant/internal/track"
)

// DeletedAt is the type of a soft delete field. A model with one is marked
// deleted instead of removed, and queries skip such rows unless asked.
type DeletedAt = meta.DeletedAt

// UnixTime is a time stored as whole seconds since 1970. On CreatedAt and
// UpdatedAt, it is maintained like a time.Time. The zero time is stored as 0,
// as schemas of this kind mark no time, so 0, and the Unix epoch with it, reads
// back as the zero time.
type UnixTime = meta.UnixTime

// Base is an optional embeddable model base. ID is a generated primary key. Any
// model's time.Time fields named CreatedAt and UpdatedAt are maintained for it.
type Base struct {
	ID        int64
	CreatedAt time.Time
	UpdatedAt time.Time
}

// Context is embedded in a user's database struct. Its methods are promoted,
// so they are called directly on that struct.
type Context struct {
	state *state
}

// state is what a context shares with its tables and queries. A transaction
// has its own state, with the same tracker.
type state struct {
	db        *sql.DB
	conn      *sql.Conn // set by UseConn
	exec      exec.Executor
	replicas  []exec.Executor
	shared    *sharedState
	dialect   dialect.Dialect
	settings  settings
	tracker   *track.Tracker
	saving    *sync.Mutex // serializes SaveChanges on a tracker
	observed  *observers
	self      any
	tx        *txState        // nil outside a transaction
	streams   *streams        // the Each loops on a transaction's or UseConn's connection
	begun     *savepointState // the savepoint of a context from Begin inside a transaction
	lastWrite *atomic.Int64   // when this session last wrote, for sticky reads
}

// sharedState is mutable state shared by a context's sessions.
type sharedState struct {
	nextReplica atomic.Uint64
	// selects and inserts keep the SQL of statements by shape (shape.go). The
	// SQL depends on the dialect, which a context's sessions share.
	selects plans[selectShape]
	inserts plans[insertShape]
}

// claimed is the context.Context key for an operation's claim on one tracker.
// Each tracker has its own key, so a hook that moves between two contexts keeps
// both claims.
type claimed struct{ tracker *track.Tracker }

// holds reports whether ctx belongs to the operation that holds s's tracker now.
func (s *state) holds(ctx context.Context) bool { return holding(ctx, s.tracker) }

// holding reports whether ctx belongs to the operation that holds t now.
func holding(ctx context.Context, t *track.Tracker) bool {
	c, _ := ctx.Value(claimed{t}).(*track.Claim)

	return t.Owns(c)
}

// claim reserves s's tracker for one operation, if on, and returns the
// context.Context to run it with. Another operation on the same tracker gets
// ErrConcurrentUse rather than sharing entities with this one. A call made
// through the returned context, such as a hook's query or an include's, belongs
// to this operation and shares the claim until the operation ends.
func (s *state) claim(ctx context.Context, on bool) (context.Context, func(), error) {
	if !on || s.holds(ctx) {
		return ctx, func() {}, nil
	}

	c := s.tracker.TryClaim()
	if c == nil {
		return ctx, nil, ErrConcurrentUse
	}

	release := func() { s.tracker.Release(c) }

	// In a transaction whose end ant sees, the entries this operation begins
	// tracking are noted, for its rollback to forget.
	if t := s.txOf(ctx); t != nil && t.sees() {
		from := s.tracker.Mark()

		release = func() {
			t.track(s.tracker, from, s.tracker.Mark())
			s.tracker.Release(c)
		}
	}

	return context.WithValue(ctx, claimed{s.tracker}, c), release, nil
}

// touchesTracker reports whether a write on s may change tracked entities: with
// tracking on it tracks what it writes, and with it off, Add and Attach may still
// have put the entity it writes in the tracker.
func (s *state) touchesTracker() bool {
	return s.settings.tracking || !s.tracker.Empty()
}

func (c *Context) context() *Context { return c }

// Handle is any value that embeds Context, such as a *AppDB.
type Handle interface {
	context() *Context
}

type tableBinder interface {
	bind(*state) error
}

// Open connects to a database and returns a populated context. DB must be a
// struct that embeds Context. The application imports the database driver. The
// context does not track changes, so it is safe to share; a Session is a unit
// of work that does, and WithTracking(true) makes this one track.
func Open[DB any](
	ctx context.Context,
	d dialect.Dialect,
	dsn string,
	options ...Option,
) (*DB, error) {
	if _, ok := any(new(DB)).(Handle); !ok {
		return nil, noContext[DB]()
	}

	if checker, ok := d.(dialect.DSNChecker); ok {
		if err := checker.CheckDSN(dsn); err != nil {
			return nil, err
		}
	}

	pool, err := sql.Open(d.DriverName(), dsn)
	if err != nil {
		return nil, err
	}

	if err := pool.PingContext(ctx); err != nil {
		return nil, errors.Join(err, pool.Close())
	}

	if locker, ok := d.(dialect.BeginLocker); ok && locker.BeginLocks(dsn) {
		options = append(slices.Clip(options), func(s *settings) { s.beginLocks = true })
	}

	db, err := wrap[DB](pool, pool, d, options)
	if err != nil {
		return nil, errors.Join(err, pool.Close())
	}

	return db, nil
}

// Wrap returns a context on an existing pool. The pool stays yours, but Close
// closes it, as with Open.
func Wrap[DB any](d dialect.Dialect, pool *sql.DB, options ...Option) (*DB, error) {
	if _, ok := any(new(DB)).(Handle); !ok {
		return nil, noContext[DB]()
	}

	return wrap[DB](pool, pool, d, options)
}

func wrap[DB any](
	pool *sql.DB,
	to exec.Executor,
	d dialect.Dialect,
	options []Option,
) (*DB, error) {
	s := &state{
		db:        pool,
		dialect:   d,
		settings:  defaultSettings(),
		shared:    new(sharedState),
		tracker:   track.New(),
		saving:    new(sync.Mutex),
		observed:  new(observers),
		lastWrite: new(atomic.Int64),
	}

	for _, option := range options {
		option(&s.settings)
	}

	s.settings.events.attach(s.tracker)

	// A ctx bound to a transaction on the pool runs the statements there (#146).
	s.exec = exec.Wrap(routed{pool, to}, s.settings.interceptors, false, s.settings.detailed)

	for _, replica := range s.settings.replicas {
		s.replicas = append(
			s.replicas,
			exec.Wrap(replica, s.settings.interceptors, true, s.settings.detailed),
		)
	}

	if err := checkModels(new(DB), d); err != nil {
		return nil, err
	}

	return bindState[DB](s)
}

// bindState makes a new DB around s.
func bindState[DB any](s *state) (*DB, error) {
	db := new(DB)
	s.self = db

	any(db).(Handle).context().state = s

	if err := bindTables(db, s); err != nil {
		return nil, err
	}

	return db, nil
}

// Session returns a new context on the same connections with its own tracker,
// which suits one unit of work, such as one web request. A session tracks, even
// on a context with tracking off such as the one Open returns, so SaveChanges
// saves what it loaded and changed; WithTracking(false) turns that off.
//
// A session takes its other settings from db, and options change them for the
// session. Interceptors, a logger and replicas it is given add to db's, for every
// statement the session runs, in a transaction or not.
func Session[DB any](db *DB, options ...Option) *DB {
	h := any(db).(Handle)

	parent := h.context().state
	if parent == nil {
		return new(DB)
	}

	s := *parent
	s.tracker, s.saving = track.New(), new(sync.Mutex)
	s.lastWrite = new(atomic.Int64)

	// A session is a unit of work, whatever db does (#148).
	s.settings.tracking = true

	// The options append to these lists, which the copy shares with db and with
	// its other sessions: clipped, an append makes a list of the session's own.
	s.settings.interceptors = slices.Clip(s.settings.interceptors)
	s.settings.replicas = slices.Clip(s.settings.replicas)

	for _, option := range options {
		option(&s.settings)
	}

	// The executors came wrapped with db's interceptors and error detail, so
	// they are wrapped again with the session's, or its options would reach
	// only the statements of transactions it opens (#210).
	s.exec = rewrap(parent.exec, &s.settings, false)
	s.replicas = make([]exec.Executor, 0, len(s.settings.replicas))

	for _, replica := range parent.replicas {
		s.replicas = append(s.replicas, rewrap(replica, &s.settings, true))
	}

	for _, pool := range s.settings.replicas[len(parent.settings.replicas):] {
		s.replicas = append(
			s.replicas,
			exec.Wrap(pool, s.settings.interceptors, true, s.settings.detailed),
		)
	}

	s.settings.events.attach(s.tracker)

	session, err := bindState[DB](&s)
	if err != nil {
		return new(DB) // the same tables bound without error in Open
	}

	return session
}

// rewrap returns e wrapped with the interceptors and error detail of settings
// in place of those it had. A savepoint's guard stays outside them, so the
// statements still stop once the savepoint ends.
func rewrap(e exec.Executor, settings *settings, replica bool) exec.Executor {
	switch e := e.(type) {
	case savepointExec:
		return savepointExec{rewrap(e.Executor, settings, replica), e.sp}
	case *exec.Plain:
		return exec.Wrap(e.Executor, settings.interceptors, replica, settings.detailed)
	case *exec.Observed:
		return exec.Wrap(e.Executor, settings.interceptors, replica, settings.detailed)
	}

	return e
}

func bindTables(db any, s *state) error {
	return eachTable(db, func(table any) error { return table.(tableBinder).bind(s) })
}

// Close closes the underlying connection pool, for every context that shares
// it.
func (c *Context) Close() error {
	if c.state == nil {
		return nil
	}

	err := c.state.db.Close()

	for _, replica := range c.state.settings.replicas {
		err = errors.Join(err, replica.Close())
	}

	return err
}

// DB returns the underlying pool, for settings such as SetMaxOpenConns.
func (c *Context) DB() *sql.DB {
	if c.state == nil {
		return nil
	}

	return c.state.db
}

// Dialect returns the dialect the context was opened with.
func (c *Context) Dialect() dialect.Dialect {
	if c.state == nil {
		return nil
	}

	return c.state.dialect
}
