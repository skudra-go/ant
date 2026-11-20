package migrations

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/skudra-go/ant/migrate"
)

// check is for CI: it fails when someone changed a struct without generating a
// migration, and needs no database.
func TestCheck(t *testing.T) {
	each(t, func(t *testing.T, p *project) {
		p.config.DSN = ""

		t.Run("a project with no migrations fails", func(t *testing.T) {
			_, err := run[V1DB](p, "check")
			require.ErrorIs(t, err, migrate.ErrPendingChanges)
		})

		must[V1DB](p, "diff", "start")

		t.Run("models that match pass", func(t *testing.T) {
			require.Contains(t, must[V1DB](p, "check"), "OK")
		})

		t.Run("a struct change with no migration fails, and says what changed", func(t *testing.T) {
			_, err := run[V2DB](p, "check")
			require.ErrorIs(t, err, migrate.ErrPendingChanges)
			require.ErrorContains(t, err, "add column accounts.bio")
		})
	})
}

// Two branches that each add a migration get the same number. This is caught on
// every command, before anything runs.
func TestDuplicateVersions(t *testing.T) {
	each(t, func(t *testing.T, p *project) {
		p.write("versions/0002_from_alice.up.sql", "SELECT 1;")
		p.write("versions/0002_from_bob.up.sql", "SELECT 2;")

		for _, command := range []string{"up", "status", "check"} {
			_, err := run[V1DB](p, command)
			require.ErrorIs(t, err, migrate.ErrDuplicateVersion, command)
			require.ErrorContains(t, err, "0002_from_alice and 0002_from_bob")
		}
	})
}

// A branch merged late brings a migration older than what production has run.
// Running it could depend on an order that never happened, so up stops and says so.
func TestOutOfOrder(t *testing.T) {
	each(t, func(t *testing.T, p *project) {
		p.write("versions/0001_one.up.sql", "CREATE TABLE one (id int);")
		p.write("versions/0003_three.up.sql", "CREATE TABLE three (id int);")
		must[V1DB](p, "up")
		p.write("versions/0002_late.up.sql", "CREATE TABLE late (id int);")

		t.Run("it is refused by default", func(t *testing.T) {
			_, err := run[V1DB](p, "up")
			require.ErrorIs(t, err, migrate.ErrOutOfOrder)
			require.Contains(t, must[V1DB](p, "status"), "late")
		})

		t.Run("Config.OutOfOrder allows it", func(t *testing.T) {
			p.config.OutOfOrder = true
			must[V1DB](p, "up")
			require.NotEmpty(t, p.columns("late"))
		})
	})
}

// The snapshot records the migration it belongs to, so a merge that brought a
// migration without its snapshot change fails check, even if git merged cleanly.
func TestStaleSnapshot(t *testing.T) {
	each(t, func(t *testing.T, p *project) {
		must[V2DB](p, "diff", "start")
		p.write("versions/0002_from_other_branch.up.sql", "SELECT 1;")

		_, err := run[V2DB](p, "check")
		require.ErrorIs(t, err, migrate.ErrStaleSnapshot)

		t.Run("snapshot is how to say the merge is resolved", func(t *testing.T) {
			must[V2DB](p, "snapshot")
			require.Contains(t, must[V2DB](p, "check"), "OK")
		})
	})
}

// snapshot also accepts a change made by hand, with no migration written.
func TestAcceptingAHandWrittenChange(t *testing.T) {
	each(t, func(t *testing.T, p *project) {
		must[V1DB](p, "diff", "start")
		must[V1DB](p, "up")
		must[V2DB](p, "diff", "--empty", "by_hand")

		p.write(
			"versions/0002_by_hand.up.sql",
			`ALTER TABLE accounts ADD COLUMN bio text, ADD COLUMN age bigint NOT NULL DEFAULT 21;
ALTER TABLE accounts ALTER COLUMN age DROP DEFAULT;`,
		)

		must[V2DB](p, "up")
		must[V2DB](p, "snapshot")
		require.Contains(t, must[V2DB](p, "diff", "nothing"), "No changes")
		require.Contains(t, must[V2DB](p, "drift"), "No drift")
	})
}

