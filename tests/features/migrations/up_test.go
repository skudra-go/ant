package migrations

import (
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/skudra-go/ant/migrate"
	"github.com/skudra-go/ant/tests/features/fixture"
)

// up applies pending migrations in order, and records each one.
func TestUp(t *testing.T) {
	each(t, func(t *testing.T, p *project) {
		must[V1DB](p, "diff", "create_accounts")
		must[V2DB](p, "diff", "add_bio_and_age")
		must[V3DB](p, "diff", "rename_and_notes", "--rename", "accounts.name=display_name")

		t.Run("applies everything, oldest first", func(t *testing.T) {
			out := must[V3DB](p, "up")
			require.Less(
				t,
				strings.Index(out, "0001_create_accounts"),
				strings.Index(out, "0002_add_bio_and_age"),
			)

			require.Less(
				t,
				strings.Index(out, "0002_add_bio_and_age"),
				strings.Index(out, "0003_rename_and_notes"),
			)
		})

		t.Run("the models work on the result", func(t *testing.T) {
			db := open[V3DB](p)
			ctx := t.Context()

			account := &V3Account{
				DisplayName: "Ada",
				Email:       "ada@example.com",
				Age:         36,
				Notes:       []V3Note{{Text: "hi"}},
			}
			require.NoError(t, db.Accounts.Insert(ctx, account))
			loaded, err := db.Accounts.Include(&V3Accounts.Notes).First(ctx)
			require.NoError(t, err)
			require.Equal(t, "Ada", loaded.DisplayName)
			require.Len(t, loaded.Notes, 1)
		})

		t.Run("running it again does nothing", func(t *testing.T) {
			require.Contains(t, must[V3DB](p, "up"), "Nothing to apply")
		})

		t.Run("history records checksums, times, and success", func(t *testing.T) {
			require.Equal(
				t,
				3,
				scalar[int](
					p,
					"SELECT count(*) FROM ant_migrations WHERE kind = 'versioned' AND success",
				),
			)

			checksum := scalar[string](
				p,
				"SELECT checksum FROM ant_migrations WHERE version = '0001'",
			)
			require.Len(t, checksum, 64)

			applied := scalar[time.Time](
				p,
				"SELECT applied_at FROM ant_migrations WHERE version = '0001'",
			)
			require.WithinDuration(t, time.Now(), applied, time.Minute)
		})

		t.Run("the database now matches, so drift finds nothing", func(t *testing.T) {
			require.Contains(t, must[V3DB](p, "drift"), "No drift")
		})
	})
}

// Generated SQL is written for a table with data in it, because that is where
// migrations are hard. A NOT NULL column on a table with rows does not fail.
func TestUpWithExistingRows(t *testing.T) {
	each(t, func(t *testing.T, p *project) {
		must[V1DB](p, "diff", "create_accounts")
		must[V1DB](p, "up")

		p.sql(
			"INSERT INTO accounts (created_at, updated_at, name, email) VALUES (now(), now(), 'Ada', 'ada@example.com')",
		)

		must[V2DB](p, "diff", "add_bio_and_age")
		must[V2DB](p, "up")
		require.Equal(t, 0, scalar[int](p, "SELECT age FROM accounts"))

		t.Run("and the temporary default does not stay", func(t *testing.T) {
			require.Contains(t, must[V2DB](p, "drift"), "No drift")
		})

		t.Run("a rename keeps the row", func(t *testing.T) {
			must[V3DB](p, "diff", "rename", "--rename", "accounts.name=display_name")
			must[V3DB](p, "up")
			require.Equal(t, "Ada", scalar[string](p, "SELECT display_name FROM accounts"))
		})
	})
}

