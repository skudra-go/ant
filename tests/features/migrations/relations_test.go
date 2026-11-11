package migrations

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/skudra-go/ant"
	"github.com/skudra-go/ant/dialect/postgres"
	"github.com/skudra-go/ant/schema"
)

// Author, Book, Shelf, Genre, and Series show how relations become foreign keys.
type Author struct {
	ant.Base
	Email string `ant:"unique"`
	Books []Book
	Fans  []Reader
}

func (Author) Configure(b *ant.ModelBuilder[Author]) {
	b.HasMany(&Authors.Books).OnDelete(ant.Cascade)
	b.ManyToMany(&Authors.Fans).Through("author_fans")
}

// Book has one required parent and two optional ones.
type Book struct {
	ant.Base
	AuthorID    int64
	Author      *Author
	EditorEmail *string
	Editor      *Author
	ShelfID     *int64
	Shelf       *Shelf
	GenreID     *int64
	ParentID    *int64
	Parent      *Book
}

func (Book) Configure(b *ant.ModelBuilder[Book]) {
	b.BelongsTo(&Books.Editor).
		ForeignKey(&Books.EditorEmail).
		References(&Authors.Email).
		ConstraintName("books_editor_fk")

	b.BelongsTo(&Books.Shelf).OnDelete(ant.SetNull).OnUpdate(ant.Cascade).NoIndex()
	b.BelongsTo(&Books.Parent).OnDelete(ant.Restrict)
	b.Index(&Books.AuthorID).And(&Books.CreatedAt)
}

// Shelf is only reachable through a relation, and still gets its table.
type Shelf struct {
	ID   int64
	Room string
}

// Reader is the other side of a many-to-many relation, and declares it too.
type Reader struct {
	ID      int64
	Authors []Author
}

func (Reader) Configure(b *ant.ModelBuilder[Reader]) {
	b.ManyToMany(&Readers.Authors).Through("author_fans")
}

// External belongs to another system, so migrations leave it alone.
type External struct{ ID int64 }

func (External) Configure(b *ant.ModelBuilder[External]) { b.ExcludeFromMigrations() }

// Report is a view, which belongs in the code folder.
type Report struct{ ID int64 }

func (Report) Configure(b *ant.ModelBuilder[Report]) { b.View("reports") }

// Linked refers to External, with and without a foreign key.
type Linked struct {
	ID         int64
	ExternalID int64
	External   *External
	OtherID    int64
	Other      *Book
}

func (Linked) Configure(b *ant.ModelBuilder[Linked]) {
	b.BelongsTo(&Linkeds.Other).ForeignKey(&Linkeds.OtherID).NoForeignKey()
}

// LibraryDB declares its tables. Shelf and Reader come through relations.
type LibraryDB struct {
	ant.Context
	Authors   ant.Table[Author]
	Books     ant.Table[Book]
	Externals ant.Table[External]
	Reports   ant.Table[Report]
	Linked    ant.Table[Linked]
}

// Probes for the models.
var (
	Authors = ant.Fields[Author]()
	Books   = ant.Fields[Book]()
	Readers = ant.Fields[Reader]()
	Linkeds = ant.Fields[Linked]()
)

func keys(t *schema.Table) map[string]*schema.ForeignKey {
	out := map[string]*schema.ForeignKey{}

	for _, key := range t.ForeignKeys {
		out[key.Name] = key
	}

	return out
}

func indexed(t *schema.Table, name string) bool {
	for _, index := range t.Indexes {
		if index.Name == name {
			return true
		}
	}

	return false
}

func TestRelationsBecomeForeignKeys(t *testing.T) {
	s := schemaOf[LibraryDB](t)
	books := s.Table(schema.Ref{Name: "books"})
	got := keys(books)

	t.Run("both sides of a relation are one key, and either can configure it", func(t *testing.T) {
		require.Equal(t, "CASCADE", got["books_author_id_fkey"].OnDelete)
	})

	t.Run("a reference to a unique column", func(t *testing.T) {
		key := got["books_editor_fk"]
		require.Equal(t, []string{"email"}, key.RefColumns)
		require.Equal(t, "authors", key.RefTable)
	})

	t.Run("delete and update actions", func(t *testing.T) {
		require.Equal(t, "SET NULL", got["books_shelf_id_fkey"].OnDelete)
		require.Equal(t, "CASCADE", got["books_shelf_id_fkey"].OnUpdate)
		require.Equal(t, "RESTRICT", got["books_parent_id_fkey"].OnDelete)
	})

	t.Run("a field that only looks like a foreign key is not one", func(t *testing.T) {
		require.Len(t, got, 4, "GenreID has no relation behind it")
	})

	t.Run("foreign keys get an index, unless one already covers them", func(t *testing.T) {
		require.True(t, indexed(books, "books_editor_email_idx"))
		require.True(t, indexed(books, "books_parent_id_idx"))
		require.True(t, indexed(books, "books_author_id_created_at_idx"))

		require.False(
			t,
			indexed(books, "books_author_id_idx"),
			"the composite index starts with it",
		)

		require.False(t, indexed(books, "books_shelf_id_idx"), "NoIndex")
	})

	t.Run("models reached through relations get tables", func(t *testing.T) {
		require.NotNil(t, s.Table(schema.Ref{Name: "shelfs"}))
		require.NotNil(t, s.Table(schema.Ref{Name: "readers"}))
	})

	t.Run("a join table is declared once, whichever sides mention it", func(t *testing.T) {
		pivot := s.Table(schema.Ref{Name: "author_fans"})
		require.NotNil(t, pivot)
		require.ElementsMatch(t, []string{"author_id", "reader_id"}, pivot.PrimaryKey.Columns)
		require.Len(t, pivot.ForeignKeys, 2)
		require.Equal(t, "CASCADE", pivot.ForeignKeys[0].OnDelete)
		require.True(t, indexed(pivot, "author_fans_reader_id_idx"))
	})

	t.Run("views and excluded models have no table", func(t *testing.T) {
		require.Nil(t, s.Table(schema.Ref{Name: "reports"}))
		require.Nil(t, s.Table(schema.Ref{Name: "externals"}))
		require.Equal(t, []schema.Ref{{Name: "externals"}}, s.Unmanaged)
	})

	t.Run("a key to an excluded table stays, and NoForeignKey removes one", func(t *testing.T) {
		linked := keys(s.Table(schema.Ref{Name: "linkeds"}))
		require.Contains(t, linked, "linkeds_external_id_fkey")
		require.NotContains(t, linked, "linkeds_other_id_fkey")
	})
}