// Two branches both write 0002, and one database already ran the other
// branch's. Once one file is renumbered, that database's 0002 is a different
// migration from the file, not an edit to it: read as one, repair recorded the
// file as applied though it never ran there (#178).
func TestRenumberedMigration(t *testing.T) {
	each(t, func(t *testing.T, p *project) {
		p.write("versions/0001_init.up.sql", "CREATE TABLE items (id int);")
		p.write("versions/0002_add_bee.up.sql", "ALTER TABLE items ADD COLUMN bee int;")
		must[V1DB](p, "up")

		// After the merge, bee became 0003 and ay took 0002.
		require.NoError(t, removeFile(p, "versions/0002_add_bee.up.sql"))
		p.write("versions/0002_add_ay.up.sql", "ALTER TABLE items ADD COLUMN ay int;")
		p.write("versions/0003_add_bee.up.sql", "ALTER TABLE items ADD COLUMN bee int;")

		const said = "0002_add_ay was applied as 0002_add_bee, a different migration"

		for _, command := range []string{"up", "repair"} {
			_, err := run[V1DB](p, command)
			require.ErrorIs(t, err, migrate.ErrChecksum, command)
			require.ErrorContains(t, err, said, command)
		}

		require.Equal(t, []string{"id", "bee"}, p.columns("items"), "nothing ran or was recorded")

		t.Run("an edit that keeps the name is still one repair can accept", func(t *testing.T) {
			require.NoError(t, removeFile(p, "versions/0002_add_ay.up.sql"))
			require.NoError(t, removeFile(p, "versions/0003_add_bee.up.sql"))

			p.write(
				"versions/0002_add_bee.up.sql",
				"ALTER TABLE items ADD COLUMN bee int; -- edited",
			)

			_, err := run[V1DB](p, "up")
			require.ErrorIs(t, err, migrate.ErrChecksum)
			require.NotContains(t, err.Error(), "a different migration")

			require.Contains(
				t,
				must[V1DB](p, "repair"),
				"Accepted the current contents of 0002_add_bee",
			)
		})

		t.Run("a new name with the same contents is the same migration", func(t *testing.T) {
			require.NoError(t, removeFile(p, "versions/0002_add_bee.up.sql"))

			p.write(
				"versions/0002_add_bee_column.up.sql",
				"ALTER TABLE items ADD COLUMN bee int; -- edited",
			)

			require.Contains(t, must[V1DB](p, "up"), "Nothing to apply")
		})
	})
}

// A file deleted to write it again under a better name takes its change with it,
// while the snapshot still holds the change. diff found nothing to write, and the
// advice to run snapshot made check pass with the change in no migration (#179).
func TestDeletedNewestFile(t *testing.T) {
	each(t, func(t *testing.T, p *project) {
		must[V1DB](p, "diff", "start")
		before := p.read("versions/schema.snapshot.json")
		must[V2DB](p, "diff", "add_bio")
		require.Len(t, listFiles(p, "versions"), 2)

		for _, f := range listFiles(p, "versions") {
			if f != listFiles(p, "versions")[0] {
				require.NoError(t, removeFile(p, "versions/"+f))
			}
		}

		for _, args := range [][]string{{"diff", "add_bio_column"}, {"diff", "by_hand", "--empty"}, {"check"}} {
			_, err := run[V2DB](p, args...)
			require.ErrorIs(t, err, migrate.ErrStaleSnapshot, args)
			require.ErrorContains(t, err, "which has no file", args)
			require.ErrorContains(t, err, "put back the snapshot it replaced", args)
		}

		require.Len(t, listFiles(p, "versions"), 1, "nothing was written")

		t.Run("with the snapshot put back, diff writes the change again", func(t *testing.T) {
			p.write("versions/schema.snapshot.json", before)
			require.Contains(t, must[V2DB](p, "diff", "add_bio_column"), "add_bio_column")
			require.Contains(t, must[V2DB](p, "check"), "OK")
		})
	})
}
