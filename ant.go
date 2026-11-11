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
package ant

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"reflect"
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
	tx        *txState      // nil outside a transaction
	lastWrite *atomic.Int64 // when this session last wrote, for sticky reads
}

// sharedState is mutable state shared by a context's sessions.
type sharedState struct {
	nextReplica atomic.Uint64
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

	return context.WithValue(ctx, claimed{s.tracker}, c), func() { s.tracker.Release(c) }, nil
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
// struct that embeds Context. The application imports the database driver.
func Open[DB any](
	ctx context.Context,
	d dialect.Dialect,
	dsn string,
	options ...Option,
) (*DB, error) {
	if _, ok := any(new(DB)).(Handle); !ok {
		return nil, fmt.Errorf("%w: %T", ErrNoContext, new(DB))
	}

	pool, err := sql.Open(d.DriverName(), dsn)
	if err != nil {
		return nil, err
	}

	if err := pool.PingContext(ctx); err != nil {
		return nil, errors.Join(err, pool.Close())
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
		return nil, fmt.Errorf("%w: %T", ErrNoContext, new(DB))
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

	s.exec = exec.Wrap(to, s.settings.interceptors, false, s.settings.detailed)

	for _, replica := range s.settings.replicas {
		s.replicas = append(
			s.replicas,
			exec.Wrap(replica, s.settings.interceptors, true, s.settings.detailed),
		)
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
// which suits one unit of work, such as one web request. Tracking on a long-lived
// context grows without end, so prefer a session per request.
//
// A session takes its settings from db, and options change them for the session.
// An application that keeps tracking off on its shared context passes
// WithTracking(true) to the sessions that use SaveChanges, which otherwise has
// nothing to save.
func Session[DB any](db *DB, options ...Option) *DB {
	h := any(db).(Handle)

	parent := h.context().state
	if parent == nil {
		return new(DB)
	}

	s := *parent
	s.tracker, s.saving = track.New(), new(sync.Mutex)
	s.lastWrite = new(atomic.Int64)

	for _, option := range options {
		option(&s.settings)
	}

	s.settings.events.attach(s.tracker)

	session, err := bindState[DB](&s)
	if err != nil {
		return new(DB) // the same tables bound without error in Open
	}

	return session
}

func bindTables(db any, s *state) error {
	v := reflect.ValueOf(db).Elem()
	for i := range v.NumField() {
		f := v.Field(i)
		if !f.CanAddr() || !f.Addr().CanInterface() {
			continue
		}

		if t, ok := f.Addr().Interface().(tableBinder); ok {
			if err := t.bind(s); err != nil {
				return err
			}
		}
	}

	return nil
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
