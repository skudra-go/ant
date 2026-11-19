package migrate

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/skudra-go/ant"
	"github.com/skudra-go/ant/dialect"
	"github.com/skudra-go/ant/schema"
)

// State says where a migration stands.
type State string

// Migration states.
const (
	Pending    State = "pending"
	Done       State = "applied"
	Failed     State = "failed"   // it stopped partway, outside a transaction
	Changed    State = "changed"  // applied, but its file differs
	Missing    State = "missing"  // applied, but it has no file
	Outdated   State = "outdated" // repeatable, and its file differs: it will rerun
	Removed    State = "removed"  // repeatable, applied, and its file is gone
	Skipped    State = "skipped"  // listed in Config.Skip
	Late       State = "late"     // pending, and older than an applied migration
	Superseded State = "squashed" // replaced by a baseline
	Absorbed   State = "absorbed" // a baseline this database already covers
)

// Entry is the status of one migration.
type Entry struct {
	Kind      Kind
	ID        string
	State     State
	AppliedAt time.Time
	Duration  time.Duration
	Error     string

	migration *Migration
}

// engine applies migrations to one database.
type engine struct {
	pool       *sql.DB
	d          schema.Dialect
	config     Config
	source     Source
	history    history
	registered []Migration
	bind       func(tx *sql.Tx) (any, error)
	plain      any
	seeders    []seeder
	output     sync.Mutex // the lock timer can print alongside the main path
}

// opened is what every context has, through its embedded ant.Context.
type opened interface {
	DB() *sql.DB
	Dialect() dialect.Dialect
}

func newEngine[DB any](db *DB, config Config) (*engine, error) {
	h, ok := any(db).(opened)
	if !ok {
		return nil, ant.ErrNoContext
	}

	if h.DB() == nil {
		return nil, ant.ErrNotOpen
	}

	d, err := schema.Of(h.Dialect())
	if err != nil {
		return nil, err
	}

	config = config.resolve(d.Name())

	source, err := config.source()
	if err != nil {
		return nil, err
	}

	return &engine{
		pool:       h.DB(),
		d:          d,
		config:     config,
		source:     source,
		history:    history{d, config.HistorySchema, config.HistoryTable},
		registered: registeredMigrations[DB](),
		seeders:    registeredSeeders[DB](),
		plain:      db,
		bind:       func(tx *sql.Tx) (any, error) { return ant.UseTx(db, tx) },
	}, nil
}

// Up applies pending migrations, then repeatable ones that are new or changed. It
// is safe to call from several processes at once: the others wait, then find
// nothing to do.
func Up[DB any](ctx context.Context, db *DB, config ...Config) error {
	e, err := newEngine(db, single(config))
	if err != nil {
		return err
	}

	return e.up(ctx, "")
}

// Status returns every migration and where it stands, without changing anything.
func Status[DB any](ctx context.Context, db *DB, config ...Config) ([]Entry, error) {
	e, err := newEngine(db, single(config))
	if err != nil {
		return nil, err
	}

	return e.status(ctx)
}

func (e *engine) say(format string, args ...any) {
	e.output.Lock()
	defer e.output.Unlock()

	fmt.Fprintf(e.config.Out, format+"\n", args...)
}

// locked runs body on one connection, holding the migration lock.
func (e *engine) locked(ctx context.Context, body func(conn *sql.Conn) error) (err error) {
	conn, err := e.pool.Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()

	if e.config.NoLock {
		return body(conn)
	}

	waiting := time.AfterFunc(
		2*time.Second,
		func() { e.say("Waiting for another migrator to finish...") },
	)

	release, err := e.d.Lock(ctx, conn, "ant_migrate:"+e.history.name())

	waiting.Stop()

	if err != nil {
		return fmt.Errorf("ant: take migration lock: %w", err)
	}

	defer func() {
		// A cancelled context must not leave the lock held.
		err = errors.Join(err, release(context.WithoutCancel(ctx)))
	}()

	return body(conn)
}

