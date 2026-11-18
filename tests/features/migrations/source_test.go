package migrations

import (
	"context"
	"io/fs"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/skudra-go/ant/migrate"
)

// memory keeps migrations somewhere other than files. Anything that can list
// migrations works, such as a table, S3, or a config service.
type memory struct {
	migrations []migrate.Migration
	snapshot   []byte
}

func (m *memory) List(context.Context) ([]migrate.Migration, error) { return m.migrations, nil }
func (m *memory) Snapshot(context.Context) ([]byte, error)          { return m.snapshot, nil }
func (m *memory) Save(_ context.Context, migration migrate.Migration) error {
	m.migrations = append(m.migrations, migration)

	return nil
}
func (m *memory) Remove(context.Context, migrate.Migration) error { return nil }
func (m *memory) SaveSnapshot(_ context.Context, data []byte) error {
	m.snapshot = data

	return nil
}

// Source replaces file handling entirely, and everything else keeps working.
func TestCustomSource(t *testing.T) {
	each(t, func(t *testing.T, p *project) {
		store := new(memory)
		p.config.Source = store

		must[V1DB](p, "diff", "start")
		require.Len(t, store.migrations, 1)
		require.NotNil(t, store.snapshot)
		must[V1DB](p, "check")
		must[V1DB](p, "up")
		require.NotEmpty(t, p.columns("accounts"))
		require.False(t, p.exists("versions"), "no files were involved")

		t.Run("a repeatable migration comes from the same list", func(t *testing.T) {
			store.migrations = append(store.migrations, migrate.Migration{
				Kind: migrate.Repeatable,
				File: "stats.sql",
				SQL:  "CREATE OR REPLACE VIEW stats AS SELECT 1 AS n;",
			})

			must[V1DB](p, "up")
			require.NotEmpty(t, p.columns("stats"))
		})
	})
}

// What a Source returns is checked like anything else.
func TestCustomSourceIsValidated(t *testing.T) {
	each(t, func(t *testing.T, p *project) {
		store := new(memory)
		p.config.Source = store

		store.migrations = []migrate.Migration{
			{Kind: migrate.Versioned, Name: "no_version", SQL: "SELECT 1"},
		}

		_, err := run[V1DB](p, "up")
		require.ErrorIs(t, err, migrate.ErrBadMigration)

		store.migrations = []migrate.Migration{
			{Kind: migrate.Repeatable, Name: "no_file", SQL: "SELECT 1"},
		}

		_, err = run[V1DB](p, "up")
		require.ErrorIs(t, err, migrate.ErrBadMigration)

		store.migrations = []migrate.Migration{{
			Kind:    "sideways",
			Version: "1",
			Name:    "odd",
			SQL:     "SELECT 1",
		}}

		_, err = run[V1DB](p, "up")
		require.ErrorIs(t, err, migrate.ErrBadMigration)
	})
}

// A read-only source can apply, and says plainly why it cannot generate.
func TestEmbeddedMigrations(t *testing.T) {
	each(t, func(t *testing.T, p *project) {
		files := fstest.MapFS{
			"versions/0001_one.up.sql": {Data: []byte("CREATE TABLE one (id int);")},
			"code/view.sql": {
				Data: []byte("CREATE OR REPLACE VIEW v AS SELECT 1 AS n;"),
			},
		}
		p.config = migrate.Config{FS: files, DSN: p.target.DSN}
		db := open[V1DB](p)
		require.NoError(t, migrate.Up(t.Context(), db, p.config))
		require.NotEmpty(t, p.columns("one"))
		require.NotEmpty(t, p.columns("v"))

		_, err := run[V1DB](p, "diff", "nope")
		require.ErrorIs(t, err, migrate.ErrReadOnly)
	})
}

// Embedded lets one program do both jobs: it generates against the folder in a
// repository, and applies from its binary once deployed, where no folder exists.
func TestEmbeddedFallback(t *testing.T) {
	each(t, func(t *testing.T, p *project) {
		files := fstest.MapFS{
			"versions/0001_baked_in.up.sql": {Data: []byte("CREATE TABLE baked_in (id int);")},
		}
		p.config.Embedded = files

		t.Run("with the folder on disk, the folder wins", func(t *testing.T) {
			must[V1DB](p, "diff", "start")
			require.True(t, p.exists("versions/0001_start.up.sql"))
		})

		t.Run("with no folder, the embedded copy applies", func(t *testing.T) {
			p.config.Dir = p.dir + "/not-here"
			must[V1DB](p, "up")
			require.NotEmpty(t, p.columns("baked_in"))
		})
	})
}

// An embed.FS rooted above the folder takes Dir to say where to look.
func TestEmbeddedSubfolder(t *testing.T) {
	each(t, func(t *testing.T, p *project) {
		files := fstest.MapFS{
			"db/versions/0001_one.up.sql": {Data: []byte("CREATE TABLE one (id int);")},
		}

		db := open[V1DB](p)
		require.NoError(t, migrate.Up(t.Context(), db, migrate.Config{FS: files, Dir: "db"}))
		require.NotEmpty(t, p.columns("one"))
	})
}

