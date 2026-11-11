package model_definition

import (
	"context"
	"reflect"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/skudra-go/ant"
	"github.com/skudra-go/ant/dialect/postgres"
	"github.com/skudra-go/ant/schema"
	"github.com/skudra-go/ant/tests/features/fixture"
)

// Dog and Cat share one table. Go has no inheritance, so each is a plain struct
// that says what its rows hold in the discriminator.
type Dog struct {
	ID    int64
	Kind  string
	Name  string
	Barks *int
}

func (Dog) Configure(b *ant.ModelBuilder[Dog]) {
	b.Table("animals")
	b.Discriminator(&Dogs.Kind, "dog")
}

// Cat shares the table.
type Cat struct {
	ID    int64
	Kind  string
	Name  string
	Lives *int
}

func (Cat) Configure(b *ant.ModelBuilder[Cat]) {
	b.Table("animals")
	b.Discriminator(&Cats.Kind, "cat")
}

// AnimalsDB holds both.
type AnimalsDB struct {
	ant.Context
	Dogs ant.Table[Dog]
	Cats ant.Table[Cat]
}

// Probes.
var (
	Dogs = ant.Fields[Dog]()
	Cats = ant.Fields[Cat]()
)

// Single-table inheritance: inserts set the discriminator, and queries see only
// their own rows.
func TestSingleTableInheritance(t *testing.T) {
	const ddl = `CREATE TABLE animals (id BIGSERIAL PRIMARY KEY, kind TEXT NOT NULL, name TEXT NOT NULL, barks INT, lives INT)`
	fixture.RunWith(t, ddl, func(t *testing.T, db *AnimalsDB) {
		ctx := t.Context()
		require.NoError(t, db.Dogs.Insert(ctx, &Dog{Name: "Rex"}))
		require.NoError(t, db.Cats.Insert(ctx, &Cat{Name: "Tom"}, &Cat{Name: "Kit"}))

		dogs, err := db.Dogs.All(ctx)
		require.NoError(t, err)
		require.Len(t, dogs, 1)
		require.Equal(t, "dog", dogs[0].Kind)

		cats, err := db.Cats.Count(ctx)
		require.NoError(t, err)
		require.EqualValues(t, 2, cats)
	})
}

// Migrations see one table, with a column only one model declares allowing NULL.
func TestSharedTableSchema(t *testing.T) {
	s, err := ant.SchemaOf[AnimalsDB](postgres.Dialect)
	require.NoError(t, err)
	require.Len(t, s.Tables, 1)
	table := s.Table(schema.Ref{Name: "animals"})
	require.False(t, table.Column("name").Nullable)
	require.True(t, table.Column("barks").Nullable)
	require.True(t, table.Column("lives").Nullable)
}

// Timestamps is meant to be embedded, and brings its index with it.
type Timestamps struct {
	Created int64
}

// ConfigureMixin implements ant.MixinConfigurer.
func (Timestamps) ConfigureMixin(m *ant.Mixin) {
	m.Index("created").Name(m.Table() + "_created_mixin_idx")
	m.Check(m.Table()+"_created_positive", "created >= 0")
}

// Stamped embeds the mixin.
type Stamped struct {
	ID int64
	Timestamps
}

// MixinDB holds it.
type MixinDB struct {
	ant.Context
	Rows ant.Table[Stamped]
}

// A mixin is an embedded struct that brings its configuration along, for what
// many models share. It works by column name, not knowing who embeds it.
func TestMixin(t *testing.T) {
	s, err := ant.SchemaOf[MixinDB](postgres.Dialect)
	require.NoError(t, err)

	table := s.Table(schema.Ref{Name: "stampeds"})
	require.Equal(t, "stampeds_created_mixin_idx", table.Indexes[0].Name)
	require.Equal(t, "stampeds_created_positive", table.Checks[0].Name)
}

// Renamed is named by a strategy.
type Renamed struct {
	RowID    int64 `ant:"column=row_id"`
	FullName string
}

func (Renamed) Configure(b *ant.ModelBuilder[Renamed]) { b.Key(&Renameds.RowID) }

// Renameds is Renamed's probe.
var Renameds = ant.Fields[Renamed]()

// NamingDB holds it.
type NamingDB struct {
	ant.Context
	Rows ant.Table[Renamed]
}

// A naming strategy replaces the conventions, which suits an existing database
// with its own style. Names set explicitly stay.
func TestNamingStrategy(t *testing.T) {
	strategy := ant.WithNaming(ant.Naming{
		Table:  func(name string) string { return "tbl_" + name },
		Column: func(name string) string { return "col_" + name },
	})

	fixture.Each(t, func(t *testing.T, target fixture.Target) {
		ctx := context.Background()
		_, err := target.DB.ExecContext(
			ctx,
			`CREATE TABLE "tbl_Renamed" (row_id BIGSERIAL PRIMARY KEY, "col_FullName" TEXT NOT NULL)`,
		)
		require.NoError(t, err)
		db := fixture.Open[NamingDB](t, target, strategy)
		// Metadata is global, so the test leaves things as it found them.
		_, err = ant.Open[NamingDB](ctx, target.Dialect, target.DSN, ant.WithNaming(ant.Naming{}))
		require.NoError(t, err)

		row := &Renamed{FullName: "x"}
		require.NoError(t, db.Rows.Insert(ctx, row))
		require.NotZero(t, row.RowID)

		loaded, err := db.Rows.Where(ant.Eq(&Renameds.FullName, "x")).First(ctx)
		require.NoError(t, err)
		require.Equal(t, row.RowID, loaded.RowID)
	})
}

// Lengthy has strings a convention limits.
type Lengthy struct {
	ID   int64
	Name string
	Note string
}

func (Lengthy) Configure(b *ant.ModelBuilder[Lengthy]) { b.Column(&Lengthies.Note).Type("text") }

// Lengthies is Lengthy's probe.
var Lengthies = ant.Fields[Lengthy]()

// ConventionDB holds it.
type ConventionDB struct {
	ant.Context
	Rows ant.Table[Lengthy]
}

// A convention applies a rule to every model, and Configure can override it.
func TestBulkConvention(t *testing.T) {
	fixture.Each(t, func(t *testing.T, target fixture.Target) {
		opened := false
		rule := func(m *ant.ModelConvention) {
			if m.Name() != "Lengthy" {
				return
			}

			for _, f := range m.Fields() {
				if f.Type.Kind() == reflect.String {
					f.SetLength(100)
				}
			}

			opened = true
		}

		_, err := target.DB.ExecContext(
			t.Context(),
			`CREATE TABLE lengthies (id BIGSERIAL PRIMARY KEY, name VARCHAR(100), note TEXT)`,
		)
		require.NoError(t, err)
		fixture.Open[ConventionDB](t, target, ant.WithConvention(rule))
		require.True(t, opened)

		s, err := ant.SchemaOf[ConventionDB](postgres.Dialect)
		require.NoError(t, err)

		table := s.Table(schema.Ref{Name: "lengthies"})
		require.Equal(t, "varchar(100)", table.Column("name").Type)
		require.Equal(t, "text", table.Column("note").Type, "Configure wins")
	})
}
