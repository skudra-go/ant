package migrations

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/skudra-go/ant"
	"github.com/skudra-go/ant/migrate"
)

// ScriptDB has a Go migration between two SQL ones.
type ScriptDB struct {
	ant.Context
	Accounts ant.Table[V1Account]
}

func init() {
	migrate.Register[ScriptDB]("0002_trim", func(ctx context.Context, tx *ScriptDB) error {
		_, err := ant.Raw[any](tx, "CREATE TABLE trimmed (n int)").Exec(ctx)

		return err
	})
}

// script exports everything as one file, for a database ant cannot reach. It can
// run any number of times, and shares its record with up.
func TestIdempotentScript(t *testing.T) {
	each(t, func(t *testing.T, p *project) {
		must[V1DB](p, "diff", "create_accounts")

		p.write(
			"versions/0002_quotes.up.sql",
			"CREATE TABLE quoted (t text DEFAULT 'it''s; $$ fine');\nCREATE FUNCTION two() RETURNS int AS $$ SELECT 2; $$ LANGUAGE sql;\n",
		)

		p.write("code/view.sql", "CREATE OR REPLACE VIEW v AS SELECT 1 AS n;")
		text := must[V1DB](p, "script")

		t.Run("it can run more than once", func(t *testing.T) {
			p.sql(text)
			p.sql(text)
			require.NotEmpty(t, p.columns("accounts"))
			require.NotEmpty(t, p.columns("v"))
			require.Equal(t, 2, scalar[int](p, "SELECT two()"))
			require.Equal(t, 3, scalar[int](p, "SELECT count(*) FROM ant_migrations"))
		})

		t.Run("up agrees that everything is done", func(t *testing.T) {
			require.Contains(t, must[V1DB](p, "up"), "Nothing to apply")
		})

		t.Run("a changed repeatable file runs again", func(t *testing.T) {
			p.write("code/view.sql", "CREATE OR REPLACE VIEW v AS SELECT 1 AS n, 2 AS m;")
			p.sql(must[V1DB](p, "script"))
			require.Contains(t, p.columns("v"), "m")
			require.Contains(t, must[V1DB](p, "up"), "Nothing to apply")
		})
	})
}

// A migration that needs to run outside a transaction cannot sit in a DO block.
func TestScriptRefusesNoTransaction(t *testing.T) {
	each(t, func(t *testing.T, p *project) {
		p.write("versions/0001_index.up.sql", "-- ant:no-transaction\nSELECT 1;")
		_, err := run[V1DB](p, "script")
		require.ErrorIs(t, err, migrate.ErrBadMigration)
		require.ErrorContains(t, err, "--plain")
	})
}

// script stops before a Go migration, which only up can run: a file that went
// on past it would apply later migrations first, and up would then refuse the
// Go one as older than an applied one (#183).
func TestScriptStopsBeforeAGoMigration(t *testing.T) {
	each(t, func(t *testing.T, p *project) {
		p.write(
			"versions/0001_init.up.sql",
			"CREATE TABLE accounts (id bigint PRIMARY KEY, name text, email text UNIQUE);",
		)

		p.write("versions/0003_note.up.sql", "ALTER TABLE accounts ADD COLUMN note text;")
		p.write("code/view.sql", "CREATE OR REPLACE VIEW v AS SELECT 1 AS n;")

		text := must[ScriptDB](p, "script")
		require.Contains(t, text, "CREATE TABLE accounts")
		require.Contains(t, text, "0002_trim")
		require.NotContains(t, text, "0003_note", "nothing after the Go migration")
		require.NotContains(t, text, "VIEW", "repeatable files run after every versioned one")

		// From the Go migration itself, there is nothing it can write yet.
		stopped := must[ScriptDB](p, "script", "--from", "0002")
		require.Contains(t, stopped, "Stopped before 0002_trim")
		require.Contains(t, stopped, "script --from 0003")
		require.NotContains(t, stopped, "0001_init")
		require.NotContains(t, stopped, "0003_note")

		p.sql(text)
		require.Contains(t, must[ScriptDB](p, "up", "--to", "0002"), "Applied 0002_trim")
		require.NotEmpty(t, p.columns("trimmed"))

		// The rest, from the version after the Go migration.
		rest := must[ScriptDB](p, "script", "--from", "0003")
		require.NotContains(t, rest, "0001", "migrations before --from are left out")
		require.Contains(t, rest, "0003_note")
		require.Contains(t, rest, "VIEW")

		p.sql(rest)
		require.Contains(t, p.columns("accounts"), "note")
		require.Contains(t, must[ScriptDB](p, "up"), "Nothing to apply")
	})
}

