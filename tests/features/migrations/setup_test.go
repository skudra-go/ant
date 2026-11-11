package migrations

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/skudra-go/ant"
	"github.com/skudra-go/ant/migrate"
	"github.com/skudra-go/ant/schema"
	"github.com/skudra-go/ant/tests/features/fixture"
)

// The models below are one application at three points in time. Real projects
// edit their structs instead, but a test needs them all at once, so each version is
// its own type on the same table.

// V1Account is the first version of the model.
type V1Account struct {
	ant.Base
	Name  string
	Email string `ant:"unique"`
}

func (V1Account) Configure(b *ant.ModelBuilder[V1Account]) { b.Table("accounts") }

// V1DB is the application at its first release.
type V1DB struct {
	ant.Context
	Accounts ant.Table[V1Account]
}

// V2Account adds a nullable bio and a required age.
type V2Account struct {
	ant.Base
	Name  string
	Email string `ant:"unique"`
	Bio   *string
	Age   int
}

func (V2Account) Configure(b *ant.ModelBuilder[V2Account]) { b.Table("accounts") }

// V2DB is the application after its first schema change.
type V2DB struct {
	ant.Context
	Accounts ant.Table[V2Account]
}

// V3Account renames Name, and has notes.
type V3Account struct {
	ant.Base
	DisplayName string
	Email       string `ant:"unique"`
	Bio         *string
	Age         int
	Notes       []V3Note
}

func (V3Account) Configure(b *ant.ModelBuilder[V3Account]) {
	b.Table("accounts")
	b.HasMany(&V3Accounts.Notes).ForeignKey(&V3Notes.AccountID).OnDelete(ant.Cascade)
}

// V3Note belongs to an account.
type V3Note struct {
	ant.Base
	AccountID int64
	Text      string
}

func (V3Note) Configure(b *ant.ModelBuilder[V3Note]) { b.Table("notes") }

// V3DB is the application after a rename and a new table.
type V3DB struct {
	ant.Context
	Accounts ant.Table[V3Account]
	Notes    ant.Table[V3Note]
}

// Probes for the models.
var (
	V3Accounts = ant.Fields[V3Account]()
	V3Notes    = ant.Fields[V3Note]()
)

// project is a folder of migrations and an empty database.
type project struct {
	t      *testing.T
	target fixture.Target
	dir    string
	config migrate.Config
}

// each gives fn an empty database and migrations folder.
func each(t *testing.T, fn func(t *testing.T, p *project)) {
	t.Helper()
	t.Setenv("DATABASE_URL", "")

	fixture.Each(t, func(t *testing.T, target fixture.Target) {
		_, err := target.DB.ExecContext(t.Context(), "DROP TABLE posts; DROP TABLE users")
		require.NoError(t, err)
		dir := t.TempDir()
		fn(t, &project{
			t:      t,
			target: target,
			dir:    dir,
			config: migrate.Config{Dir: dir, DSN: target.DSN},
		})
	})
}

// run runs a command as the CLI would, and returns what it printed.
func run[DB any](p *project, args ...string) (string, error) {
	p.t.Helper()

	var out bytes.Buffer

	config := p.config
	config.Out = &out
	err := migrate.Run[DB](p.t.Context(), p.target.Dialect, args, config)

	return out.String(), err
}

// must is run for commands that are expected to work.
func must[DB any](p *project, args ...string) string {
	p.t.Helper()
	out, err := run[DB](p, args...)
	require.NoError(p.t, err, out)

	return out
}

// write adds a file to the migrations folder.
func (p *project) write(name, text string) {
	p.t.Helper()
	path := filepath.Join(p.dir, name)
	require.NoError(p.t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(p.t, os.WriteFile(path, []byte(text), 0o644))
}

// read returns a file from the migrations folder.
func (p *project) read(name string) string {
	p.t.Helper()
	text, err := os.ReadFile(filepath.Join(p.dir, name))
	require.NoError(p.t, err)

	return string(text)
}

// exists reports whether a file is in the migrations folder.
func (p *project) exists(name string) bool {
	_, err := os.Stat(filepath.Join(p.dir, name))

	return err == nil
}

// sql runs a statement on the database.
func (p *project) sql(statement string, args ...any) {
	p.t.Helper()
	_, err := p.target.DB.ExecContext(p.t.Context(), statement, args...)
	require.NoError(p.t, err)
}

// scalar reads one value.
func scalar[V any](p *project, query string, args ...any) V {
	p.t.Helper()

	var out V
	require.NoError(p.t, p.target.DB.QueryRowContext(p.t.Context(), query, args...).Scan(&out))

	return out
}

// columns returns a table's columns.
func (p *project) columns(table string) []string {
	p.t.Helper()

	rows, err := p.target.DB.QueryContext(
		p.t.Context(),
		"SELECT column_name FROM information_schema.columns WHERE table_schema = current_schema() AND table_name = $1 ORDER BY ordinal_position",
		table,
	)
	require.NoError(p.t, err)

	defer rows.Close()

	var out []string

	for rows.Next() {
		var name string
		require.NoError(p.t, rows.Scan(&name))
		out = append(out, name)
	}

	return out
}

// open returns an ant context on the project's database.
func open[DB any](p *project) *DB {
	p.t.Helper()

	return fixture.Open[DB](p.t, p.target)
}

// dropSchemaAfter removes a database schema now and when the test ends. Tests
// outside the isolated schema share a server, so a leftover would fail the next run.
func dropSchemaAfter(p *project, name string) {
	p.t.Helper()

	drop := func() {
		_, err := p.target.DB.ExecContext(
			context.Background(),
			`DROP SCHEMA IF EXISTS "`+name+`" CASCADE`,
		)
		require.NoError(p.t, err)
	}
	drop()
	p.t.Cleanup(drop)
}

// removeFile deletes a file from the migrations folder.
func removeFile(p *project, name string) error { return os.Remove(filepath.Join(p.dir, name)) }

// listFiles returns the SQL files in a folder.
func listFiles(p *project, folder string) []string {
	p.t.Helper()
	entries, err := os.ReadDir(filepath.Join(p.dir, folder))
	require.NoError(p.t, err)

	var out []string

	for _, entry := range entries {
		if filepath.Ext(entry.Name()) == ".sql" {
			out = append(out, entry.Name())
		}
	}

	return out
}

func renderWithBanner(plan *schema.Plan, d schema.Dialect) (string, error) {
	text, err := schema.Script(d, plan)

	return "-- Reviewed by: \n" + text, err
}