// A failed migration rolls back completely, says where it failed, and can run
// again once fixed.
func TestFailedMigration(t *testing.T) {
	each(t, func(t *testing.T, p *project) {
		p.write("versions/0001_good.up.sql", "CREATE TABLE first (id int);")

		p.write(
			"versions/0002_bad.up.sql",
			"CREATE TABLE second (id int);\n\nINSERT INTO second (nope) VALUES (1);\n",
		)

		p.write("versions/0003_later.up.sql", "CREATE TABLE third (id int);")

		_, err := run[V1DB](p, "up")

		t.Run("the error names the migration and the line", func(t *testing.T) {
			require.ErrorContains(
				t,
				err,
				"migration 0002_bad failed and was rolled back, at line 3",
			)

			require.ErrorContains(t, err, "3 | INSERT INTO second (nope) VALUES (1);")
		})

		t.Run("migrations before it stay applied", func(t *testing.T) {
			require.NotEmpty(t, p.columns("first"))
		})

		t.Run("nothing of it remains", func(t *testing.T) {
			require.Empty(t, p.columns("second"))
		})

		t.Run("nothing after it ran", func(t *testing.T) {
			require.Empty(t, p.columns("third"))
		})

		t.Run("a rolled-back failure leaves no mark, so fixing it is enough", func(t *testing.T) {
			p.write("versions/0002_bad.up.sql", "CREATE TABLE second (id int);\n")
			must[V1DB](p, "up")
			require.NotEmpty(t, p.columns("third"))
		})
	})
}

// -- ant:no-transaction is for statements PostgreSQL refuses in a transaction.
func TestNoTransaction(t *testing.T) {
	each(t, func(t *testing.T, p *project) {
		p.write("versions/0001_table.up.sql", "CREATE TABLE items (id int, name text);")

		p.write("versions/0002_index.up.sql", `-- ant:no-transaction
-- Comments and semicolons; in strings stay out of the way.
CREATE INDEX CONCURRENTLY items_name_idx ON items (name);
INSERT INTO items (name) VALUES ('a;b');
`)

		t.Run("runs each statement on its own", func(t *testing.T) {
			must[V1DB](p, "up")
			require.Equal(t, "a;b", scalar[string](p, "SELECT name FROM items"))

			require.Equal(
				t,
				1,
				scalar[int](
					p,
					"SELECT count(*) FROM pg_indexes WHERE schemaname = current_schema() AND indexname = 'items_name_idx'",
				),
			)
		})

		t.Run("without the directive PostgreSQL refuses", func(t *testing.T) {
			p.write(
				"versions/0003_refused.up.sql",
				"CREATE INDEX CONCURRENTLY items_id_idx ON items (id);",
			)

			_, err := run[V1DB](p, "up")
			require.ErrorContains(t, err, "cannot run inside a transaction block")
			require.NoError(t, removeFile(p, "versions/0003_refused.up.sql"))
		})
	})
}

// A failure outside a transaction can leave work behind, so it is recorded, and up
// refuses to go on until someone has looked. repair clears it.
func TestFailureOutsideATransaction(t *testing.T) {
	each(t, func(t *testing.T, p *project) {
		p.write("versions/0001_half.up.sql", `-- ant:no-transaction
CREATE TABLE half (id int);
SELECT 1 FROM nowhere;
CREATE TABLE never (id int);
`)

		_, err := run[V1DB](p, "up")
		require.ErrorContains(t, err, "failed partway")
		require.ErrorContains(t, err, "SELECT 1 FROM nowhere")

		t.Run("what ran stays, and what followed did not run", func(t *testing.T) {
			require.NotEmpty(t, p.columns("half"))
			require.Empty(t, p.columns("never"))
		})

		t.Run("status shows it", func(t *testing.T) {
			require.Contains(t, must[V1DB](p, "status"), "failed")
		})

		t.Run("up refuses until someone has looked", func(t *testing.T) {
			_, err := run[V1DB](p, "up")
			require.ErrorIs(t, err, migrate.ErrFailed)
			require.ErrorContains(t, err, "run repair")
		})

		t.Run("repair clears it, and a fixed file runs again", func(t *testing.T) {
			p.sql("DROP TABLE half")

			p.write(
				"versions/0001_half.up.sql",
				"-- ant:no-transaction\nCREATE TABLE half (id int);\n",
			)

			require.Contains(t, must[V1DB](p, "repair"), "Cleared the failed run")
			must[V1DB](p, "up")
			require.Equal(t, 1, scalar[int](p, "SELECT count(*) FROM ant_migrations WHERE success"))
		})
	})
}

// baseline after a hand fix: the migration really did finish, so record that.
func TestFinishingAFailedMigrationByHand(t *testing.T) {
	each(t, func(t *testing.T, p *project) {
		p.write("versions/0001_half.up.sql", "-- ant:no-transaction\nSELECT 1 FROM nowhere;\n")
		_, err := run[V1DB](p, "up")
		require.Error(t, err)
		must[V1DB](p, "repair")
		must[V1DB](p, "baseline", "0001")
		require.Contains(t, must[V1DB](p, "up"), "Nothing to apply")
	})
}

