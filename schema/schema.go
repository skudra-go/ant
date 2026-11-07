// Package schema describes a database schema as plain data, and works out the
// changes that turn one schema into another. It runs no SQL and needs no
// connection, so a migration can be generated offline.
//
// A Schema comes from models, with ant.SchemaOf, from a stored snapshot, with
// Decode, or from a live database, with Dialect.Inspect. Diff compares two of
// them, and Script writes the result as plain SQL.
package schema

import (
	"bytes"
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
)

// Format is the version of the snapshot file format.
const Format = 1

// ErrNewerFormat reports a snapshot written by a newer version of ant.
var ErrNewerFormat = errors.New("ant: snapshot was written by a newer version of ant")

// Ref names a table.
type Ref struct {
	Schema string `json:"schema,omitempty"` // empty means the connection's default
	Name   string `json:"name"`
}

// String returns the name as schema.table, or table in the default schema.
func (r Ref) String() string {
	if r.Schema == "" {
		return r.Name
	}

	return r.Schema + "." + r.Name
}

// Schema is the full structure of a database.
type Schema struct {
	Format  int    `json:"format"`
	Dialect string `json:"dialect"`
	// Latest is the last migration this schema reflects. Two branches that each
	// add a migration conflict on this line, which surfaces the merge.
	Latest  string   `json:"latest,omitempty"`
	Schemas []string `json:"schemas,omitempty"`
	// Unmanaged tables exist, but migrations leave them alone.
	Unmanaged []Ref       `json:"unmanaged,omitempty"`
	Enums     []*Enum     `json:"enums,omitempty"`
	Sequences []*Sequence `json:"sequences,omitempty"`
	Tables    []*Table    `json:"tables"`
}

// Enum is a native enum type.
type Enum struct {
	Name   string   `json:"name"`
	Values []string `json:"values"`
}

// Sequence is a database sequence. A zero Start or Increment means 1, the
// default.
type Sequence struct {
	Name      string `json:"name"`
	Start     int64  `json:"start,omitempty"`
	Increment int64  `json:"increment,omitempty"`
}

// start returns the sequence's start, 1 when unset.
func (q *Sequence) start() int64 { return cmp.Or(q.Start, 1) }

// increment returns the sequence's increment, 1 when unset.
func (q *Sequence) increment() int64 { return cmp.Or(q.Increment, 1) }

// Table is one table.
type Table struct {
	Schema      string        `json:"schema,omitempty"`
	Name        string        `json:"name"`
	Comment     string        `json:"comment,omitempty"`
	Partition   *Partition    `json:"partition,omitempty"`
	Columns     []*Column     `json:"columns"`
	PrimaryKey  *Key          `json:"primaryKey,omitempty"`
	Uniques     []*Key        `json:"uniques,omitempty"`
	Checks      []*Check      `json:"checks,omitempty"`
	Indexes     []*Index      `json:"indexes,omitempty"`
	ForeignKeys []*ForeignKey `json:"foreignKeys,omitempty"`
}

// Partition says how a table is split into partitions.
type Partition struct {
	Method  string   `json:"method"` // RANGE, LIST, or HASH
	Columns []string `json:"columns"`
}

// Ref returns the table's name.
func (t *Table) Ref() Ref { return Ref{t.Schema, t.Name} }

// Column is one column.
type Column struct {
	Name      string `json:"name"`
	Type      string `json:"type"`
	Nullable  bool   `json:"nullable,omitempty"`
	Default   string `json:"default,omitempty"`   // SQL expression
	Identity  bool   `json:"identity,omitempty"`  // the database generates values
	Generated string `json:"generated,omitempty"` // SQL expression, stored
	Collation string `json:"collation,omitempty"`
	Comment   string `json:"comment,omitempty"`
}

func (c *Column) sameAs(o *Column) bool {
	a, b := *c, *o
	a.Comment, b.Comment = "", ""

	return a == b
}

// Key is a primary key or unique constraint.
type Key struct {
	Name    string   `json:"name"`
	Columns []string `json:"columns"`
}

func (k *Key) sameAs(o *Key) bool { return slices.Equal(k.Columns, o.Columns) }

// Check is a check constraint.
type Check struct {
	Name string `json:"name"`
	Expr string `json:"expr"`
}

func (c *Check) sameAs(o *Check) bool { return c.Expr == o.Expr }

// IndexPart is one indexed column or expression. Nulls is "first" or "last"
// where the part puts NULLs other than where its direction does by default,
// which is last going up and first going down, and empty otherwise.
type IndexPart struct {
	Column string `json:"column,omitempty"`
	Expr   string `json:"expr,omitempty"`
	Desc   bool   `json:"desc,omitempty"`
	Nulls  string `json:"nulls,omitempty"`
}

// NullsOrder returns the Nulls value for a part going down or up whose NULLs
// come first or not: empty where that is the direction's default.
func NullsOrder(desc, first bool) string {
	switch {
	case first && !desc:
		return "first"
	case !first && desc:
		return "last"
	}

	return ""
}

