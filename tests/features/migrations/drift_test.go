package migrations

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/skudra-go/ant"
	"github.com/skudra-go/ant/migrate"
)

// Status is a model with the expressions PostgreSQL likes to rewrite.
type Item struct {
	ant.Base
	Kind    string `ant:"default='plain'"`
	Price   float64
	Weight  int32
	Label   string
	Lower   string
	Tags    ant.JSON[[]string]
	Deleted ant.DeletedAt
}

func (Item) Configure(b *ant.ModelBuilder[Item]) {
	b.Column(&Items.Price).Precision(10, 2)
	b.Column(&Items.Label).Length(50).Default("'none'")
	b.Column(&Items.Lower).Generated("lower(label)")
	b.Check("items_kind_check", "kind IN ('plain', 'fancy')")
	b.Check("items_weight_check", "weight BETWEEN 0 AND 100")
	b.Index(&Items.Kind).And(&Items.Weight).Desc().Where("kind <> 'plain'").Include(&Items.Price)
	b.IndexExpr("lower(label)").Name("items_label_lower_idx").Unique()
	// NULLS FIRST going up and NULLS LAST going down are not the defaults.
	b.Index(&Items.Weight).
		NullsFirst().
		And(&Items.Price).
		Desc().
		NullsLast().
		Name("items_weight_nulls_idx")
}

// ItemDB holds Item.
type ItemDB struct {
	ant.Context
	Items ant.Table[Item]
}

// Items is Item's probe.
var Items = ant.Fields[Item]()

// drift compares the live database with what migrations describe. PostgreSQL
// rewrites much of what it is given, so both sides are read back from the server.
func TestDrift(t *testing.T) {
	each(t, func(t *testing.T, p *project) {
		must[ItemDB](p, "diff", "start")
		must[ItemDB](p, "up")

		t.Run("expressions the server rewrites are not drift", func(t *testing.T) {
			require.Contains(t, must[ItemDB](p, "drift"), "No drift")
		})

		t.Run("so is an index that puts NULLs elsewhere", func(t *testing.T) {
			p.sql("DROP INDEX items_weight_nulls_idx")
			p.sql("CREATE INDEX items_weight_nulls_idx ON items (weight, price DESC)")
			out, err := run[ItemDB](p, "drift", "--sql")
			require.ErrorIs(t, err, migrate.ErrDrift)
			require.Contains(t, out, `("weight" NULLS FIRST, "price" DESC NULLS LAST)`)
			p.sql("DROP INDEX items_weight_nulls_idx")

			p.sql(
				"CREATE INDEX items_weight_nulls_idx ON items (weight NULLS FIRST, price DESC NULLS LAST)",
			)

			require.Contains(t, must[ItemDB](p, "drift"), "No drift")
		})

		t.Run("a change made by hand is", func(t *testing.T) {
			p.sql("ALTER TABLE items ADD COLUMN hotfix text")
			out, err := run[ItemDB](p, "drift")
			require.ErrorIs(t, err, migrate.ErrDrift)
			require.Contains(t, out, "drop column items.hotfix")
			p.sql("ALTER TABLE items DROP COLUMN hotfix")
		})

		t.Run("so is a dropped index, a changed check, and a changed default", func(t *testing.T) {
			p.sql("DROP INDEX items_label_lower_idx")

			p.sql(
				"ALTER TABLE items DROP CONSTRAINT items_weight_check, ADD CONSTRAINT items_weight_check CHECK (weight BETWEEN 0 AND 200)",
			)

			p.sql("ALTER TABLE items ALTER COLUMN kind SET DEFAULT 'fancy'")
			out, err := run[ItemDB](p, "drift", "--sql")
			require.ErrorIs(t, err, migrate.ErrDrift)
			require.Contains(t, out, "create index items_label_lower_idx")
			require.Contains(t, out, "add check items_weight_check")
			require.Contains(t, out, "change column items.kind: default")
			require.Contains(t, out, "CREATE UNIQUE INDEX")
		})
	})
}

// Things outside the schema's reach are not reported: tables ant does not manage
// are named, and history is not drift.
func TestDriftIgnores(t *testing.T) {
	each(t, func(t *testing.T, p *project) {
		must[V1DB](p, "diff", "start")
		must[V1DB](p, "up")

		t.Run("the history table never counts", func(t *testing.T) {
			require.Contains(t, must[V1DB](p, "drift"), "No drift")
		})

		p.sql("CREATE TABLE legacy_log (id int)")
		p.sql("CREATE INDEX accounts_handmade_idx ON accounts (name)")

		t.Run("other tables do", func(t *testing.T) {
			out, err := run[V1DB](p, "drift")
			require.ErrorIs(t, err, migrate.ErrDrift)
			require.Contains(t, out, "drop table legacy_log")
			require.Contains(t, out, "drop index accounts_handmade_idx")
		})

		t.Run("unless named in Config.Ignore", func(t *testing.T) {
			p.config.Ignore = []string{"legacy_*", "accounts.accounts_handmade_idx"}
			require.Contains(t, must[V1DB](p, "drift"), "No drift")
		})
	})
}

// Tables an extension owns are the extension's business.
func TestDriftSkipsExtensionTables(t *testing.T) {
	each(t, func(t *testing.T, p *project) {
		must[V1DB](p, "diff", "start")
		must[V1DB](p, "up")
		p.sql("CREATE TABLE from_extension (id int)")
		// Marking the dependency directly stands in for an extension, which a test
		// database may not have available.
		p.sql(
			`INSERT INTO pg_depend (classid, objid, objsubid, refclassid, refobjid, refobjsubid, deptype)
			SELECT 'pg_class'::regclass, 'from_extension'::regclass, 0, 'pg_extension'::regclass, oid, 0, 'e'
			FROM pg_extension WHERE extname = 'plpgsql'`,
		)

		t.Cleanup(func() {
			_, _ = p.target.DB.ExecContext(
				context.Background(),
				"DELETE FROM pg_depend WHERE deptype = 'e' AND objid = 'from_extension'::regclass",
			)
		})

		require.Contains(t, must[V1DB](p, "drift"), "No drift")
	})
}

// A database behind its migrations is not drift, but drift says why it differs.
func TestDriftWithPendingMigrations(t *testing.T) {
	each(t, func(t *testing.T, p *project) {
		must[V1DB](p, "diff", "start")
		out, err := run[V1DB](p, "drift")
		require.ErrorIs(t, err, migrate.ErrDrift)
		require.Contains(t, out, "1 migrations are pending")
	})
}

// With no snapshot yet, drift uses the models, which is the loop for adopting an
// existing database: adjust the structs until there is nothing left.
func TestDriftBeforeAnySnapshot(t *testing.T) {
	each(t, func(t *testing.T, p *project) {
		p.sql(
			"CREATE TABLE accounts (id bigserial PRIMARY KEY, created_at timestamptz NOT NULL, updated_at timestamptz NOT NULL, name text NOT NULL, email text NOT NULL UNIQUE)",
		)

		require.Contains(t, must[V1DB](p, "drift"), "matches the models")
	})
}