// Several instances starting at once is normal in a deployment. One does the work,
// and the rest wait, then find nothing to do.
func TestConcurrentUp(t *testing.T) {
	each(t, func(t *testing.T, p *project) {
		p.write("versions/0001_table.up.sql", "CREATE TABLE counted (n int); SELECT pg_sleep(0.2);")
		p.write("versions/0002_row.up.sql", "INSERT INTO counted VALUES (1);")

		var wg sync.WaitGroup

		errs := make([]error, 8)
		for i := range errs {
			wg.Go(func() { _, errs[i] = run[V1DB](p, "up") })
		}

		wg.Wait()

		for _, err := range errs {
			require.NoError(t, err)
		}

		require.Equal(t, 1, scalar[int](p, "SELECT count(*) FROM counted"))
	})
}

// A migrator stopped while waiting gives up cleanly, and holds nothing.
func TestLockIsReleased(t *testing.T) {
	fixture.Serial(t) // it counts the advisory locks of every session

	each(t, func(t *testing.T, p *project) {
		p.write("versions/0001_table.up.sql", "SELECT 1;")
		must[V1DB](p, "up")

		require.Equal(
			t,
			0,
			scalar[int](p, "SELECT count(*) FROM pg_locks WHERE locktype = 'advisory'"),
		)
	})
}

// up --to stops at a version, and --dry-run changes nothing.
func TestUpOptions(t *testing.T) {
	each(t, func(t *testing.T, p *project) {
		p.write("versions/0001_one.up.sql", "CREATE TABLE one (id int);")
		p.write("versions/0002_two.up.sql", "CREATE TABLE two (id int);")

		t.Run("--dry-run prints the SQL and runs none of it", func(t *testing.T) {
			require.Contains(t, must[V1DB](p, "up", "--dry-run"), "CREATE TABLE one")
			require.Empty(t, p.columns("one"))
		})

		t.Run("--to stops at a version", func(t *testing.T) {
			must[V1DB](p, "up", "--to", "0001")
			require.NotEmpty(t, p.columns("one"))
			require.Empty(t, p.columns("two"))
		})
	})
}

// migrate.Up does the same from a running program.
func TestUpAtRuntime(t *testing.T) {
	each(t, func(t *testing.T, p *project) {
		must[V1DB](p, "diff", "start")
		db := open[V1DB](p)

		var applied []string

		config := p.config
		config.OnApply = func(a migrate.Applied) { applied = append(applied, a.Migration.ID()) }
		require.NoError(t, migrate.Up(t.Context(), db, config))
		require.Equal(t, []string{"0001_start"}, applied)

		require.NoError(
			t,
			db.Accounts.Insert(t.Context(), &V1Account{Name: "Ada", Email: "ada@example.com"}),
		)
	})
}

// A migration can use the session's search_path and settings as it likes, as a
// pg_dump file does, without affecting what runs after it.
func TestMigrationCannotBreakTheSession(t *testing.T) {
	each(t, func(t *testing.T, p *project) {
		p.write("versions/0001_dump.up.sql", `SET lock_timeout = 12345;
SELECT pg_catalog.set_config('search_path', '', false);
`)

		p.write("versions/0002_next.up.sql", "CREATE TABLE after_dump (id int);")
		must[V1DB](p, "up")
		require.NotEmpty(t, p.columns("after_dump"))
		require.Equal(t, 2, scalar[int](p, "SELECT count(*) FROM ant_migrations"))
	})
}

// Files from other tools often manage their own transaction, which still works.
func TestMigrationWithItsOwnTransaction(t *testing.T) {
	each(t, func(t *testing.T, p *project) {
		p.write("versions/0001_own.up.sql", "BEGIN;\nCREATE TABLE own (id int);\nCOMMIT;\n")
		p.write("versions/0002_next.up.sql", "CREATE TABLE after_own (id int);")
		must[V1DB](p, "up")
		require.NotEmpty(t, p.columns("after_own"))
		require.Equal(t, 2, scalar[int](p, "SELECT count(*) FROM ant_migrations"))
	})
}