func (e *engine) catalog(ctx context.Context) (*catalog, error) {
	return load(ctx, e.source, e.registered)
}

// entries works out each migration's state.
func (e *engine) entries(c *catalog, rows []record) []Entry {
	applied := map[Kind]map[string]record{Versioned: {}, Repeatable: {}}
	newest := ""

	for _, r := range rows {
		if applied[r.Kind] == nil {
			continue
		}

		applied[r.Kind][r.Key] = r

		if r.Kind == Versioned && r.Success &&
			(newest == "" || compareVersions(r.Key, newest) > 0) {
			newest = r.Key
		}
	}

	var out []Entry

	for i := range c.versioned {
		m := &c.versioned[i]
		entry := Entry{Kind: Versioned, ID: m.ID(), migration: m}
		r, ok := applied[Versioned][m.Version]
		delete(applied[Versioned], m.Version)

		entry.State = e.versionedState(c, m, r, ok, newest)

		if ok {
			entry.AppliedAt, entry.Duration, entry.Error = r.AppliedAt, r.Duration, r.Error
		}

		out = append(out, entry)
	}

	for key, r := range applied[Versioned] {
		state := Missing

		if c.supersededBy(key) != nil {
			state = Superseded
		}

		out = append(out, Entry{
			Kind:      Versioned,
			ID:        key + "_" + r.Name,
			State:     state,
			AppliedAt: r.AppliedAt,
			Duration:  r.Duration,
		})
	}

	slices.SortStableFunc(
		out,
		func(a, b Entry) int { return compareVersions(versionOf(a.ID), versionOf(b.ID)) },
	)

	for i := range c.repeatable {
		m := &c.repeatable[i]
		entry := Entry{
			Kind:      Repeatable,
			ID:        m.ID(),
			State:     Pending,
			migration: m,
		}

		r, ok := applied[Repeatable][m.key()]
		delete(applied[Repeatable], m.key())

		if ok {
			entry.AppliedAt, entry.Duration, entry.Error = r.AppliedAt, r.Duration, r.Error
			entry.State = Done

			if r.Checksum != m.Checksum() || !r.Success {
				entry.State = Outdated
			}
		}

		out = append(out, entry)
	}

	gone := make([]Entry, 0, len(applied[Repeatable]))

	for key, r := range applied[Repeatable] {
		gone = append(gone, Entry{
			Kind:      Repeatable,
			ID:        key,
			State:     Removed,
			AppliedAt: r.AppliedAt,
		})
	}

	slices.SortFunc(gone, func(a, b Entry) int { return strings.Compare(a.ID, b.ID) })

	return append(out, gone...)
}

func versionOf(id string) string {
	version, _, _ := splitID(id)

	return version
}

func (e *engine) versionedState(c *catalog, m *Migration, r record, ok bool, newest string) State {
	switch {
	case ok && !r.Success:
		return Failed
	case ok && m.run == nil && r.Checksum != m.Checksum():
		return Changed
	case ok:
		return Done
	case slices.Contains(e.config.Skip, m.Version):
		return Skipped
	case c.supersededBy(m.Version) != nil:
		return Superseded
	case m.replaces != "" && newest != "":
		return Absorbed
	case newest != "" && compareVersions(m.Version, newest) < 0:
		return Late
	}

	return Pending
}

func (e *engine) status(ctx context.Context) ([]Entry, error) {
	return e.statusFrom(ctx, e.source)
}

// statusFrom is status over the migrations source lists.
func (e *engine) statusFrom(ctx context.Context, source Source) ([]Entry, error) {
	c, err := load(ctx, source, e.registered)
	if err != nil {
		return nil, err
	}

	rows, err := e.history.read(ctx, e.pool)
	if err != nil {
		return nil, err
	}

	return e.entries(c, rows), nil
}

