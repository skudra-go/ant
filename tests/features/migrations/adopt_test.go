package migrations

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/skudra-go/ant"
)

// Legacy is the kind of model that adopts an existing table. Its names and types
// are whatever the database already uses.
type Legacy struct {
	ID       int32
	FullName string
	Status   string
	Created  string
}

func (Legacy) Configure(b *ant.ModelBuilder[Legacy]) {
	b.Table("tbl_customer")
	b.KeyName("pk_customer")
	b.NoTimestamps()
	b.Column(&Legacies.FullName).Type("varchar(120)").Unique("uq_customer_name")
	b.Column(&Legacies.Status).Type("varchar(10)").Default("'new'")
	b.Column(&Legacies.Created).Type("date").Default("CURRENT_DATE")
	b.Check("ck_customer_status", "status IN ('new', 'gone')")
	b.Index(&Legacies.Status).Name("ix_customer_status")
}

// LegacyDB holds Legacy.
type LegacyDB struct {
	ant.Context
	Customers ant.Table[Legacy]
}

// Legacies is Legacy's probe.
var Legacies = ant.Fields[Legacy]()

const legacySchema = `
	CREATE TABLE tbl_customer (
		id serial CONSTRAINT pk_customer PRIMARY KEY,
		full_name character varying(120) NOT NULL CONSTRAINT uq_customer_name UNIQUE,
		status varchar(10) NOT NULL DEFAULT 'new' CONSTRAINT ck_customer_status CHECK (status IN ('new', 'gone')),
		created date NOT NULL DEFAULT CURRENT_DATE
	);
	CREATE INDEX ix_customer_status ON tbl_customer (status);
	INSERT INTO tbl_customer (full_name) VALUES ('Existing Row');`

// A database with history joins by baseline: its tables stay, and its existing
// state counts as the first migration. New databases run that migration.
func TestAdoptingAnExistingDatabase(t *testing.T) {
	each(t, func(t *testing.T, p *project) {
		p.sql(legacySchema)

		t.Run("drift proves the models describe what is there", func(t *testing.T) {
			require.Contains(t, must[LegacyDB](p, "drift"), "No drift")
		})

		must[LegacyDB](p, "diff", "baseline")

		t.Run("baseline records it without running it", func(t *testing.T) {
			out := must[LegacyDB](p, "baseline")
			require.Contains(t, out, "Recorded 0001_baseline as applied, without running it")
			require.Equal(t, 1, scalar[int](p, "SELECT count(*) FROM tbl_customer"))
			require.Contains(t, must[LegacyDB](p, "up"), "Nothing to apply")
		})

		t.Run("the models then work on the old table", func(t *testing.T) {
			db := open[LegacyDB](p)
			row, err := db.Customers.First(t.Context())
			require.NoError(t, err)
			require.Equal(t, "Existing Row", row.FullName)
		})

		t.Run("the next change is an ordinary migration", func(t *testing.T) {
			p.sql("ALTER TABLE tbl_customer ADD COLUMN extra text")
			must[LegacyDB](p, "diff", "--empty", "later")
			p.write("versions/0002_later.up.sql", "ALTER TABLE tbl_customer DROP COLUMN extra;")
			must[LegacyDB](p, "up")
			require.Contains(t, must[LegacyDB](p, "drift"), "No drift")
		})
	})
}

// A model that only nearly matches is exactly what drift is for: each difference
// is named, so the loop with a person or an assistant ends.
func TestDriftGuidesAdoption(t *testing.T) {
	each(t, func(t *testing.T, p *project) {
		p.sql(`CREATE TABLE accounts (
			id bigint PRIMARY KEY, created_at timestamptz NOT NULL, updated_at timestamptz NOT NULL,
			name varchar(50) NOT NULL, email text NOT NULL, age int)`)

		out, err := run[V1DB](p, "drift")
		require.Error(t, err)
		require.Contains(t, out, "change column accounts.id: identity")
		require.Contains(t, out, "change column accounts.name: type character varying(50) to text")
		require.Contains(t, out, "add unique accounts_email_key on accounts")
		require.Contains(t, out, "drop column accounts.age")
	})
}