// status lists everything and changes nothing.
func TestStatus(t *testing.T) {
	each(t, func(t *testing.T, p *project) {
		p.write("versions/0001_one.up.sql", "CREATE TABLE one (id int);")
		p.write("versions/0002_two.up.sql", "CREATE TABLE two (id int);")
		p.write("code/view.sql", "CREATE OR REPLACE VIEW v AS SELECT 1 AS n;")

		t.Run("before the first run, nothing was created", func(t *testing.T) {
			out := must[V1DB](p, "status")
			require.Contains(t, out, "3 waiting")
			require.Empty(t, p.columns("ant_migrations"))
		})

		must[V1DB](p, "up", "--to", "0001")

		t.Run("shows each state", func(t *testing.T) {
			db := open[V1DB](p)
			entries, err := migrate.Status(t.Context(), db, p.config)
			require.NoError(t, err)
			require.Len(t, entries, 3)
			require.Equal(t, migrate.Done, entries[0].State)
			require.False(t, entries[0].AppliedAt.IsZero())
			require.Equal(t, migrate.Pending, entries[1].State)
			require.Equal(t, migrate.Repeatable, entries[2].Kind)
			require.Equal(t, migrate.Pending, entries[2].State)
		})
	})
}

// Applied migrations are history, so an edit to one is an error, and repair is
// the way to say it was deliberate.
func TestChecksums(t *testing.T) {
	each(t, func(t *testing.T, p *project) {
		p.write("versions/0001_one.up.sql", "CREATE TABLE one (id int);\n")
		must[V1DB](p, "up")

		t.Run("line endings do not count as an edit", func(t *testing.T) {
			p.write("versions/0001_one.up.sql", "CREATE TABLE one (id int);\r\n")
			require.Contains(t, must[V1DB](p, "up"), "Nothing to apply")
		})

		p.write("versions/0001_one.up.sql", "CREATE TABLE one (id bigint);\n")

		t.Run("an edit stops up", func(t *testing.T) {
			_, err := run[V1DB](p, "up")
			require.ErrorIs(t, err, migrate.ErrChecksum)
			require.ErrorContains(t, err, "0001_one")
			require.Contains(t, must[V1DB](p, "status"), "changed")
		})

		t.Run("repair accepts it", func(t *testing.T) {
			require.Contains(
				t,
				must[V1DB](p, "repair"),
				"Accepted the current contents of 0001_one",
			)

			require.Contains(t, must[V1DB](p, "up"), "Nothing to apply")
		})

		t.Run("a missing file stops up too", func(t *testing.T) {
			require.NoError(t, removeFile(p, "versions/0001_one.up.sql"))
			_, err := run[V1DB](p, "up")
			require.ErrorIs(t, err, migrate.ErrMissing)
			require.Contains(t, must[V1DB](p, "repair"), "Forgot 0001_one")
			require.Contains(t, must[V1DB](p, "up"), "Nothing to apply")
		})
	})
}

// History can live anywhere, which matters on a database shared with other tools.
func TestCustomHistoryTable(t *testing.T) {
	each(t, func(t *testing.T, p *project) {
		p.config.HistoryTable = "schema_log"
		p.config.HistorySchema = "ops_" + strings.ToLower(strings.ReplaceAll(t.Name(), "/", "_"))
		dropSchemaAfter(p, p.config.HistorySchema)
		p.write("versions/0001_one.up.sql", "SELECT 1;")
		must[V1DB](p, "up")

		require.Equal(
			t,
			1,
			scalar[int](p, `SELECT count(*) FROM "`+p.config.HistorySchema+`".schema_log`),
		)

		require.Contains(t, must[V1DB](p, "up"), "Nothing to apply")
	})
}

// A version listed in Config.Skip stays unapplied, here and in status.
func TestSkip(t *testing.T) {
	each(t, func(t *testing.T, p *project) {
		p.config.Skip = []string{"0002"}
		p.write("versions/0001_one.up.sql", "CREATE TABLE one (id int);")
		p.write("versions/0002_prod_only.up.sql", "CREATE TABLE prod_only (id int);")
		p.write("versions/0003_three.up.sql", "CREATE TABLE three (id int);")
		must[V1DB](p, "up")
		require.Empty(t, p.columns("prod_only"))
		require.NotEmpty(t, p.columns("three"))
		require.Contains(t, must[V1DB](p, "status"), "skipped")
	})
}