// validate refuses to go on from a state that needs a person to look at it.
func (e *engine) validate(entries []Entry, rows []record) error {
	var problems []error

	for _, entry := range entries {
		switch entry.State {
		case Failed:
			problems = append(
				problems,
				fmt.Errorf(
					"%w: %s: %s. Check the database, then run repair",
					ErrFailed,
					entry.ID,
					entry.Error,
				),
			)
		case Changed:
			problems = append(
				problems,
				fmt.Errorf(
					"%w: %s. Applied migrations must not change: put the file back, write a new migration, or run repair if the edit changes nothing",
					ErrChecksum,
					entry.ID,
				),
			)
		case Missing:
			problems = append(
				problems,
				fmt.Errorf(
					"%w: %s. Put the file back, or register its Go migration, or run repair to forget it",
					ErrMissing,
					entry.ID,
				),
			)
		case Late:
			if !e.config.OutOfOrder {
				problems = append(
					problems,
					fmt.Errorf(
						"%w: %s. Renumber it after the newest applied migration, or set Config.OutOfOrder",
						ErrOutOfOrder,
						entry.ID,
					),
				)
			}
		case Absorbed:
			m := entry.migration

			if !slices.ContainsFunc(rows, func(r record) bool {
				return r.Kind == Versioned && r.Success && compareVersions(r.Key, m.replaces) >= 0
			}) {
				problems = append(
					problems,
					fmt.Errorf(
						"%w: %s replaces everything through %s, which this database has not reached. Apply the older migrations from a release before the squash",
						ErrBehindSquash,
						m.ID(),
						m.replaces,
					),
				)
			}
		}
	}

	return errors.Join(problems...)
}

// up applies what is pending, through version to if it is set.
func (e *engine) up(ctx context.Context, to string) error {
	return e.locked(ctx, func(conn *sql.Conn) error {
		// The migrations first, so that a folder that is not there records
		// nothing.
		c, err := e.catalog(ctx)
		if err != nil {
			return err
		}

		if err := e.history.ensure(ctx, conn); err != nil {
			return err
		}

		rows, err := e.history.read(ctx, conn)
		if err != nil {
			return err
		}

		entries := e.entries(c, rows)
		if err := e.validate(entries, rows); err != nil {
			return err
		}

		count := 0

		for _, entry := range entries {
			if entry.migration == nil {
				continue
			}

			m := *entry.migration

			switch {
			case to != "" && m.Kind == Repeatable:
				continue
			case to != "" && compareVersions(m.Version, to) > 0:
				continue
			case entry.State == Absorbed:
				if err := e.absorb(ctx, conn, m); err != nil {
					return err
				}
			case entry.State == Pending, entry.State == Outdated, entry.State == Late:
				if err := e.apply(ctx, conn, m); err != nil {
					return err
				}

				count++
			}
		}

		if count == 0 {
			e.say("Nothing to apply: the database is up to date.")
		}

		return nil
	})
}

// absorb records a baseline without running it, since the database already has
// everything it describes.
func (e *engine) absorb(ctx context.Context, conn *sql.Conn, m Migration) error {
	e.say("Recorded %s: it replaces migrations this database already has.", m.ID())

	return e.history.write(ctx, conn, record{
		Kind:      m.Kind,
		Key:       m.key(),
		Name:      m.Name,
		Checksum:  m.Checksum(),
		AppliedAt: time.Now(),
		Success:   true,
	})
}

// apply runs one migration and records it. In a transaction, the two happen
// together, so a crash cannot leave a migration half done or done but unrecorded.
func (e *engine) apply(ctx context.Context, conn *sql.Conn, m Migration) error {
	if m.run == nil && len(e.d.Split(m.SQL)) == 0 {
		return fmt.Errorf(
			"%w: %s has no statements. Write it, or delete the file",
			ErrBadMigration,
			m.ID(),
		)
	}

	started := time.Now()
	r := record{
		Kind:      m.Kind,
		Key:       m.key(),
		Name:      m.Name,
		Checksum:  m.Checksum(),
		AppliedAt: started,
		Success:   true,
	}

	var err error

	if m.noTransaction {
		err = e.applyPlain(ctx, conn, m, &r)
	} else {
		err = e.applyInTx(ctx, conn, m, &r)
	}

	if err != nil {
		return err
	}

	e.say("Applied %s (%s)", m.ID(), r.Duration.Round(time.Millisecond))

	if e.config.OnApply != nil {
		e.config.OnApply(Applied{Migration: m, Duration: r.Duration})
	}

	return nil
}