// Name and Parse replace the file naming.
func TestCustomNames(t *testing.T) {
	each(t, func(t *testing.T, p *project) {
		t.Run("Timestamped is built in", func(t *testing.T) {
			p.config.Name = migrate.Timestamped
			must[V1DB](p, "diff", "start")

			prefix := time.Now().UTC().Format("20060102")
			files := listFiles(p, "versions")
			require.True(t, strings.HasPrefix(files[0], prefix), files[0])
			require.Contains(t, must[V1DB](p, "up"), "_start")
		})
	})

	each(t, func(t *testing.T, p *project) {
		t.Run("any scheme works, given Parse to read it back", func(t *testing.T) {
			p.config.Name = func(n migrate.Next) string {
				return "release-" + n.Version + "." + n.Name + ".sql"
			}

			p.config.Parse = func(file string) (string, string, bool) {
				rest, ok := strings.CutPrefix(file, "release-")
				version, name, found := strings.Cut(strings.TrimSuffix(rest, ".sql"), ".")

				return version, name, ok && found
			}

			must[V1DB](p, "diff", "start")
			require.True(t, p.exists("versions/release-0001.start.sql"))
			require.Contains(t, must[V1DB](p, "up"), "Applied 0001_start")
		})
	})
}

// A versions folder that is not there is an error, not an empty one: a binary
// run from another folder, or an image built without its migrations, would
// otherwise report nothing to apply and leave the database empty. The commands
// that write the first migration create the folder, and check and drift read
// it as a project with no migrations yet, as when adopting a database.
func TestMissingFolder(t *testing.T) {
	each(t, func(t *testing.T, p *project) {
		for _, command := range [][]string{
			{"up"},
			{"status"},
			{"baseline"},
			{"repair"},
			{"script"},
			{"snapshot"},
			{"squash", "all"},
		} {
			_, err := run[V1DB](p, command...)
			require.ErrorIs(t, err, fs.ErrNotExist, command)
			require.ErrorContains(t, err, "no migrations folder at "+p.dir+"/versions", command)
			require.ErrorContains(t, err, "working directory", command)
		}

		require.Empty(t, p.columns("ant_migrations"), "nothing was recorded")

		_, err := run[V1DB](p, "check")
		require.ErrorIs(t, err, migrate.ErrPendingChanges)

		_, err = run[V1DB](p, "drift")
		require.ErrorIs(t, err, migrate.ErrDrift, "the models have a table the database lacks")

		must[V1DB](p, "diff", "start")
		require.True(t, p.exists("versions/0001_start.up.sql"))
		require.Contains(t, must[V1DB](p, "up"), "Applied 0001_start")
	})
}

// With go:embed, the file system's root holds the migrations folder, so Dir
// names it. Without it, Up says where it looked.
func TestMissingFolderInFS(t *testing.T) {
	each(t, func(t *testing.T, p *project) {
		files := fstest.MapFS{
			"migrations/versions/0001_init.up.sql": {
				Data: []byte("CREATE TABLE embedded (id bigint PRIMARY KEY);"),
			},
		}

		db := open[V1DB](p)
		err := migrate.Up(t.Context(), db, migrate.Config{FS: files})
		require.ErrorIs(t, err, fs.ErrNotExist, "Up says so too")

		_, err = migrate.Status(t.Context(), db, migrate.Config{FS: files})
		require.ErrorIs(t, err, fs.ErrNotExist)

		p.config = migrate.Config{FS: files, DSN: p.target.DSN}

		_, err = run[V1DB](p, "up")
		require.ErrorIs(t, err, fs.ErrNotExist)
		require.ErrorContains(t, err, "no migrations folder at versions in the file system")
		require.ErrorContains(t, err, `Config.Dir`)
		require.Empty(t, p.columns("embedded"))

		p.config.Dir = "migrations"

		require.Contains(t, must[V1DB](p, "up"), "Applied 0001_init")
		require.NotEmpty(t, p.columns("embedded"))
	})
}

// VersionsDir and CodeDir fit an existing layout.
func TestCustomFolders(t *testing.T) {
	each(t, func(t *testing.T, p *project) {
		p.config.VersionsDir, p.config.CodeDir = "schema", "objects"
		must[V1DB](p, "diff", "start")
		require.True(t, p.exists("schema/0001_start.up.sql"))
		p.write("objects/view.sql", "CREATE OR REPLACE VIEW v AS SELECT 1 AS n;")
		must[V1DB](p, "up")
		require.NotEmpty(t, p.columns("v"))
	})
}

// PerDialect keeps one folder per database, for projects that run on several.
func TestPerDialectFolders(t *testing.T) {
	each(t, func(t *testing.T, p *project) {
		p.config.PerDialect = true
		must[V1DB](p, "diff", "start")
		require.True(t, p.exists("postgres/versions/0001_start.up.sql"))
		require.Contains(t, must[V1DB](p, "up"), "Applied 0001_start")
	})
}

// Render replaces how a plan becomes SQL. For small tweaks, it can wrap the default.
func TestCustomRender(t *testing.T) {
	each(t, func(t *testing.T, p *project) {
		p.write("versions/.keep", "")
		p.config.Render = renderWithBanner
		must[V1DB](p, "diff", "start")
		require.True(t, strings.HasPrefix(p.read("versions/0001_start.up.sql"), "-- Reviewed by: "))
	})
}
