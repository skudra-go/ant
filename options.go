package ant

import (
	"database/sql"
	"time"

	"github.com/skudra-go/ant/internal/exec"
)

// settings holds what Open's options configure.
type settings struct {
	tracking     bool
	autoTx       bool
	now          func() time.Time
	interceptors []exec.Interceptor
	replicas     []*sql.DB
	sticky       time.Duration
	strict       bool
	detailed     bool
	retry        Retry
	events       Events
	diagnostics  *diagnostics
	// beginLocks says a writable transaction holds the database's write lock
	// from its start, which Open reads from the DSN (dialect.BeginLocker).
	beginLocks bool
}

func defaultSettings() settings {
	return settings{autoTx: true, now: time.Now}
}

// timestamp returns the time for automatic columns, in UTC at the database's
// precision, so that a value reads back exactly as it was written.
func (s settings) timestamp() time.Time { return s.now().UTC().Truncate(time.Microsecond) }

// WithNow sets the clock for automatic timestamps, which suits tests.
func WithNow(now func() time.Time) Option { return func(s *settings) { s.now = now } }

// WithReplicas sends reads outside transactions to these pools in turn. Writes,
// locks, and transactions use the primary. Close closes them. A failing replica
// is an error for that read, not a retry elsewhere.
func WithReplicas(pools ...*sql.DB) Option {
	return func(s *settings) { s.replicas = append(s.replicas, pools...) }
}

// WithStickyWrites sends a context's reads to the primary for d after each of its
// writes, so that replication lag cannot hide what it just wrote.
func WithStickyWrites(d time.Duration) Option { return func(s *settings) { s.sticky = d } }

// WithStrict makes a query fail if a result column has no field, which usually
// means a model has fallen behind the schema, or a SELECT * picked up something new.
func WithStrict() Option { return func(s *settings) { s.strict = true } }

// WithLooseScan ignores result columns with no field. It is the default, and
// exists to say so where WithStrict is on for other contexts.
func WithLooseScan() Option { return func(s *settings) { s.strict = false } }

// WithDetailedErrors adds the failing statement to every query error's message,
// which helps in development. SQL text can reveal a schema, and raw SQL can hold
// values, so leave it off where errors reach users. FailedSQL reads the statement
// from an error whether this is on or not.
func WithDetailedErrors() Option { return func(s *settings) { s.detailed = true } }

// Retry says how to run a transaction again after a deadlock or serialization
// failure, which a database reports when two transactions collide and only one
// can win.
type Retry struct {
	// Attempts is the number of runs in all.
	Attempts int
}

// WithRetry applies a retry policy to every Tx and SaveChanges of this context,
// unless Attempts says otherwise for one call. The work must be safe to run again,
// so a function given to Tx should change nothing outside the database.
func WithRetry(policy Retry) Option { return func(s *settings) { s.retry = policy } }

// Option configures a context in Open.
type Option func(*settings)

// WithTracking sets whether a context tracks what it loads, adds, and saves. It
// is off on the context Open and Wrap return, which an application shares, and
// on in a Session, which is one unit of work. With it off, queries return
// entities nothing tracks, and SaveChanges, Add, Attach, MarkModified, Remove,
// and TrackGraph return ErrNoTracking. Open a shared context with it on only if
// one operation at a time uses it, such as a script.
func WithTracking(on bool) Option { return func(s *settings) { s.tracking = on } }

// WithoutAutoTx stops SaveChanges and Insert from opening a transaction of
// their own, so a failure partway leaves earlier statements in place. The
// tracker records those as saved, with their generated keys, so a retry writes
// only the rest. Use it only to control transactions yourself, or where a
// transaction is unavailable.
func WithoutAutoTx() Option { return func(s *settings) { s.autoTx = false } }
