package migrate

import (
	"errors"
	"io"
	"io/fs"
	"path"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/skudra-go/ant"
	"github.com/skudra-go/ant/schema"
)

// Errors returned by commands and by Up.
var (
	// ErrChecksum reports an applied migration whose file has changed. Restore
	// the file, or run repair if the change was deliberate.
	ErrChecksum = errors.New("ant: applied migration was changed")
	// ErrMissing reports an applied migration with no file.
	ErrMissing = errors.New("ant: applied migration is missing")
	// ErrFailed reports a migration that failed partway outside a transaction,
	// so the database needs a look. Fix it, then run repair.
	ErrFailed = errors.New("ant: a migration failed partway")
	// ErrOutOfOrder reports a pending migration older than one already applied,
	// which usually comes from merging two branches.
	ErrOutOfOrder = errors.New("ant: pending migration is older than an applied one")
	// ErrDuplicateVersion reports two migrations with one version, which usually
	// comes from merging two branches.
	ErrDuplicateVersion = errors.New("ant: two migrations share a version")
	// ErrBehindSquash reports a database too old for a squashed baseline.
	ErrBehindSquash = errors.New("ant: database is behind a squashed baseline")
	// ErrPendingChanges reports models that differ from the snapshot.
	ErrPendingChanges = errors.New("ant: models have changes with no migration")
	// ErrStaleSnapshot reports a snapshot that is out of step with the
	// migration files.
	ErrStaleSnapshot = errors.New("ant: snapshot does not match the migration files")
	// ErrDrift reports a database that differs from what migrations describe.
	ErrDrift = errors.New("ant: database differs from the migrations")
	// ErrPending reports migrations that have not been applied, from status
	// --check.
	ErrPending = errors.New("ant: migrations are pending")
	// ErrReadOnly reports a command that writes, on a Source that cannot.
	ErrReadOnly = errors.New("ant: migration source cannot be written")
	// ErrBadMigration reports a migration that cannot be used as it is.
	ErrBadMigration = errors.New("ant: bad migration")
	// ErrUnconfirmed reports a destructive command that was not confirmed.
	ErrUnconfirmed = errors.New("ant: not confirmed")
)

// Config adjusts how migrations work. Every field is optional.
type Config struct {
	// Dir is the folder holding the migration folders. The default is
	// "migrations", or the root of FS.
	Dir string
	// FS reads migrations from a file system, such as a go:embed one, instead
	// of the disk. Commands that write files need a real folder.
	FS fs.FS
	// Embedded is used in place of Dir when that folder is not on disk, so one
	// program can generate migrations in a repository and apply them once
	// deployed, where only its binary exists.
	Embedded fs.FS
	// Source replaces file handling completely.
	Source Source
	// VersionsDir and CodeDir name the two folders inside Dir. The defaults are
	// "versions" and "code".
	VersionsDir string
	CodeDir     string
	// PerDialect keeps each dialect's migrations in Dir/<dialect>, for a project
	// that runs on several databases.
	PerDialect bool

	// Name chooses a new migration's file name. The default is DefaultName, and
	// Timestamped is an alternative.
	Name func(Next) string
	// Parse reads a version and name from a file name. Set it if Name writes a
	// form the default cannot read: 0001_name.up.sql, 0001_name.sql, and
	// V1__name.sql all work without it.
	Parse func(file string) (version, name string, ok bool)
	// Render replaces how a plan becomes SQL. The default is schema.Script, which
	// a replacement can wrap.
	Render func(*schema.Plan, schema.Dialect) (string, error)

	// HistoryTable and HistorySchema say where applied migrations are recorded.
	// The default is ant_migrations in the connection's default schema.
	HistoryTable  string
	HistorySchema string

	// DSN connects the commands that need a database. The --dsn flag and the
	// DATABASE_URL variable come first and second, and this is the last resort.
	// Main and Run do not load .env files; the application may load one itself.
	DSN string
	// Options are those the application gives ant.Open. Main and Run pass them
	// to ant.SchemaOf and ant.Open, so that WithNaming and WithConvention name
	// tables and columns in migrations as they do in the application. Up and
	// Status use the context they are given, which was opened with them.
	Options []ant.Option
	// Skip lists versions to leave unapplied, for a migration that must not run
	// in some environment.
	Skip []string
	// OutOfOrder lets a pending migration run even if a newer one is applied.
	OutOfOrder bool
	// Ignore lists names for drift to overlook, as path.Match patterns: table or
	// schema.table for a table, table.object for a column, index, or constraint.
	Ignore []string
	// NoLock turns off the migration lock, for connection poolers that break
	// session locks. Then only one migrator must run at a time.
	NoLock bool
	// SeedHooks runs model hooks while seeding. They are off by default, since
	// hooks usually send mail or write audit rows.
	SeedHooks bool

	// Out receives progress messages. Up is silent by default, and the CLI uses
	// standard output.
	Out io.Writer
	// OnApply runs after each migration is applied, for logs and metrics.
	OnApply func(Applied)

	resolved bool
}

