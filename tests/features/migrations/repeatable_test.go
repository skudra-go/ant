package migrations

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// Files in code/ run after every versioned migration, and again when they change.
// They suit whatever can be recreated, such as views and functions.
func TestRepeatable(t *testing.T) {
	each(t, func(t *testing.T, p *project) {
		p.write("versions/0001_accounts.up.sql", "CREATE TABLE accounts (id int, active boolean);")

		p.write(
			"code/10_views/active.sql",
			"CREATE OR REPLACE VIEW active_accounts AS SELECT id FROM accounts WHERE active;",
		)

		p.write(
			"code/20_functions/count.sql",
			"CREATE OR REPLACE FUNCTION active_count() RETURNS bigint AS $$ SELECT count(*) FROM active_accounts; $$ LANGUAGE sql;",
		)

		t.Run("they run after versioned files, in name order", func(t *testing.T) {
			p.write("code/00_first.sql", "CREATE OR REPLACE VIEW first_view AS SELECT 1 AS n;")
			out := must[V1DB](p, "up")
			require.Less(t, strings.Index(out, "0001_accounts"), strings.Index(out, "00_first.sql"))

			require.Less(
				t,
				strings.Index(out, "00_first.sql"),
				strings.Index(out, "10_views/active.sql"),
			)

			require.Less(
				t,
				strings.Index(out, "10_views/active.sql"),
				strings.Index(out, "20_functions/count.sql"),
			)
		})

		t.Run("an unchanged file does not run again", func(t *testing.T) {
			require.Contains(t, must[V1DB](p, "up"), "Nothing to apply")
		})

		t.Run("a changed file runs again", func(t *testing.T) {
			p.write(
				"code/10_views/active.sql",
				"CREATE OR REPLACE VIEW active_accounts AS SELECT id, active FROM accounts WHERE active;",
			)

			require.Contains(t, must[V1DB](p, "status"), "outdated")
			require.Contains(t, must[V1DB](p, "up"), "Applied 10_views/active.sql")
			require.Contains(t, p.columns("active_accounts"), "active")
		})

		t.Run("a removed file drops nothing, and status says so", func(t *testing.T) {
			require.NoError(t, removeFile(p, "code/00_first.sql"))
			require.Contains(t, must[V1DB](p, "status"), "removed")
			require.Contains(t, must[V1DB](p, "up"), "Nothing to apply")
			require.NotEmpty(t, p.columns("first_view"))
		})
	})
}

// A versioned migration may drop a view to change a column the view reads. The
// file in code/ is the view's only definition, so it runs again after any
// versioned migration, even unchanged.
func TestRepeatableAfterVersioned(t *testing.T) {
	each(t, func(t *testing.T, p *project) {
		p.write("versions/0001_items.up.sql", "CREATE TABLE items (id int, name varchar(50));")

		p.write(
			"code/item_names.sql",
			"CREATE OR REPLACE VIEW item_names AS SELECT id, name FROM items;",
		)

		must[V1DB](p, "up")

		t.Run("a migration that drops the view gets it back", func(t *testing.T) {
			p.write(
				"versions/0002_name_length.up.sql",
				"DROP VIEW IF EXISTS item_names;\nALTER TABLE items ALTER COLUMN name TYPE varchar(100);",
			)

			require.Contains(t, must[V1DB](p, "up", "--dry-run"), "-- item_names.sql")

			out := must[V1DB](p, "up")
			require.Less(
				t,
				strings.Index(out, "Applied 0002_name_length"),
				strings.Index(out, "Applied item_names.sql"),
			)

			require.Equal(t, []string{"id", "name"}, p.columns("item_names"))
		})

		t.Run("with nothing versioned to apply, nothing reruns", func(t *testing.T) {
			require.Contains(t, must[V1DB](p, "up"), "Nothing to apply")
			require.Contains(t, must[V1DB](p, "up", "--dry-run"), "Nothing to apply")
		})

		t.Run(
			"up through a version leaves it outdated, and the next up runs it",
			func(t *testing.T) {
				p.write("versions/0003_drop_again.up.sql", "DROP VIEW item_names;")
				out := must[V1DB](p, "up", "--to", "0003")
				require.NotContains(t, out, "item_names.sql")
				require.Empty(t, p.columns("item_names"))

				require.Contains(t, must[V1DB](p, "status"), "outdated")
				require.Contains(t, must[V1DB](p, "up"), "Applied item_names.sql")
				require.Equal(t, []string{"id", "name"}, p.columns("item_names"))
				require.Contains(t, must[V1DB](p, "up"), "Nothing to apply")
			},
		)
	})
}

// Seed data is a good fit for code/: idempotent SQL that reruns when edited.
func TestSeedAsRepeatableSQL(t *testing.T) {
	each(t, func(t *testing.T, p *project) {
		p.write("versions/0001_roles.up.sql", "CREATE TABLE roles (name text PRIMARY KEY);")
		p.write("code/seed_roles.sql", "INSERT INTO roles VALUES ('admin') ON CONFLICT DO NOTHING;")
		must[V1DB](p, "up")

		p.write(
			"code/seed_roles.sql",
			"INSERT INTO roles VALUES ('admin'), ('editor') ON CONFLICT DO NOTHING;",
		)

		must[V1DB](p, "up")
		require.Equal(t, 2, scalar[int](p, "SELECT count(*) FROM roles"))
	})
}
