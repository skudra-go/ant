package migrations

import (
	"context"
	"strings"
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

		t.Run("a default the models lack comes with a note on zero values", func(t *testing.T) {
			p.sql("ALTER TABLE items ALTER COLUMN weight SET DEFAULT 3")
			out, err := run[ItemDB](p, "drift")
			require.ErrorIs(t, err, migrate.ErrDrift)
			require.Contains(t, out, "change column items.weight: default 3 to none")
			require.Contains(t, out, "never inserts false or 0")
			require.Contains(t, out, ".NotNull() and .Default(...)")
			p.sql("ALTER TABLE items ALTER COLUMN weight DROP DEFAULT")
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
			require.Contains(t, out, "change column items.kind: default 'fancy'::text to 'plain'")
			require.NotContains(t, out, "never inserts false or 0", "the models have a default")
			require.Contains(t, out, "CREATE UNIQUE INDEX")
		})
	})
}

// Film names a domain and a composite type, which the database has and the
// models do not create, and an enum, which they do.
type Film struct {
	ID          int64
	Title       string
	ReleaseYear int64
	Years       []int64
	Pair        string
	Code        string
	Kind        string
}

func (Film) Configure(b *ant.ModelBuilder[Film]) {
	b.Column(&Films.ReleaseYear).Type("year")
	b.Column(&Films.Years).Type("year[]")
	b.Column(&Films.Pair).Type("pair")
	b.Column(&Films.Code).Type("film_code")
	b.Column(&Films.Kind).Enum("film_kind", "short", "feature")
}

var Films = ant.Fields[Film]()

type FilmDB struct {
	ant.Context
	Films ant.Table[Film]
}

// A database that matches the models is no drift, but a type the models use
// and do not create is not in an empty database, so a migration made from the
// models alone fails there. drift names such types (#193).
func TestDriftNamesTypesTheModelsDoNotCreate(t *testing.T) {
	each(t, func(t *testing.T, p *project) {
		p.sql("CREATE DOMAIN year AS integer CHECK (VALUE BETWEEN 1901 AND 2155)")
		p.sql("CREATE TYPE pair AS (a integer, b integer)")
		p.sql("CREATE DOMAIN film_code AS text")
		// Marking the dependency stands in for an extension's type, as in
		// TestDriftSkipsExtensionTables.
		p.sql(
			`INSERT INTO pg_depend (classid, objid, objsubid, refclassid, refobjid, refobjsubid, deptype)
			SELECT 'pg_type'::regclass, 'film_code'::regtype, 0, 'pg_extension'::regclass, oid, 0, 'e'
			FROM pg_extension WHERE extname = 'plpgsql'`,
		)

		t.Cleanup(func() {
			_, _ = p.target.DB.ExecContext(
				context.Background(),
				"DELETE FROM pg_depend WHERE deptype = 'e' AND objid = 'film_code'::regtype",
			)
		})

		must[FilmDB](p, "diff", "start")
		must[FilmDB](p, "up")

		out := must[FilmDB](p, "drift")
		require.Contains(t, out, "No drift")
		require.Contains(t, out, "The models name types they do not create")
		require.Contains(t, out, "year, a domain: films.release_year, films.years")
		require.Contains(t, out, "pair, a composite type: films.pair")
		require.Contains(t, out, "film_code, from extension plpgsql: films.code")
		require.Contains(t, out, "diff --empty")
		require.NotContains(t, out, "film_kind", "the models create it")
		require.NotContains(t, out, "bigint", "PostgreSQL has it")
	})
}

// viewSchema holds a materialized view that TestDriftComparesViewModels reads.
// No table is in it.
const viewSchema = "ant_view_reports"

// Subscriber is a table the views read.
type Subscriber struct {
	ID     int64
	ListID int64
	Name   string
}

// ListStat reads a view.
type ListStat struct {
	ListID          int64
	SubscriberCount int64
}

func (ListStat) Configure(b *ant.ModelBuilder[ListStat]) {
	b.View("list_stats")
	b.NoKey()
}

// YearlyStat reads a materialized view in a schema of its own.
type YearlyStat struct {
	Year  int64
	Total int64
}