// Index is an index.
type Index struct {
	Name    string      `json:"name"`
	Parts   []IndexPart `json:"parts"`
	Unique  bool        `json:"unique,omitempty"`
	Method  string      `json:"method,omitempty"` // empty means the default
	Where   string      `json:"where,omitempty"`
	Include []string    `json:"include,omitempty"`
	// Concurrent builds the index without blocking writes, on an existing table.
	// It says how to build the index, so changing it alone is not a change.
	Concurrent bool `json:"concurrent,omitempty"`
}

func (i *Index) sameAs(o *Index) bool {
	return slices.Equal(i.Parts, o.Parts) && i.Unique == o.Unique && i.Method == o.Method &&
		i.Where == o.Where && slices.Equal(i.Include, o.Include)
}

// ForeignKey is a foreign key constraint.
type ForeignKey struct {
	Name       string   `json:"name"`
	Columns    []string `json:"columns"`
	RefSchema  string   `json:"refSchema,omitempty"`
	RefTable   string   `json:"refTable"`
	RefColumns []string `json:"refColumns"`
	OnDelete   string   `json:"onDelete,omitempty"` // SQL action; empty means NO ACTION
	OnUpdate   string   `json:"onUpdate,omitempty"`
}

func (f *ForeignKey) sameAs(o *ForeignKey) bool {
	return slices.Equal(f.Columns, o.Columns) && f.RefSchema == o.RefSchema &&
		f.RefTable == o.RefTable &&
		slices.Equal(f.RefColumns, o.RefColumns) &&
		f.OnDelete == o.OnDelete &&
		f.OnUpdate == o.OnUpdate
}

// Table returns the table called ref, or nil.
func (s *Schema) Table(ref Ref) *Table {
	if s == nil {
		return nil
	}

	for _, t := range s.Tables {
		if t.Ref() == ref {
			return t
		}
	}

	return nil
}

// Enum returns the enum called name, or nil.
func (s *Schema) Enum(name string) *Enum {
	for _, e := range s.Enums {
		if e.Name == name {
			return e
		}
	}

	return nil
}

// Sequence returns the sequence called name, or nil.
func (s *Schema) Sequence(name string) *Sequence {
	for _, q := range s.Sequences {
		if q.Name == name {
			return q
		}
	}

	return nil
}

// Column returns the column called name, or nil.
func (t *Table) Column(name string) *Column {
	for _, c := range t.Columns {
		if c.Name == name {
			return c
		}
	}

	return nil
}

// Clone returns a deep copy. A nil schema clones to an empty one.
func (s *Schema) Clone() *Schema {
	if s == nil {
		return &Schema{Format: Format}
	}

	text, err := json.Marshal(s)
	if err != nil {
		panic(err) // the schema types always marshal
	}

	out := new(Schema)
	if err := json.Unmarshal(text, out); err != nil {
		panic(err)
	}

	return out
}

// Sort puts tables and their objects in a stable order, so that the snapshot of
// an unchanged model is byte for byte the same. Columns keep their order.
func (s *Schema) Sort() {
	byRef := func(a, b *Table) int {
		if a.Schema != b.Schema {
			return compare(a.Schema, b.Schema)
		}

		return compare(a.Name, b.Name)
	}
	slices.SortFunc(s.Tables, byRef)
	slices.Sort(s.Schemas)
	s.Schemas = slices.Compact(s.Schemas)
	slices.SortFunc(s.Unmanaged, func(a, b Ref) int { return compare(a.String(), b.String()) })
	slices.SortFunc(s.Enums, func(a, b *Enum) int { return compare(a.Name, b.Name) })
	slices.SortFunc(s.Sequences, func(a, b *Sequence) int { return compare(a.Name, b.Name) })

	for _, t := range s.Tables {
		slices.SortFunc(t.Uniques, func(a, b *Key) int { return compare(a.Name, b.Name) })
		slices.SortFunc(t.Checks, func(a, b *Check) int { return compare(a.Name, b.Name) })
		slices.SortFunc(t.Indexes, func(a, b *Index) int { return compare(a.Name, b.Name) })

		slices.SortFunc(
			t.ForeignKeys,
			func(a, b *ForeignKey) int { return compare(a.Name, b.Name) },
		)
	}
}

func compare(a, b string) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	}

	return 0
}

// Encode returns the snapshot file for s.
func (s *Schema) Encode() ([]byte, error) {
	out := s.Clone()
	out.Format = Format
	out.Sort()

	var b bytes.Buffer

	e := json.NewEncoder(&b)
	e.SetEscapeHTML(false)
	e.SetIndent("", "  ")

	if err := e.Encode(out); err != nil {
		return nil, err
	}

	return b.Bytes(), nil
}

// Decode reads a snapshot file. An empty file is an empty schema.
func Decode(data []byte) (*Schema, error) {
	if len(bytes.TrimSpace(data)) == 0 {
		return &Schema{Format: Format}, nil
	}

	s := new(Schema)
	if err := json.Unmarshal(data, s); err != nil {
		return nil, fmt.Errorf("ant: read snapshot: %w", err)
	}

	if s.Format > Format {
		return nil, fmt.Errorf(
			"%w: format %d, this version reads up to %d",
			ErrNewerFormat,
			s.Format,
			Format,
		)
	}

	return s, nil
}
