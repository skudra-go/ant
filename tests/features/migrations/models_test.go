package migrations

import (
	"database/sql"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/skudra-go/ant"
	"github.com/skudra-go/ant/dialect/postgres"
	"github.com/skudra-go/ant/schema"
)

// Catalog declares a column of every kind. The Go type decides the column, and
// nothing else is needed for the common ones.
type Catalog struct {
	ID        string
	Name      string
	Code      int32
	Big       int64
	Ratio     float64
	Active    bool
	Blob      []byte
	When      time.Time
	Maybe     *string
	MaybeTime sql.NullTime
	MaybeInt  sql.Null[int64]
	Meta      ant.JSON[map[string]int]
	Labels    postgres.Array[string]
	Removed   ant.DeletedAt
	Hits      int64  `ant:"computed"`
	Skipped   string `ant:"-"`
}

func (Catalog) Configure(b *ant.ModelBuilder[Catalog]) { b.NoTimestamps() }

// CatalogDB holds Catalog.
type CatalogDB struct {
	ant.Context
	Catalogs ant.Table[Catalog]
}

func schemaOf[DB any](t *testing.T) *schema.Schema {
	t.Helper()

	s, err := ant.SchemaOf[DB](postgres.Dialect)
	require.NoError(t, err)

	return s
}

func TestColumnsComeFromGoTypes(t *testing.T) {
	table := schemaOf[CatalogDB](t).Table(schema.Ref{Name: "catalogs"})

	want := map[string]struct {
		columnType string
		nullable   bool
	}{
		"id":         {"text", false},
		"name":       {"text", false},
		"code":       {"integer", false},
		"big":        {"bigint", false},
		"ratio":      {"double precision", false},
		"active":     {"boolean", false},
		"blob":       {"bytea", true},
		"when":       {"timestamptz", false},
		"maybe":      {"text", true},
		"maybe_time": {"timestamptz", true},
		"maybe_int":  {"bigint", true},
		"meta":       {"jsonb", false},
		"labels":     {"text[]", true},
		"removed":    {"timestamptz", true},
	}
	require.Len(t, table.Columns, len(want), "computed and skipped fields have no column")

	for name, expected := range want {
		c := table.Column(name)
		require.NotNil(t, c, name)
		require.Equal(t, expected.columnType, c.Type, name)
		require.Equal(t, expected.nullable, c.Nullable, name)
	}
}

// Custom is a type ant knows nothing about.
type Custom struct{ V string }

// Scan implements sql.Scanner.
func (c *Custom) Scan(any) error { return nil }

// Unknown has a column with no obvious type.
type Unknown struct {
	ID    int64
	Value Custom
}

// UnknownDB holds Unknown.
type UnknownDB struct {
	ant.Context
	Rows ant.Table[Unknown]
}

// A type with no obvious column is an error that says what to do, not a guess.
func TestUnknownColumnType(t *testing.T) {
	_, err := ant.SchemaOf[UnknownDB](postgres.Dialect)
	require.ErrorIs(t, err, ant.ErrNoColumnType)
	require.ErrorContains(t, err, "Unknown.Value")
	require.ErrorContains(t, err, "b.Column(&...).Type(...)")
}

// Named is a type that declares its own column, which is how a library type joins.
type Named struct{ V string }

// Scan implements sql.Scanner.
func (n *Named) Scan(any) error { return nil }

// ColumnType implements schema.Typed.
func (Named) ColumnType(string) string { return "citext" }

// Declared uses it.
type Declared struct {
	ID    int64
	Value Named
}

// DeclaredDB holds Declared.
type DeclaredDB struct {
	ant.Context
	Rows ant.Table[Declared]
}

func TestTypedColumn(t *testing.T) {
	table := schemaOf[DeclaredDB](t).Table(schema.Ref{Name: "declareds"})
	require.Equal(t, "citext", table.Column("value").Type)
}

// Tuned shows the configuration a column can take.
type Tuned struct {
	ID       int64
	Code     string
	Amount   float64
	Note     *string
	Required *string
	Title    string
	Status   string
	Upper    string
}

func (Tuned) Configure(b *ant.ModelBuilder[Tuned]) {
	b.Column(&Tuneds.Code).Length(8).Collation("C").Unique("tuned_code")
	b.Column(&Tuneds.Amount).Precision(12, 4)
	b.Column(&Tuneds.Required).NotNull()
	b.Column(&Tuneds.Title).Type("citext").Nullable()
	b.Column(&Tuneds.Status).Default("'draft'")
	b.Column(&Tuneds.Upper).Generated("upper(code)")
	b.Index(&Tuneds.Code).And(&Tuneds.Amount).Desc()

	b.Index(&Tuneds.Status).
		Unique().
		Where("status = 'draft'").
		Name("one_draft").
		Using("btree").
		Include(&Tuneds.Title).
		Concurrently()

	b.IndexExpr("lower(code)").Name("tuned_lower").AndExpr("upper(code)").Unique()
	b.Check("tuned_amount", "amount > 0")
}

