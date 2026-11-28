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
	Outdated   State = "outdated" // repeatable, and its file differs or a versioned migration ran since: it will rerun
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
	// appliedAs is the name the version was applied under, when it differs
	// from the file's and so do the contents: another migration took the
	// number, as when two branches both wrote it.
	appliedAs string
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

	// lastVersioned is when the latest versioned migration was applied. A code
	// file applied before it may define what that migration dropped or changed.
	var lastVersioned time.Time

	for _, r := range rows {
		if applied[r.Kind] == nil {
			continue
		}

		applied[r.Kind][r.Key] = r

		if r.Kind == Versioned && r.Success &&
			(newest == "" || compareVersions(r.Key, newest) > 0) {
			newest = r.Key
		}

		if r.Kind == Versioned && r.Success && r.AppliedAt.After(lastVersioned) {
			lastVersioned = r.AppliedAt
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

		if entry.State == Changed && r.Name != m.Name {
			entry.appliedAs = r.Key + "_" + r.Name
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

			if r.Checksum != m.Checksum() || !r.Success || r.AppliedAt.Before(lastVersioned) {
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
			if entry.appliedAs != "" {
				problems = append(problems, renumbered(entry))

				continue
			}

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

// renumbered is the error for a version applied as another migration than its
// file. Read as an edit, it led to repair, which recorded the file as applied
// without running it (#178).
func renumbered(entry Entry) error {
	return fmt.Errorf(
		"%w: %s was applied as %s, a different migration. If it was renumbered after a "+
			"merge, give the file the next free number; if it is the same migration, name it %s again",
		ErrChecksum,
		entry.ID,
		entry.appliedAs,
		entry.appliedAs,
	)
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
		versioned := false

		for _, entry := range entries {
			if entry.migration == nil {
				continue
			}

			m := *entry.migration

			switch {
			case !within(m, to):
				continue
			case entry.State == Absorbed:
				if err := e.absorb(ctx, conn, m); err != nil {
					return err
				}
			case runs(entry), rerunsAfter(entry, versioned):
				if err := e.apply(ctx, conn, m); err != nil {
					return err
				}

				count++
				versioned = versioned || m.Kind == Versioned
			}
		}

		if count == 0 {
			e.say("Nothing to apply: the database is up to date.")
		}

		return nil
	})
}

// within reports whether up, through version to if it is set, reaches m. Through
// a version, up applies no code file.
func within(m Migration, to string) bool {
	if to == "" {
		return true
	}

	return m.Kind == Versioned && compareVersions(m.Version, to) <= 0
}

// runs reports whether up applies entry as it stands: a pending migration, or a
// code file that is outdated.
func runs(entry Entry) bool {
	return slices.Contains([]State{Pending, Outdated, Late}, entry.State)
}

// rerunsAfter reports whether up reruns a code file that is up to date, because
// versioned migrations ran before it in the same up. Such a migration may have
// dropped a view the file defines, to change a column the view reads, and the
// file is the only place the view is defined.
func rerunsAfter(entry Entry, versioned bool) bool {
	return versioned && entry.Kind == Repeatable && entry.State == Done
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

	if err := m.checkBody(e.d); err != nil {
		return err
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

	if err := e.ran(ctx, tx, m); err != nil {
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

	// The file has run, so the context ending now must not leave it unfinished.
	return e.history.writeAlone(calm, conn, *r)
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

// ran runs a file's statements one at a time inside tx. PostgreSQL runs a whole
// file as one string, but a failure then says nothing about which statement it
// was unless the driver sends a position, which DDL and constraint errors do not
// (#303). Running them one by one, in the same transaction, lets the failing
// statement and its line be named. It is one round trip per statement instead of
// one per file, which a large data migration notices.
//
// A file that ends its own transaction never gets here: checkBody has refused it
// before the transaction began.
func (e *engine) ran(ctx context.Context, tx *sql.Tx, m Migration) error {
	if m.run != nil || m.noTransaction {
		return e.run(ctx, tx, m)
	}

	lines, ok := splitWithLines(e.d.Split(m.SQL), m.SQL)
	if !ok || len(lines) == 0 {
		return e.run(ctx, tx, m)
	}

	for _, s := range lines {
		if _, err := tx.ExecContext(ctx, s.text); err != nil {
			return &statementError{
				line:    s.line,
				skipped: s.skipped,
				text:    s.text,
				err:     err,
			}
		}
	}

	return nil
}

// statementError is a statement that failed, with where it starts in the file.
// line is the statement's first line that carries code; skipped counts the
// leading comment and blank lines before it, which a position, counted from the
// statement's text, still includes.
type statementError struct {
	line    int
	skipped int
	text    string
	err     error
}

func (e *statementError) Error() string { return e.err.Error() }
func (e *statementError) Unwrap() error { return e.err }

// lineOf returns the line in the file that offset points at, where offset is a
// position within this statement in characters from 1, as a driver sends it, or
// the statement's first code line when it is past the text. The offset counts
// from the statement's text, comment lines included, so the lines skipped before
// its code come off.
func (e *statementError) lineOf(offset int) int {
	runes := []rune(e.text)
	if offset < 1 || offset > len(runes) {
		return e.line
	}

	return e.line - e.skipped + strings.Count(string(runes[:offset-1]), "\n")
}

// splitWithLines matches each statement Split returns back to the line it starts
// on, by finding it in the script in order. Split keeps the comments before a
// statement with it, so the line is the first that carries code, not the first
// comment. A statement Split rebuilt so that it is not a substring of the script
// leaves ok false, and the caller runs the file whole, as before.
func splitWithLines(statements []string, script string) ([]splitLine, bool) {
	out := make([]splitLine, 0, len(statements))

	offset := 0

	for _, statement := range statements {
		begin, ok := findStatement(script, statement, offset)
		if !ok {
			return nil, false
		}

		code := lineOfCode(script[begin : begin+len(statement)])
		out = append(out, splitLine{
			line:    strings.Count(script[:begin], "\n") + code,
			skipped: code - 1,
			text:    statement,
		})

		// Look past this statement for the next, so two alike are told apart.
		offset = begin + len(statement)
	}

	return out, true
}

// findStatement returns where the statement starts in script at or after from.
//
// A statement starts where the one before it ended: at the top of the file, or
// after the semicolon that closed the previous statement. A match anywhere else,
// as inside a comment that quotes the statement, is another occurrence, and the
// search moves past it rather than taking it, so a comment above cannot claim the
// statement's line.
func findStatement(script, statement string, from int) (int, bool) {
	for from <= len(script) {
		at := strings.Index(script[from:], statement)
		if at < 0 {
			return 0, false
		}

		begin := from + at
		if beginsStatement(script, begin) {
			return begin, true
		}

		from = begin + 1
	}

	return 0, false
}

// beginsStatement reports whether i is where a statement starts: at the top of
// the script, or after the semicolon that ended the one before, with only
// whitespace or a comment between.
func beginsStatement(script string, i int) bool {
	if i == 0 {
		return true
	}

	for j := i - 1; j >= 0; j-- {
		switch script[j] {
		case ' ', '\t', '\r', '\n':
			continue
		case ';':
			return true
		default:
			return false
		}
	}

	return true
}

// lineOfCode returns the line within text that first carries code, counting
// from 1, skipping the blank lines and the comments before it: a line comment to
// its line's end, and a block comment to where it closes, over the lines it
// runs and the block comments nested in it, as PostgreSQL reads them. Text that
// is all comment gives its last line.
func lineOfCode(text string) int {
	i := 0

	for i < len(text) {
		switch {
		case text[i] == ' ' || text[i] == '\t' || text[i] == '\r' || text[i] == '\n':
			i++
		case strings.HasPrefix(text[i:], "--"):
			end := strings.IndexByte(text[i:], '\n')
			if end < 0 {
				return 1 + strings.Count(text, "\n")
			}

			i += end + 1
		case strings.HasPrefix(text[i:], "/*"):
			i = afterBlockComment(text, i)
		default:
			return 1 + strings.Count(text[:i], "\n")
		}
	}

	return 1 + strings.Count(text, "\n")
}

// afterBlockComment returns where the block comment that opens at i ends, past
// its closing mark, or the end of text when it is never closed.
func afterBlockComment(text string, i int) int {
	depth := 0

	for i < len(text) {
		switch {
		case strings.HasPrefix(text[i:], "/*"):
			depth++
			i += 2
		case strings.HasPrefix(text[i:], "*/"):
			depth--
			i += 2

			if depth == 0 {
				return i
			}
		default:
			i++
		}
	}

	return len(text)
}

type splitLine struct {
	line    int
	skipped int
	text    string
}

// explain adds the failing migration, its line, and the database's own detail to
// err. A driver that sends a position gives the line within the text it ran; a
// statement ran() ran alone is positioned within that statement, so the file
// before it is added. A driver that sends no position, as for most DDL and
// constraint errors, leaves it to the statement that failed, which ran() knows.
func (e *engine) explain(m Migration, err error) error {
	failure := e.d.Explain(err)
	where, more := "", ""

	failed, isStatement := errors.AsType[*statementError](err)

	switch {
	case failure.Offset > 0 && isStatement:
		if line := failed.lineOf(failure.Offset); line > 0 {
			where, more = lineText(m.SQL, line)
		}
	case failure.Offset > 0:
		if runes := []rune(m.SQL); failure.Offset <= len(runes) {
			line := strings.Count(string(runes[:failure.Offset-1]), "\n") + 1
			where, more = lineText(m.SQL, line)
		}
	case isStatement:
		where, more = lineText(m.SQL, failed.line)
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

// lineText returns the ", at line N" part and the numbered source line, for a
// line of the file, or nothing when it is out of range.
func lineText(script string, line int) (string, string) {
	lines := strings.Split(script, "\n")
	if line < 1 || line > len(lines) {
		return "", ""
	}

	return fmt.Sprintf(", at line %d", line), fmt.Sprintf(
		"\n\n  %d | %s",
		line,
		strings.TrimSpace(lines[line-1]),
	)
}
