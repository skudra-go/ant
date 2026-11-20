package migrations

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/skudra-go/ant/migrate"
)

// reset deletes everything and applies all migrations, which suits a developer's
// own database. It asks for the database's name, so it cannot happen by accident.
func TestReset(t *testing.T) {
	each(t, func(t *testing.T, p *project) {
		must[V1DB](p, "diff", "start")
		must[V1DB](p, "up")

		p.sql(
			"INSERT INTO accounts (created_at, updated_at, name, email) VALUES (now(), now(), 'ada', 'ada@example.com')",
		)

		p.sql("CREATE TABLE stray (id int)")
		database := scalar[string](p, "SELECT current_database()")

		t.Run("with no terminal and no confirmation, it refuses", func(t *testing.T) {
			_, err := run[V1DB](p, "reset")
			require.ErrorIs(t, err, migrate.ErrUnconfirmed)
			require.ErrorContains(t, err, "--confirm "+database)
			require.Equal(t, 1, scalar[int](p, "SELECT count(*) FROM accounts"))
		})

		t.Run("the wrong name is refused", func(t *testing.T) {
			_, err := run[V1DB](p, "reset", "--confirm", "production")
			require.ErrorIs(t, err, migrate.ErrUnconfirmed)
			require.Equal(t, 1, scalar[int](p, "SELECT count(*) FROM accounts"))
		})

		t.Run("the right name wipes it and starts again", func(t *testing.T) {
			must[V1DB](p, "reset", "--confirm", database)
			require.Equal(t, 0, scalar[int](p, "SELECT count(*) FROM accounts"))
			require.Empty(t, p.columns("stray"))
			require.Contains(t, must[V1DB](p, "drift"), "No drift")
		})
	})
}

// A helpful failure beats a stack trace for the common mistakes.
func TestCommandErrors(t *testing.T) {
	each(t, func(t *testing.T, p *project) {
		t.Run("no name for diff", func(t *testing.T) {
			_, err := run[V1DB](p, "diff")
			require.ErrorContains(t, err, "diff needs a name")
		})

		t.Run("a name that cleans up to nothing", func(t *testing.T) {
			_, err := run[V1DB](p, "diff", "...")
			require.ErrorContains(t, err, "give the migration a name")
		})

		t.Run("an unknown command", func(t *testing.T) {
			_, err := run[V1DB](p, "uppp")
			require.ErrorContains(t, err, `unknown command "uppp"`)
		})

		t.Run("too many names", func(t *testing.T) {
			_, err := run[V1DB](p, "diff", "add", "bio")
			require.ErrorContains(t, err, "diff takes one name, and got: add bio")
		})

		t.Run("a misspelled directive is not ignored", func(t *testing.T) {
			p.write("versions/0001_typo.up.sql", "-- ant:no-transactions\nSELECT 1;")
			_, err := run[V1DB](p, "up")
			require.ErrorIs(t, err, migrate.ErrBadMigration)
			require.ErrorContains(t, err, "unknown directive ant:no-transactions")
			require.NoError(t, removeFile(p, "versions/0001_typo.up.sql"))
		})

		t.Run("no database", func(t *testing.T) {
			p.config.DSN = ""
			_, err := run[V1DB](p, "up")
			require.ErrorContains(t, err, "pass --dsn, or set DATABASE_URL")
		})

		t.Run("an unknown flag", func(t *testing.T) {
			_, err := run[V1DB](p, "up", "--nope")
			require.ErrorContains(t, err, "not defined")
		})
	})
}

// A file runs as it says, or not at all. A directive after the first statement
// is not read, so a file that put a SET first ran in a transaction (#182). And
// ant runs each file in a transaction, which a COMMIT in the file ends, so the
// statements before it stayed when a later one failed (#181).
func TestFileBodyIsChecked(t *testing.T) {
	each(t, func(t *testing.T, p *project) {
		refused := func(t *testing.T, file, body, want string) {
			t.Helper()

			p.write("versions/"+file, body)
			t.Cleanup(func() { require.NoError(t, removeFile(p, "versions/"+file)) })

			_, err := run[V1DB](p, "up")
			require.ErrorIs(t, err, migrate.ErrBadMigration)
			require.ErrorContains(t, err, want)
			require.Empty(t, p.columns("audit_a"), "nothing of it ran")
		}

		t.Run("a directive after the first statement", func(t *testing.T) {
			refused(
				t,
				"0001_late.up.sql",
				"SET lock_timeout = '5s';\n-- ant:no-transaction\nCREATE TABLE audit_a (id int);\n",
				"-- ant:no-transaction at line 2 comes after the first statement",
			)
		})

		for _, c := range []struct{ name, body, word string }{
			{"BEGIN and COMMIT", "BEGIN;\nCREATE TABLE audit_a (id int);\nCOMMIT;\n", "BEGIN"},
			{"START TRANSACTION", "START TRANSACTION;\nCREATE TABLE audit_a (id int);\n", "START"},
			{"a COMMIT halfway", "CREATE TABLE audit_a (id int);\n/* done */ commit;\nSELECT 1;\n", "COMMIT"},
			{"END", "CREATE TABLE audit_a (id int);\nEND;\n", "END"},
			{"ROLLBACK", "CREATE TABLE audit_a (id int);\n-- undo\nROLLBACK;\n", "ROLLBACK"},
		} {
			t.Run(c.name, func(t *testing.T) {
				refused(t, "0001_wrapped.up.sql", c.body, "has a "+c.word+" of its own")
			})
		}

		t.Run("savepoints stay inside the transaction", func(t *testing.T) {
			p.write("versions/0001_savepoint.up.sql", "SAVEPOINT s;\nCREATE TABLE audit_a (id int);\n"+
				"ROLLBACK TO SAVEPOINT s;\nRELEASE s;\nCREATE TABLE audit_b (id int);\n"+
				"-- BEGIN; in a comment, and 'COMMIT' in a string, are no statements\nSELECT 'COMMIT';\n")
			must[V1DB](p, "up")
			require.Empty(t, p.columns("audit_a"))
			require.NotEmpty(t, p.columns("audit_b"))
		})

		t.Run("a file without a transaction may hold its own", func(t *testing.T) {
			p.write("versions/0002_own.up.sql", "-- ant:no-transaction\nBEGIN;\nCREATE TABLE audit_c (id int);\nCOMMIT;\n")
			must[V1DB](p, "up")
			require.NotEmpty(t, p.columns("audit_c"))
		})
	})
}