// A trimmed copy of what pg_dump --schema-only writes today. The \restrict lines
// are psql commands, and the settings would otherwise leak into what runs next.
const dump = `--
-- PostgreSQL database dump
--

\restrict abc123

SET statement_timeout = 0;
SET client_encoding = 'UTF8';
SELECT pg_catalog.set_config('search_path', '', false);
SET check_function_bodies = false;

CREATE TYPE %[1]s.mood AS ENUM ('sad', 'ok');

CREATE TABLE %[1]s.tbl_customer (
    id integer NOT NULL,
    full_name character varying(120) NOT NULL,
    status character varying(10) DEFAULT 'new'::character varying NOT NULL,
    created date DEFAULT CURRENT_DATE NOT NULL,
    CONSTRAINT ck_customer_status CHECK (((status)::text = ANY ((ARRAY['new'::character varying, 'gone'::character varying])::text[])))
);

CREATE SEQUENCE %[1]s.tbl_customer_id_seq AS integer START WITH 1 INCREMENT BY 1 NO MINVALUE NO MAXVALUE CACHE 1;
ALTER SEQUENCE %[1]s.tbl_customer_id_seq OWNED BY %[1]s.tbl_customer.id;
ALTER TABLE ONLY %[1]s.tbl_customer ALTER COLUMN id SET DEFAULT nextval('%[1]s.tbl_customer_id_seq'::regclass);
ALTER TABLE ONLY %[1]s.tbl_customer ADD CONSTRAINT pk_customer PRIMARY KEY (id);
ALTER TABLE ONLY %[1]s.tbl_customer ADD CONSTRAINT uq_customer_name UNIQUE (full_name);
CREATE INDEX ix_customer_status ON %[1]s.tbl_customer USING btree (status);

--
-- PostgreSQL database dump complete
--

\unrestrict abc123
`

// adopt starts from a dump of the schema, so new databases get everything, and
// existing ones record it as done. The models become the snapshot.
func TestAdoptFromADump(t *testing.T) {
	each(t, func(t *testing.T, p *project) {
		current := scalar[string](p, "SELECT current_schema()")
		file := filepath.Join(t.TempDir(), "schema.sql")
		require.NoError(t, os.WriteFile(file, []byte(fmt.Sprintf(dump, current)), 0o644))
		out := must[LegacyDB](p, "adopt", file)
		require.Contains(t, out, "0001_baseline.up.sql")

		t.Run("psql commands are gone, and the rest is the file you gave", func(t *testing.T) {
			text := p.read("versions/0001_baseline.up.sql")
			require.NotContains(t, text, "restrict")
			require.Contains(t, text, "CREATE TYPE")
		})

		t.Run("check passes straight away", func(t *testing.T) {
			must[LegacyDB](p, "check")
		})

		t.Run("a new database runs it, and models match the result", func(t *testing.T) {
			must[LegacyDB](p, "up")
			require.NotEmpty(t, p.columns("tbl_customer"))
			p.sql("INSERT INTO tbl_customer (full_name) VALUES ('Ada')")
			require.Contains(t, must[LegacyDB](p, "drift"), "No drift")
		})

		t.Run("what runs after a dump still works", func(t *testing.T) {
			p.write("versions/0002_after.up.sql", "CREATE TABLE after_dump (id int);")
			must[LegacyDB](p, "up")
			require.NotEmpty(t, p.columns("after_dump"))
		})

		t.Run("a project with migrations uses baseline instead", func(t *testing.T) {
			_, err := run[LegacyDB](p, "adopt", file)
			require.ErrorContains(t, err, "already has migrations")
		})
	})
}

// Keeping an old folder: files from another tool work as they are, down files are
// ignored, and baseline marks them done.
func TestAdoptingAnExistingMigrationFolder(t *testing.T) {
	each(t, func(t *testing.T, p *project) {
		p.write("versions/000001_create.up.sql", "CREATE TABLE tbl_old (id int);")
		p.write("versions/000001_create.down.sql", "DROP TABLE tbl_old;")
		p.write("versions/000002_more.up.sql", "ALTER TABLE tbl_old ADD COLUMN extra int;")
		p.sql("CREATE TABLE tbl_old (id int, extra int)")

		must[V1DB](p, "baseline")
		require.Contains(t, must[V1DB](p, "up"), "Nothing to apply")

		t.Run("new files continue the old numbering", func(t *testing.T) {
			must[V1DB](p, "diff", "--empty", "next")
			require.True(t, p.exists("versions/000003_next.up.sql"))
		})
	})
}

// A file that looks like a migration but cannot be read is an error. Skipping it
// would skip a schema change without a word.
func TestUnreadableFileName(t *testing.T) {
	each(t, func(t *testing.T, p *project) {
		p.write("versions/add_things.sql", "SELECT 1;")
		_, err := run[V1DB](p, "up")
		require.ErrorContains(t, err, "cannot read a version from versions/add_things.sql")
	})
}