// The whole model applies to a real database, and comes back identical.
func TestRelationsApply(t *testing.T) {
	each(t, func(t *testing.T, p *project) {
		p.sql("CREATE TABLE externals (id bigint PRIMARY KEY)")
		must[LibraryDB](p, "diff", "start")
		must[LibraryDB](p, "up")
		require.Contains(t, must[LibraryDB](p, "drift"), "No drift")
	})
}

// Invoice lives in a database schema of its own.
type Invoice struct {
	ID         int64
	CustomerID int64
	Customer   *Customer
}

func (Invoice) Configure(b *ant.ModelBuilder[Invoice]) { b.Schema("billing_test") }

// Customer lives in the default schema.
type Customer struct {
	ID       int64
	Invoices []Invoice
}

// BillingDB uses two schemas.
type BillingDB struct {
	ant.Context
	Invoices  ant.Table[Invoice]
	Customers ant.Table[Customer]
}

// A model in another schema creates that schema, and everything stays qualified.
func TestMultipleSchemas(t *testing.T) {
	each(t, func(t *testing.T, p *project) {
		dropSchemaAfter(p, "billing_test")
		must[BillingDB](p, "diff", "start")
		text := p.read("versions/0001_start.up.sql")
		require.Contains(t, text, `CREATE SCHEMA IF NOT EXISTS "billing_test"`)
		require.Contains(t, text, `CREATE TABLE "billing_test"."invoices"`)
		must[BillingDB](p, "up")
		require.Contains(t, must[BillingDB](p, "drift"), "No drift")

		require.Equal(
			t,
			1,
			scalar[int](
				p,
				"SELECT count(*) FROM information_schema.tables WHERE table_schema = 'billing_test'",
			),
		)
	})
}

// BadSetNull asks for NULL in a column that cannot hold it.
type BadSetNull struct {
	ID       int64
	ParentID int64
	Parent   *BadSetNull
}

func (BadSetNull) Configure(b *ant.ModelBuilder[BadSetNull]) {
	b.BelongsTo(&BadSetNulls.Parent).OnDelete(ant.SetNull)
}

// BadSetNullDB holds BadSetNull.
type BadSetNullDB struct {
	ant.Context
	Rows ant.Table[BadSetNull]
}

// BadSetNulls is BadSetNull's probe.
var BadSetNulls = ant.Fields[BadSetNull]()

// A model that cannot work is an error at generation, not at the first delete.
func TestInvalidRelations(t *testing.T) {
	_, err := ant.SchemaOf[BadSetNullDB](postgres.Dialect)
	require.ErrorIs(t, err, ant.ErrBadSchema)
	require.ErrorContains(t, err, "cannot hold NULL")
}

// EnsureCreated builds tables straight from the structs, for tests and prototypes.
// EnsureDeleted removes them.
func TestEnsureCreated(t *testing.T) {
	each(t, func(t *testing.T, p *project) {
		db := open[V3DB](p)
		ctx := t.Context()

		require.NoError(t, db.EnsureCreated(ctx))

		require.NoError(
			t,
			db.Accounts.Insert(ctx, &V3Account{DisplayName: "Ada", Email: "ada@example.com"}),
		)

		t.Run("existing tables are left alone", func(t *testing.T) {
			require.NoError(t, db.EnsureCreated(ctx))
			require.Equal(t, 1, scalar[int](p, "SELECT count(*) FROM accounts"))
		})

		t.Run("only missing tables are created", func(t *testing.T) {
			p.sql("DROP TABLE notes")
			require.NoError(t, db.EnsureCreated(ctx))
			require.NotEmpty(t, p.columns("notes"))
			require.Equal(t, 1, scalar[int](p, "SELECT count(*) FROM accounts"))
		})

		t.Run("EnsureDeleted drops them", func(t *testing.T) {
			require.NoError(t, db.EnsureDeleted(ctx))
			require.Empty(t, p.columns("accounts"))
			require.NoError(t, db.EnsureDeleted(ctx), "and is safe to repeat")
		})
	})
}
