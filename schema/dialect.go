package schema

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"reflect"
)

// ErrUnsupported reports a dialect with no migration support.
var ErrUnsupported = errors.New("ant: dialect does not support migrations")

// Typed is implemented by Go types that know their column type, such as
// ant.JSON. It is how a custom type joins migrations without configuration.
type Typed interface {
	ColumnType(dialect string) string
}

// Facets are the sizes configured on a column.
type Facets struct {
	Length    int
	Fixed     bool // Length is exact, not a limit
	Precision int
	Scale     int
}

// Failure is what a driver reports beyond its error message.
type Failure struct {
	Offset int    // where in the script it happened, in characters from 1, or 0
	Detail string // the database's explanation, such as the duplicated value
	Hint   string // the database's suggestion
}

// Querier runs read queries, on a pool, a connection, or a transaction.
type Querier interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}

// Dialect is what a database implements to take part in migrations. A dialect
// that implements dialect.Dialect but not this has no migration support yet.
type Dialect interface {
	Name() string
	Quote(ident string) string
	Placeholder(n int) string

	// MaxIdentifier is the longest allowed name, or 0 for no limit.
	MaxIdentifier() int
	// ColumnType maps a Go type to a column type. A pointer or a nullable
	// wrapper has already been removed.
	ColumnType(t reflect.Type, f Facets) (string, bool)
	// Zero returns the literal that fills a new NOT NULL column in existing
	// rows, or "" if the type has no obvious one.
	Zero(columnType string) string
	// Statements returns the SQL for one change, without terminators.
	Statements(c Change) ([]string, error)
	// EnsureTable returns statements that create t only if it is missing.
	EnsureTable(t *Table) ([]string, error)
	// Split separates a script into statements. It is used for scripts that run
	// outside a transaction, whose statements must be sent one at a time.
	Split(script string) []string
	// Guarded returns a statement that runs the given scripts only if condition
	// is true, for idempotent exports. Each script can hold several statements.
	Guarded(condition string, scripts ...string) string
	// Wipe returns statements that remove everything in the given schemas. An
	// empty name means the default schema.
	Wipe(schemas []string) []string

	// SessionReset returns a statement that undoes session settings a script
	// changed, or "". Connections carry settings past a commit, and a pg_dump file
	// clears search_path, which would break whatever runs next.
	SessionReset() string

	// Inspect reads the tables of the live database in the given schemas. The
	// default schema is always included, and is reported with an empty name.
	Inspect(ctx context.Context, q Querier, schemas []string) (*Schema, error)
	// Canonical returns target as the database itself would store it, by
	// creating it in a throwaway form. It removes differences of spelling, such
	// as int and integer, when comparing with Inspect's result.
	Canonical(ctx context.Context, conn *sql.Conn, target *Schema) (*Schema, error)
	// SameExpression reports whether two stored expressions mean the same, when
	// their text differs. Canonical settles most spelling, but a server can print
	// one expression in two ways, depending on how it got there.
	SameExpression(a, b string) bool
	// Lock takes a lock that lasts until the release function runs, so that only
	// one migrator works at a time. It gives up when ctx ends.
	Lock(
		ctx context.Context,
		conn *sql.Conn,
		name string,
	) (release func(context.Context) error, err error)
	// Explain returns what the driver knows about a failure in a script.
	Explain(err error) Failure
	// IsMissingTable reports whether err says a table does not exist.
	IsMissingTable(err error) bool
	// Database returns the name of the connected database.
	Database(ctx context.Context, q Querier) (string, error)
	// Summary describes the connected database for a person.
	Summary(ctx context.Context, q Querier) (*Summary, error)
}

// ExternalTypes is implemented by a dialect that can tell which types the
// columns of a schema use that neither the schema nor the database itself makes,
// such as a domain, a composite type, or an extension's type. A migration made
// from the schema alone fails on an empty database without them.
type ExternalTypes interface {
	ExternalTypes(ctx context.Context, q Querier, s *Schema) ([]ExternalType, error)
}

// ExternalType is a type that columns use and the schema does not create.
type ExternalType struct {
	// Name is the type as the columns name it, without an array's brackets.
	Name string
	// Kind says what it is: "a domain", "a composite type", or the like.
	Kind string
	// Extension is the extension that makes it, or "".
	Extension string
	// Columns are the columns that use it, as table.column.
	Columns []string
}

// RelationColumns is implemented by a dialect that can read the columns of a
// table or view by name, which drift compares with the views that read-only
// models read.
type RelationColumns interface {
	// RelationColumns returns the columns of the relation ref names, found as a
	// query of it would find it, in their order. ok is false if there is none.
	RelationColumns(ctx context.Context, q Querier, ref Ref) (columns []string, ok bool, err error)
}

// Summary is a short description of a live database.
type Summary struct {
	Version           string
	Name              string
	Size              string
	Tables            []TableSummary
	MaterializedViews []TableSummary
}

// TableSummary is one table or materialized view in a Summary. Rows is the
// database's estimate.
type TableSummary struct {
	Name string
	Rows int64
	Size string
}

// Of returns d's migration support, or ErrUnsupported. It takes any so that this
// package need not import package dialect.
func Of(d any) (Dialect, error) {
	if out, ok := d.(Dialect); ok {
		return out, nil
	}

	if named, ok := d.(interface{ Name() string }); ok {
		return nil, fmt.Errorf("%w: %s", ErrUnsupported, named.Name())
	}

	return nil, ErrUnsupported
}