func (e *engine) applyInTx(ctx context.Context, conn *sql.Conn, m Migration, r *record) error {
	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if err := e.run(ctx, tx, m); err != nil {
		return e.explain(m, err)
	}

	if err := e.resetSession(ctx, tx); err != nil {
		return err
	}

	r.Duration = time.Since(r.AppliedAt)

	if err := e.history.write(ctx, tx, *r); err != nil {
		return fmt.Errorf("ant: record %s: %w", m.ID(), err)
	}

	return tx.Commit()
}

// unfinished is what history says of a migration with no transaction around it
// while it runs, and after a process that died while running it.
const unfinished = "it did not finish: the process running up stopped partway, or is still running it"

// applyPlain runs a migration with no transaction around it. A failure may leave
// part of it applied, so it is recorded, and up refuses to continue until someone
// has looked. It is recorded as failed before it starts, too, so a process that
// is killed partway leaves it failed rather than pending, which up would run
// again from the first statement.
func (e *engine) applyPlain(ctx context.Context, conn *sql.Conn, m Migration, r *record) error {
	started := *r
	started.Success, started.Error = false, unfinished

	if err := e.history.writeAlone(ctx, conn, started); err != nil {
		return fmt.Errorf("ant: record %s: %w", m.ID(), err)
	}

	var failure error

	if m.run != nil {
		failure = m.run(ctx, e.plain, nil)
	} else {
		for _, statement := range e.d.Split(m.SQL) {
			if _, err := conn.ExecContext(ctx, statement); err != nil {
				failure = fmt.Errorf("%w\n\nThe statement was:\n%s", err, statement)

				break
			}
		}
	}

	r.Duration = time.Since(r.AppliedAt)
	// The context may be why it failed, and these must still run.
	calm := context.WithoutCancel(ctx)
	if err := e.resetSession(calm, conn); err != nil {
		return errors.Join(failure, err)
	}

	if failure != nil {
		r.Success, r.Error = false, failure.Error()

		return errors.Join(
			fmt.Errorf(
				"ant: migration %s failed partway, outside a transaction: %w",
				m.ID(),
				failure,
			),
			e.history.writeAlone(calm, conn, *r),
		)
	}

	return e.history.writeAlone(ctx, conn, *r)
}

func (e *engine) resetSession(ctx context.Context, x execer) error {
	statement := e.d.SessionReset()
	if statement == "" {
		return nil
	}

	_, err := x.ExecContext(ctx, statement)

	return err
}

func (e *engine) run(ctx context.Context, tx *sql.Tx, m Migration) error {
	if m.run == nil {
		_, err := tx.ExecContext(ctx, m.SQL)

		return err
	}

	db, err := e.bind(tx)
	if err != nil {
		return err
	}

	return m.run(ctx, db, tx)
}

// explain adds the failing migration, its line, and the database's own detail to
// err. PostgreSQL gets a whole file at once, so without this a message says nothing
// about where.
func (e *engine) explain(m Migration, err error) error {
	failure := e.d.Explain(err)
	where, more := "", ""

	if runes := []rune(m.SQL); failure.Offset > 0 && failure.Offset <= len(runes) {
		line := strings.Count(string(runes[:failure.Offset-1]), "\n") + 1
		where = fmt.Sprintf(", at line %d", line)

		more = fmt.Sprintf(
			"\n\n  %d | %s",
			line,
			strings.TrimSpace(strings.Split(m.SQL, "\n")[line-1]),
		)
	}

	if failure.Detail != "" {
		more += "\n  " + failure.Detail
	}

	if failure.Hint != "" {
		more += "\n  Hint: " + failure.Hint
	}

	return fmt.Errorf(
		"ant: migration %s failed and was rolled back%s: %w%s",
		m.ID(),
		where,
		err,
		more,
	)
}