// TunedDB holds Tuned.
type TunedDB struct {
	ant.Context
	Rows ant.Table[Tuned]
}

// Tuneds is Tuned's probe.
var Tuneds = ant.Fields[Tuned]()

func TestColumnConfiguration(t *testing.T) {
	table := schemaOf[TunedDB](t).Table(schema.Ref{Name: "tuneds"})

	require.Equal(t, "varchar(8)", table.Column("code").Type)
	require.Equal(t, "C", table.Column("code").Collation)
	require.Equal(t, "numeric(12,4)", table.Column("amount").Type)
	require.True(t, table.Column("note").Nullable)
	require.False(t, table.Column("required").Nullable)
	require.True(t, table.Column("title").Nullable)
	require.Equal(t, "'draft'", table.Column("status").Default)
	require.Equal(t, "upper(code)", table.Column("upper").Generated)

	t.Run("a default means a zero field is left to the database", func(t *testing.T) {
		each(t, func(t *testing.T, p *project) {
			p.sql(
				`CREATE TABLE tuneds (id bigint GENERATED BY DEFAULT AS IDENTITY PRIMARY KEY, code text NOT NULL, amount numeric NOT NULL,
				note text, required text NOT NULL, title text NOT NULL, status text NOT NULL DEFAULT 'draft',
				upper text GENERATED ALWAYS AS (upper(code)) STORED)`,
			)

			db := open[TunedDB](p)

			row := &Tuned{
				Code:     "ab",
				Amount:   1,
				Required: new("x"),
				Title:    "t",
			}
			require.NoError(t, db.Rows.Insert(t.Context(), row))
			require.Equal(t, "draft", row.Status)
			require.Equal(t, "AB", row.Upper, "a generated column reads back, and is never written")
		})
	})

	t.Run("the unique constraint takes its name", func(t *testing.T) {
		require.Equal(t, "tuned_code", table.Uniques[0].Name)
	})

	t.Run("indexes take every option", func(t *testing.T) {
		byName := map[string]*schema.Index{}

		for _, index := range table.Indexes {
			byName[index.Name] = index
		}

		composite := byName["tuneds_code_amount_idx"]
		require.Equal(
			t,
			[]schema.IndexPart{{Column: "code"}, {Column: "amount", Desc: true}},
			composite.Parts,
		)

		partial := byName["one_draft"]
		require.True(t, partial.Unique)
		require.True(t, partial.Concurrent)
		require.Equal(t, "status = 'draft'", partial.Where)
		require.Equal(t, []string{"title"}, partial.Include)

		require.Equal(
			t,
			[]schema.IndexPart{{Expr: "lower(code)"}, {Expr: "upper(code)"}},
			byName["tuned_lower"].Parts,
		)
	})

	t.Run("checks are named", func(t *testing.T) {
		require.Equal(t, []*schema.Check{{Name: "tuned_amount", Expr: "amount > 0"}}, table.Checks)
	})
}

// BadDefault says it has a default, without saying what.
type BadDefault struct {
	ID     int64
	Status string `ant:"default"`
}

// BadDefaultDB holds BadDefault.
type BadDefaultDB struct {
	ant.Context
	Rows ant.Table[BadDefault]
}

func TestDefaultNeedsAnExpression(t *testing.T) {
	_, err := ant.SchemaOf[BadDefaultDB](postgres.Dialect)
	require.ErrorIs(t, err, ant.ErrBadSchema)
	require.ErrorContains(t, err, `use ant:"default=expr"`)
}

// BadIndex has an expression and no name to go with it.
type BadIndex struct{ ID int64 }

func (BadIndex) Configure(b *ant.ModelBuilder[BadIndex]) { b.IndexExpr("id + 1") }

// BadIndexDB holds BadIndex.
type BadIndexDB struct {
	ant.Context
	Rows ant.Table[BadIndex]
}

func TestExpressionIndexNeedsAName(t *testing.T) {
	_, err := ant.SchemaOf[BadIndexDB](postgres.Dialect)
	require.ErrorIs(t, err, ant.ErrBadSchema)
	require.ErrorContains(t, err, "needs a name")
}