// Applied describes a migration that just ran.
type Applied struct {
	Migration Migration
	Duration  time.Duration
}

// Next describes the migration about to be named.
type Next struct {
	Version string    // the next number, such as 0003
	Name    string    // the name given to the command, cleaned up
	Time    time.Time // now
}

// DefaultName is the default file name: 0003_add_bio.up.sql. The .up suffix means
// golang-migrate can run the same files.
func DefaultName(n Next) string { return n.Version + "_" + n.Name + ".up.sql" }

// Timestamped names files by time, as in 20261031120501_add_bio.up.sql, which
// avoids most version clashes between branches.
func Timestamped(n Next) string {
	return n.Time.UTC().Format("20060102150405") + "_" + n.Name + ".up.sql"
}

const defaultHistory = "ant_migrations"

// resolve fills in defaults, once: PerDialect must not apply twice.
func (c Config) resolve(dialect string) Config {
	if c.resolved {
		return c
	}

	c.resolved = true

	if c.FS == nil && c.Dir == "" {
		c.Dir = "migrations"
	}

	if c.PerDialect {
		c.Dir = path.Join(c.Dir, dialect)
	}

	if c.VersionsDir == "" {
		c.VersionsDir = "versions"
	}

	if c.CodeDir == "" {
		c.CodeDir = "code"
	}

	if c.Name == nil {
		c.Name = DefaultName
	}

	if c.Parse == nil {
		c.Parse = parseFile
	}

	if c.Render == nil {
		c.Render = func(p *schema.Plan, d schema.Dialect) (string, error) {
			return schema.Script(d, p)
		}
	}

	if c.HistoryTable == "" {
		c.HistoryTable = defaultHistory
	}

	if c.Out == nil {
		c.Out = io.Discard
	}

	return c
}

func single(config []Config) Config {
	if len(config) > 0 {
		return config[0]
	}

	return Config{}
}

// slug makes a name safe for a file name.
func slug(name string) string {
	return strings.Trim(nonWord.ReplaceAllString(strings.ToLower(name), "_"), "_")
}

var (
	nonWord  = regexp.MustCompile(`[^a-z0-9]+`)
	fileName = regexp.MustCompile(`^(\d+)[_-](.+?)(\.up)?\.sql$`)
	flyway   = regexp.MustCompile(`^V(\d+)__(.+?)\.sql$`)
)

// parseFile reads the layouts common migration tools use, so that an existing
// folder can be adopted without renaming anything.
func parseFile(file string) (string, string, bool) {
	for _, pattern := range []*regexp.Regexp{fileName, flyway} {
		if m := pattern.FindStringSubmatch(file); m != nil {
			return m[1], m[2], true
		}
	}

	return "", "", false
}

// compareVersions orders versions as numbers when both are, so 9 comes before 10
// whatever the padding.
func compareVersions(a, b string) int {
	x, y := strings.TrimLeft(a, "0"), strings.TrimLeft(b, "0")

	if digits(a) && digits(b) {
		if len(x) != len(y) {
			return len(x) - len(y)
		}

		return strings.Compare(x, y)
	}

	return strings.Compare(a, b)
}

func digits(s string) bool {
	return s != "" && strings.Trim(s, "0123456789") == ""
}

// nextVersion returns the number after the highest numbered version, padded like
// the existing ones.
func nextVersion(migrations []Migration) string {
	width, highest := 4, "0"

	for _, m := range migrations {
		if !digits(m.Version) {
			continue
		}

		width = max(width, len(m.Version))

		if compareVersions(m.Version, highest) > 0 {
			highest = m.Version
		}
	}

	if len(highest) > 18 {
		highest = highest[:18] // beyond any real sequence, and still increasing
	}

	n, _ := strconv.ParseInt(highest, 10, 64)
	text := strconv.FormatInt(n+1, 10)

	return strings.Repeat("0", max(0, width-len(text))) + text
}