func (YearlyStat) Configure(b *ant.ModelBuilder[YearlyStat]) {
	b.Schema(viewSchema)
	b.View("yearly_stats")
	b.NoKey()
}

// SubscriberName reads the subscribers table without writing it.
type SubscriberName struct {
	ID   int64
	Name string
}

func (SubscriberName) Configure(b *ant.ModelBuilder[SubscriberName]) {
	b.View("subscribers")
}

type StatDB struct {
	ant.Context
	Subscribers ant.Table[Subscriber]
	ListStats   ant.Table[ListStat]
	YearlyStats ant.Table[YearlyStat]
	Names       ant.Table[SubscriberName]
}

// SubscriberNick reads a column that the subscribers table does not have.
type SubscriberNick struct {
	ID   int64
	Nick string
}

func (SubscriberNick) Configure(b *ant.ModelBuilder[SubscriberNick]) {
	b.View("subscribers")
}

type NickDB struct {
	ant.Context
	Subscribers ant.Table[Subscriber]
	Nicks       ant.Table[SubscriberNick]
}

// Migrations do not create views: their SQL lives in the code folder. drift
// still checks that each view a model reads is there with the columns the model
// reads, since a query fails without them (#232).
func TestDriftComparesViewModels(t *testing.T) {
	each(t, func(t *testing.T, p *project) {
		dropSchemaAfter(p, viewSchema)

		t.Run("a view that is not there is drift, before any snapshot too", func(t *testing.T) {
			out, err := run[StatDB](p, "drift")
			require.ErrorIs(t, err, migrate.ErrDrift)
			require.Contains(t, out, "view list_stats is missing")
			require.Contains(t, out, "view "+viewSchema+".yearly_stats is missing")
			require.NotContains(t, out, "view subscribers", "the models create that table")
		})

		p.write(
			"code/stats.sql",
			`CREATE OR REPLACE VIEW list_stats AS
				SELECT list_id, count(*) AS subscriber_count, max(name) AS last_name
				FROM subscribers GROUP BY list_id;
			CREATE SCHEMA IF NOT EXISTS `+viewSchema+`;
			CREATE MATERIALIZED VIEW IF NOT EXISTS `+viewSchema+`.yearly_stats AS
				SELECT 2026::bigint AS year, count(*) AS total FROM subscribers;`,
		)

		must[StatDB](p, "diff", "start")
		must[StatDB](p, "up")

		t.Run("views with the columns the models read are no drift", func(t *testing.T) {
			require.Contains(t, must[StatDB](p, "drift"), "No drift")
			require.NotContains(t, p.read("versions/schema.snapshot.json"), "list_stats")
		})

		p.sql("DROP VIEW list_stats")

		p.sql(
			"CREATE VIEW list_stats AS SELECT list_id, count(*) AS subscriber_cnt FROM subscribers GROUP BY list_id",
		)

		t.Run("a column the view lacks is drift", func(t *testing.T) {
			out, err := run[StatDB](p, "drift")
			require.ErrorIs(t, err, migrate.ErrDrift)
			require.ErrorContains(t, err, "1 difference")
			require.Contains(t, out, "view list_stats has no column subscriber_count")
			require.NotContains(t, out, "To match", "the tables match")
		})

		t.Run("unless Config.Ignore names the view or the column", func(t *testing.T) {
			for _, pattern := range []string{"list_*", "list_stats.subscriber_count"} {
				p.config.Ignore = []string{pattern}
				require.Contains(t, must[StatDB](p, "drift"), "No drift", pattern)
			}

			p.config.Ignore = nil
		})

		t.Run(
			"a read-only model of a table the models create reads what migrations make of it",
			func(t *testing.T) {
				out, err := run[NickDB](p, "drift")
				require.ErrorIs(t, err, migrate.ErrDrift)
				require.ErrorContains(t, err, "1 difference")
				require.Contains(t, out, "view subscribers has no column nick")
			},
		)

		t.Run("a view with no columns is there, without the columns", func(t *testing.T) {
			p.sql("DROP VIEW list_stats")
			p.sql("CREATE VIEW list_stats AS SELECT")
			out, err := run[StatDB](p, "drift")
			require.ErrorIs(t, err, migrate.ErrDrift)
			require.Contains(t, out, "view list_stats has no column list_id")
			require.Contains(t, out, "view list_stats has no column subscriber_count")
			require.NotContains(t, out, "list_stats is missing")
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
		require.Contains(t, out, "1 migration is pending")
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

// explicitSchema is the default schema of TestDriftExplicitDefaultSchema's
// connection, which its models name outright, as with Schema("public").
const explicitSchema = "ant_explicit_default"

// Rack names the default schema.
type Rack struct {
	ant.Base
	Name string
}

func (Rack) Configure(b *ant.ModelBuilder[Rack]) { b.Schema(explicitSchema) }

// Volume refers to Rack across the explicit name.
type Volume struct {
	ant.Base
	RackID int64
	Title  string
	Rack   *Rack
}

func (Volume) Configure(b *ant.ModelBuilder[Volume]) {
	b.Schema(explicitSchema)
	b.BelongsTo(&Volumes.Rack)
}

// Log is a table in the default schema that migrations leave alone.
type Log struct {
	ant.Base
	Note string
}

func (Log) Configure(b *ant.ModelBuilder[Log]) {
	b.Schema(explicitSchema)
	b.ExcludeFromMigrations()
}

// RackName is a view in the default schema.
type RackName struct {
	ID   int64
	Name string
}

func (RackName) Configure(b *ant.ModelBuilder[RackName]) {
	b.Schema(explicitSchema)
	b.View("rack_names")
}

// ArchiveDB holds them.
type ArchiveDB struct {
	ant.Context
	Racks   ant.Table[Rack]
	Volumes ant.Table[Volume]
	Logs    ant.Table[Log]
	Names   ant.Table[RackName]
}

// Volumes is Volume's probe.
var Volumes = ant.Fields[Volume]()

// A model that names the connection's default schema, as Schema("public") does
// on most databases, matches the tables in it: the database reports that schema
// without a name, and the two are the same schema.
func TestDriftExplicitDefaultSchema(t *testing.T) {
	each(t, func(t *testing.T, p *project) {
		dropSchemaAfter(p, explicitSchema)
		p.sql("CREATE SCHEMA " + explicitSchema)

		p.sql(
			"CREATE TABLE " + explicitSchema + `.logs (
				id bigint PRIMARY KEY,
				created_at timestamptz NOT NULL,
				updated_at timestamptz NOT NULL,
				note text NOT NULL
			)`,
		)

		current := scalar[string](p, "SELECT current_schema()")
		p.config.DSN = strings.Replace(
			p.config.DSN,
			"search_path="+current,
			"search_path="+explicitSchema,
			1,
		)

		p.target.DSN = p.config.DSN

		must[ArchiveDB](p, "diff", "start")
		must[ArchiveDB](p, "up")

		p.sql(
			"CREATE VIEW " + explicitSchema + ".rack_names AS SELECT id, name FROM " + explicitSchema + ".racks",
		)

		require.Contains(t, must[ArchiveDB](p, "drift"), "No drift")

		t.Run("Config.Ignore names a view there without the schema", func(t *testing.T) {
			p.sql("DROP VIEW " + explicitSchema + ".rack_names")
			_, err := run[ArchiveDB](p, "drift")
			require.ErrorIs(t, err, migrate.ErrDrift)

			p.config.Ignore = []string{"rack_names"}
			require.Contains(t, must[ArchiveDB](p, "drift"), "No drift")
			p.config.Ignore = nil
		})

		t.Run("EnsureCreated leaves the tables alone", func(t *testing.T) {
			db := open[ArchiveDB](p)
			ctx := t.Context()
			rack := &Rack{Name: "kept"}
			require.NoError(t, db.Racks.Insert(ctx, rack))
			require.NoError(t, db.EnsureCreated(ctx))

			n, err := db.Racks.Count(ctx)
			require.NoError(t, err)
			require.Equal(t, int64(1), n)
		})
	})
}