// After the last versioned migration, a Go one leaves only the repeatable
// files, which up --to skips, so script says to run up (#183).
func TestScriptStopsBeforeTheLastGoMigration(t *testing.T) {
	each(t, func(t *testing.T, p *project) {
		p.write(
			"versions/0001_init.up.sql",
			"CREATE TABLE accounts (id bigint PRIMARY KEY, name text, email text UNIQUE);",
		)

		p.write("code/view.sql", "CREATE OR REPLACE VIEW v AS SELECT 1 AS n;")

		text := must[ScriptDB](p, "script")
		require.Contains(t, text, "Stopped before 0002_trim")
		require.Contains(t, text, "run it with up.")
		require.NotContains(t, text, "--to", "up --to would leave the repeatable files")
		require.NotContains(t, text, "VIEW")

		p.sql(text)
		require.Contains(t, must[ScriptDB](p, "up"), "Applied 0002_trim")
		require.Equal(t, []string{"n"}, p.columns("v"))
		require.Contains(t, must[ScriptDB](p, "up"), "Nothing to apply")
	})
}

// A file that cannot run in a transaction blocks a guarded script only while it
// is among the migrations to write, so --from gets past one applied long ago
// (#183).
func TestScriptFromAVersion(t *testing.T) {
	each(t, func(t *testing.T, p *project) {
		must[V1DB](p, "diff", "create_accounts")

		p.write(
			"versions/0002_email_idx.up.sql",
			"-- ant:no-transaction\nCREATE INDEX CONCURRENTLY accounts_name_idx ON accounts (name);",
		)

		must[V1DB](p, "up")
		p.write("versions/0003_note.up.sql", "ALTER TABLE accounts ADD COLUMN note text;")

		_, err := run[V1DB](p, "script")
		require.ErrorIs(t, err, migrate.ErrBadMigration)
		require.ErrorContains(t, err, "--from")

		text := must[V1DB](p, "script", "--from", "0003")
		require.NotContains(t, text, "0002_email_idx")
		p.sql(text)
		p.sql(text)
		require.Contains(t, p.columns("accounts"), "note")
		require.Contains(t, must[V1DB](p, "up"), "Nothing to apply")

		t.Run("plain", func(t *testing.T) {
			plain := must[V1DB](p, "script", "--plain", "--from", "3")
			require.NotContains(t, plain, "0002_email_idx")
			require.NotContains(t, plain, `CREATE TABLE "accounts"`)
			require.Contains(t, plain, "0003_note")
		})

		t.Run("a version no migration has", func(t *testing.T) {
			_, err := run[V1DB](p, "script", "--from", "0009")
			require.ErrorIs(t, err, migrate.ErrBadMigration)
			require.ErrorContains(t, err, "no migration has version 0009")
		})
	})
}

// --plain is the way out: plain SQL, in order, for any other tool or a person.
func TestPlainScript(t *testing.T) {
	each(t, func(t *testing.T, p *project) {
		must[V1DB](p, "diff", "start")
		text := must[V1DB](p, "script", "--plain")
		require.Contains(t, text, `CREATE TABLE "accounts"`)
		require.NotContains(t, text, "ant_migrations")
		p.sql(text)
		require.NotEmpty(t, p.columns("accounts"))
	})
}

// schema-dump prints the whole schema, with no database.
func TestSchemaDump(t *testing.T) {
	each(t, func(t *testing.T, p *project) {
		p.config.DSN = ""
		require.Contains(t, must[V3DB](p, "schema-dump"), `CREATE TABLE "notes"`)

		t.Run("--snapshot uses the snapshot", func(t *testing.T) {
			_, err := run[V3DB](p, "schema-dump", "--snapshot")
			require.ErrorContains(t, err, "no snapshot yet")
			must[V1DB](p, "diff", "start")
			text := must[V3DB](p, "schema-dump", "--snapshot")
			require.NotContains(t, text, "notes")
		})
	})
}

// inspect prints what the models describe, with no database.
func TestInspect(t *testing.T) {
	each(t, func(t *testing.T, p *project) {
		p.config.DSN = ""
		out := must[V3DB](p, "inspect")
		require.Contains(t, out, "accounts")
		require.Contains(t, out, "display_name text NOT NULL")
		require.Contains(t, out, "foreign key (account_id) to accounts (id)")

		t.Run("one table", func(t *testing.T) {
			require.NotContains(t, must[V3DB](p, "inspect", "notes"), "display_name")
			_, err := run[V3DB](p, "inspect", "ghosts")
			require.ErrorContains(t, err, "no table ghosts")
		})
	})
}

// db-show summarizes a live database.
func TestDBShow(t *testing.T) {
	each(t, func(t *testing.T, p *project) {
		must[V1DB](p, "diff", "start")
		must[V1DB](p, "up")
		out := must[V1DB](p, "db-show")
		require.Contains(t, out, "on postgres")
		require.Contains(t, out, "accounts")
		require.NotContains(t, out, "MATERIALIZED VIEW", "there is none")

		t.Run("materialized views follow the tables", func(t *testing.T) {
			p.sql("CREATE MATERIALIZED VIEW account_names AS SELECT name FROM accounts")
			p.sql("CREATE VIEW plain_names AS SELECT name FROM accounts")
			out := must[V1DB](p, "db-show")
			require.Regexp(t, `(?s)TABLE.*accounts.*MATERIALIZED VIEW\s+ROWS.*account_names`, out)
			require.NotContains(t, out, "plain_names", "a view holds no rows")
		})
	})
}
