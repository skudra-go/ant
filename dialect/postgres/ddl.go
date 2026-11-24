package postgres

import (
	"cmp"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"net"
	"net/netip"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/skudra-go/ant/schema"
)

var _ schema.Dialect = impl{}

// maxIdentifier is PostgreSQL's NAMEDATALEN minus one. Longer names are cut
// silently by the server.
const maxIdentifier = 63

func (impl) MaxIdentifier() int { return maxIdentifier }

// ColumnType maps a Go type to its column type. Strings are text unless a
// length is set, which is the usual advice for PostgreSQL. A precision means
// numeric, for any type. A Go type made for a column type of PostgreSQL's own
// takes that type, and ant reads it back from there (decision 134). A slice of
// a type that maps is an array of it, unless the slice converts itself, as one
// stored as JSON does.
func (d impl) ColumnType(t reflect.Type, f schema.Facets) (string, bool) {
	if element, ok := arrayElement(t); ok {
		out, ok := d.ColumnType(element, f)
		if !ok {
			return "", false
		}

		return out + "[]", true
	}

	if f.Precision > 0 {
		return fmt.Sprintf("numeric(%d,%d)", f.Precision, f.Scale), true
	}

	switch {
	case t == reflect.TypeFor[time.Time]():
		return "timestamptz", true
	case t == reflect.TypeFor[time.Duration]():
		return "interval", true
	case t == reflect.TypeFor[json.RawMessage]():
		return "jsonb", true
	case t == reflect.TypeFor[net.IP](), t == reflect.TypeFor[netip.Addr]():
		return "inet", true
	case t == reflect.TypeFor[netip.Prefix]():
		return "cidr", true
	case t.Kind() == reflect.Map && t.Key().Kind() == reflect.String &&
		!t.Implements(valuerType) && !reflect.PointerTo(t).Implements(scannerType):
		return "jsonb", true // read with json.Number, as ant.JSON is
	case t.PkgPath() == "github.com/shopspring/decimal" && t.Name() == "Decimal":
		return "numeric", true // named, as dialect/postgres imports no driver or library
	case t.Kind() == reflect.Array && t.Len() == 16 && t.Elem().Kind() == reflect.Uint8:
		return "uuid", true // the shape of github.com/google/uuid.UUID
	case t.Kind() == reflect.Slice && t.Elem().Kind() == reflect.Uint8:
		return "bytea", true
	}

	switch t.Kind() {
	case reflect.Bool:
		return "boolean", true
	case reflect.String:
		switch {
		case f.Length > 0 && f.Fixed:
			return fmt.Sprintf("char(%d)", f.Length), true
		case f.Length > 0:
			return fmt.Sprintf("varchar(%d)", f.Length), true
		}

		return "text", true
	case reflect.Int8, reflect.Int16, reflect.Uint8:
		return "smallint", true
	case reflect.Int32, reflect.Uint16:
		return "integer", true
	case reflect.Int, reflect.Int64, reflect.Uint, reflect.Uint32:
		return "bigint", true
	case reflect.Uint64:
		// Its maximum, 18446744073709551615, is past bigint's, and numeric(20,0)
		// holds all twenty digits. A key the database generates stays bigint, as
		// an identity is an integer (decision 138). uint stays bigint above: it
		// is what code uses for ordinary counts and keys.
		return "numeric(20,0)", true
	case reflect.Float32:
		return "real", true
	case reflect.Float64:
		return "double precision", true
	}

	return "", false
}

// arrayElement returns the element type of a slice that is a PostgreSQL array:
// one of anything but bytes, which are bytea, and slices, as a slice of slices
// has no one type, and maps, which pgx would read without json.Number. A pointer element is the type it points to, as pgx reads a
// NULL element into one. A slice that is a Valuer or a Scanner says itself what
// it holds, often JSON, so it is not taken for an array.
func arrayElement(t reflect.Type) (reflect.Type, bool) {
	if t.Kind() != reflect.Slice || t.Elem().Kind() == reflect.Uint8 {
		return nil, false
	}

	if t.Implements(valuerType) || reflect.PointerTo(t).Implements(scannerType) {
		return nil, false
	}

	element := t.Elem()

	for element.Kind() == reflect.Pointer {
		element = element.Elem()
	}

	if element.Kind() == reflect.Slice && element.Elem().Kind() != reflect.Uint8 ||
		element.Kind() == reflect.Map {
		return nil, false
	}

	return element, true
}

var (
	valuerType  = reflect.TypeFor[driver.Valuer]()
	scannerType = reflect.TypeFor[sql.Scanner]()
)

// Zero returns the value a new NOT NULL column gets in existing rows.
func (impl) Zero(columnType string) string {
	t := strings.ToLower(strings.TrimSpace(columnType))

	base, _, _ := strings.Cut(t, "(")

	switch {
	case strings.HasSuffix(t, "[]"):
		return "'{}'"
	case strings.HasPrefix(base, "timestamp"), base == "date":
		return "now()"
	case base == "timetz", strings.HasPrefix(base, "time") && strings.Contains(t, "with time zone"):
		return "'00:00:00+00'"
	case base == "time", strings.HasPrefix(base, "time without"):
		return "'00:00:00'"
	case base == "bit":
		// bit(n) holds exactly n bits, one with no length.
		n, err := strconv.Atoi(strings.TrimSuffix(strings.TrimPrefix(t[len(base):], "("), ")"))
		if err != nil || n < 1 {
			n = 1
		}

		return "B'" + strings.Repeat("0", n) + "'"
	}

	switch strings.TrimSpace(base) {
	case "smallint", "integer", "int", "bigint", "numeric", "decimal", "real", "double precision",
		"money":
		return "0"
	case "text", "varchar", "character varying", "char", "character":
		return "''"
	case "boolean", "bool":
		return "false"
	case "json", "jsonb":
		return "'null'"
	case "bytea":
		return "''"
	case "uuid":
		return "gen_random_uuid()"
	case "interval":
		return "'0'"
	case "inet":
		return "'0.0.0.0'"
	case "cidr":
		return "'0.0.0.0/0'"
	case "macaddr":
		return "'00:00:00:00:00:00'"
	case "varbit", "bit varying":
		return "B''"
	}

	return ""
}

func (d impl) table(ref schema.Ref) string {
	if ref.Schema == "" {
		return d.Quote(ref.Name)
	}

	return d.Quote(ref.Schema) + "." + d.Quote(ref.Name)
}

func (d impl) names(columns []string) string {
	quoted := make([]string, len(columns))

	for i, c := range columns {
		quoted[i] = d.Quote(c)
	}

	return strings.Join(quoted, ", ")
}

func (d impl) column(c *schema.Column) string {
	var b strings.Builder
	b.WriteString(d.Quote(c.Name))
	b.WriteString(" ")
	b.WriteString(c.Type)

	if c.Collation != "" {
		b.WriteString(" COLLATE ")
		b.WriteString(d.Quote(c.Collation))
	}

	if c.Identity {
		b.WriteString(" GENERATED BY DEFAULT AS IDENTITY")

		return b.String()
	}

	if !c.Nullable {
		b.WriteString(" NOT NULL")
	}

	switch {
	case c.Generated != "":
		b.WriteString(" GENERATED ALWAYS AS (")
		b.WriteString(c.Generated)
		b.WriteString(") STORED")
	case c.Default != "":
		b.WriteString(" DEFAULT ")
		b.WriteString(c.Default)
	}

	return b.String()
}

func (d impl) createTable(t *schema.Table, ifMissing bool) string {
	var lines []string

	for _, c := range t.Columns {
		lines = append(lines, d.column(c))
	}

	if k := t.PrimaryKey; k != nil {
		lines = append(
			lines,
			fmt.Sprintf("CONSTRAINT %s PRIMARY KEY (%s)", d.Quote(k.Name), d.names(k.Columns)),
		)
	}

	for _, k := range t.Uniques {
		lines = append(
			lines,
			fmt.Sprintf("CONSTRAINT %s UNIQUE (%s)", d.Quote(k.Name), d.names(k.Columns)),
		)
	}

	for _, c := range t.Checks {
		lines = append(lines, fmt.Sprintf("CONSTRAINT %s CHECK (%s)", d.Quote(c.Name), c.Expr))
	}

	create := "CREATE TABLE "

	if ifMissing {
		create += "IF NOT EXISTS "
	}

	out := create + d.table(t.Ref()) + " (\n    " + strings.Join(lines, ",\n    ") + "\n)"

	if p := t.Partition; p != nil {
		out += " PARTITION BY " + p.Method + " (" + d.names(p.Columns) + ")"
	}

	return out
}

func (d impl) createIndex(ref schema.Ref, i *schema.Index, online bool) string {
	var b strings.Builder
	b.WriteString("CREATE ")

	if i.Unique {
		b.WriteString("UNIQUE ")
	}

	b.WriteString("INDEX ")

	if online {
		b.WriteString("CONCURRENTLY ")
	}

	b.WriteString(d.Quote(i.Name))
	b.WriteString(" ON ")
	b.WriteString(d.table(ref))

	if i.Method != "" {
		b.WriteString(" USING ")
		b.WriteString(i.Method)
	}

	parts := make([]string, len(i.Parts))

	for n, p := range i.Parts {
		parts[n] = d.Quote(p.Column)

		if p.Expr != "" {
			parts[n] = "(" + p.Expr + ")"
		}

		if p.Desc {
			parts[n] += " DESC"
		}

		if p.Nulls != "" {
			parts[n] += " NULLS " + strings.ToUpper(p.Nulls)
		}
	}

	b.WriteString(" (")
	b.WriteString(strings.Join(parts, ", "))
	b.WriteByte(')')

	if len(i.Include) > 0 {
		b.WriteString(" INCLUDE (")
		b.WriteString(d.names(i.Include))
		b.WriteByte(')')
	}

	if i.Where != "" {
		b.WriteString(" WHERE ")
		b.WriteString(i.Where)
	}

	return b.String()
}

// indexRef returns an index's name, in its table's schema.
func (d impl) indexRef(table schema.Ref, name string) string {
	return d.table(schema.Ref{Schema: table.Schema, Name: name})
}

func (d impl) foreignKey(ref schema.Ref, k *schema.ForeignKey) string {
	out := fmt.Sprintf(
		"ALTER TABLE %s ADD CONSTRAINT %s FOREIGN KEY (%s) REFERENCES %s (%s)",
		d.table(ref),
		d.Quote(k.Name),
		d.names(k.Columns),
		d.table(schema.Ref{Schema: k.RefSchema, Name: k.RefTable}),
		d.names(k.RefColumns),
	)

	if k.OnDelete != "" {
		out += " ON DELETE " + k.OnDelete
	}

	if k.OnUpdate != "" {
		out += " ON UPDATE " + k.OnUpdate
	}

	return out
}

func (d impl) alterColumn(c schema.AlterColumn) []string {
	alter := "ALTER TABLE " + d.table(c.Table) + " ALTER COLUMN " + d.Quote(c.To.Name)
	// SET EXPRESSION needs PostgreSQL 17, so the column is replaced, and the plan
	// rebuilds what goes with it.
	if c.Replaces() {
		return []string{
			"ALTER TABLE " + d.table(c.Table) + " DROP COLUMN " + d.Quote(c.From.Name),
			"ALTER TABLE " + d.table(c.Table) + " ADD COLUMN " + d.column(c.To),
		}
	}

	var out []string

	retyped := c.From.Type != c.To.Type

	if c.From.Generated != c.To.Generated {
		out = append(out, alter+" DROP EXPRESSION")
	}

	// An identity column takes no default, so it goes first.
	if c.From.Identity && !c.To.Identity {
		out = append(out, alter+" DROP IDENTITY")
	}

	// A default is of the column's old type, which it may not cast to the new
	// one, and its text keeps the old type's cast, so it goes, and comes back
	// after.
	if retyped && c.From.Default != "" {
		out = append(out, alter+" DROP DEFAULT")
	}

	if retyped || c.From.Collation != c.To.Collation {
		statement := alter + " TYPE " + c.To.Type

		if c.To.Collation != "" {
			statement += " COLLATE " + d.Quote(c.To.Collation)
		}

		// A column of a type ant gave an earlier default is converted as ant
		// stored it, which no cast does. An enum casts to another type only
		// through text.
		if using := converted(c.From.Type, c.To.Type, d.Quote(c.To.Name)); retyped && using != "" {
			statement += " USING " + using
		} else if retyped && needsUsing(c.From.Type, c.To.Type) {
			via := ""

			if c.FromEnum {
				via = "::text"
			}

			statement += " USING " + d.Quote(c.To.Name) + via + "::" + c.To.Type
		}

		out = append(out, statement)
	}

	switch {
	case retyped && c.To.Default != "":
		out = append(out, alter+" SET DEFAULT "+c.To.Default)
	case retyped, c.From.Default == c.To.Default:
	case c.To.Default == "":
		out = append(out, alter+" DROP DEFAULT")
	default:
		out = append(out, alter+" SET DEFAULT "+c.To.Default)
	}

	if c.From.Nullable != c.To.Nullable {
		if c.To.Nullable {
			out = append(out, alter+" DROP NOT NULL")
		} else {
			out = append(out, alter+" SET NOT NULL")
		}
	}

	// Identity needs NOT NULL, and no default, both set above. Its sequence
	// starts at 1, so it moves past the values the column already holds, or the
	// next insert would collide with them. pg_get_serial_sequence parses its first
	// argument as SQL, quotes and all, and takes the column's name as it is.
	if !c.From.Identity && c.To.Identity {
		table := d.table(c.Table)
		out = append(
			out,
			alter+" ADD GENERATED BY DEFAULT AS IDENTITY",
			"SELECT setval(pg_get_serial_sequence("+literal(table)+", "+literal(c.To.Name)+"), "+
				"GREATEST(max("+d.Quote(c.To.Name)+"), 0) + 1, false) FROM "+table,
		)
	}

	return out
}

// converted returns the USING expression that changes column, of type from, to
// type to, where PostgreSQL has no cast and ant knows how it stored the values:
// a time.Duration was a bigint of nanoseconds, a net.IP the 4 or 16 bytes of a
// bytea, and a json.RawMessage its text in a bytea, until ant mapped them to
// interval, inet and jsonb. It returns "" for any other change. An interval
// keeps microseconds, so the nanoseconds below one go, toward zero, as they do
// when pgx writes a time.Duration. A bytea that is no address becomes NULL.
//
// A boolean casts to and from integer only, so it becomes another number through
// integer, as 1 or 0, and a number becomes a boolean as true when it is not
// zero, with no integer between to overflow or round 0.4 to false. NULL stays
// NULL.
func converted(from, to, column string) string {
	switch from, to = baseType(from), baseType(to); {
	case boolean(from) && numeric(to):
		return column + "::integer::" + to
	case numeric(from) && boolean(to):
		return "(" + column + " <> 0)"
	case from == "bigint" && to == "interval":
		return "(" + column + " / 1000) * interval '1 microsecond'"
	case from == "bytea" && to == "inet":
		bytes := make([]string, 4)

		for i := range bytes {
			bytes[i] = "get_byte(" + column + ", " + strconv.Itoa(i) + ")"
		}

		return "CASE length(" + column + ")" +
			" WHEN 4 THEN array_to_string(ARRAY[" + strings.Join(bytes, ", ") + "], '.')" +
			" WHEN 16 THEN regexp_replace(encode(" + column + ", 'hex'), '(.{4})(?=.)', '\\1:', 'g')" +
			" END::inet"
	case from == "bytea" && (to == "jsonb" || to == "json"):
		return "convert_from(" + column + ", 'UTF8')::" + to
	}

	return ""
}

// numeric reports whether a base type is one of PostgreSQL's number types.
func numeric(name string) bool {
	switch name {
	case "smallint", "integer", "bigint", "numeric", "decimal", "real", "double precision",
		"int", "int2", "int4", "int8", "float4", "float8":
		return true
	}

	return false
}

// boolean reports whether a base type is PostgreSQL's boolean.
func boolean(name string) bool { return name == "boolean" || name == "bool" }

// needsUsing reports whether a column changing type from one to the other
// needs a USING cast. An explicit cast cuts a string or a bit string to a
// shorter length without a word, where the cast PostgreSQL applies by itself
// fails on a value that does not fit, so USING is left out wherever that cast
// exists: into a string type from any type, as every type has one to text, and
// between two forms of one type that differ only in length, precision or scale.
// An array takes its element's cast, and anything else keeps USING.
func needsUsing(from, to string) bool {
	from, to = baseType(from), baseType(to)

	fromElement, fromArray := strings.CutSuffix(from, "[]")
	toElement, toArray := strings.CutSuffix(to, "[]")

	if fromArray != toArray {
		return true
	}

	switch toElement {
	case "text", "varchar", "character varying", "char", "character", "bpchar":
		return false
	}

	return fromElement != toElement
}

// baseType returns a type's name without its length, precision or scale, in
// lower case, as numeric for numeric(10,2) and timestamp with time zone for
// timestamp(3) with time zone.
func baseType(name string) string {
	var b strings.Builder

	depth := 0

	for _, r := range strings.ToLower(name) {
		switch {
		case r == '(':
			depth++
		case r == ')':
			depth--
		case depth == 0:
			b.WriteRune(r)
		}
	}

	return strings.Join(strings.Fields(b.String()), " ")
}

// Statements returns the SQL for one change.
func (d impl) Statements(change schema.Change) ([]string, error) {
	alter := func(ref schema.Ref, rest string) []string {
		return []string{"ALTER TABLE " + d.table(ref) + " " + rest}
	}

	switch c := change.(type) {
	case schema.CreateSchema:
		return []string{"CREATE SCHEMA IF NOT EXISTS " + d.Quote(c.Name)}, nil
	case schema.CreateTable:
		return []string{d.createTable(c.Table, false)}, nil
	case schema.DropTable:
		out := "DROP TABLE "

		if c.IfExists {
			out += "IF EXISTS "
		}

		out += d.table(c.Table)

		if c.Cascade {
			out += " CASCADE"
		}

		return []string{out}, nil
	case schema.RenameTable:
		return alter(c.Table, "RENAME TO "+d.Quote(c.To)), nil
	case schema.MoveTable:
		out := alter(c.Table, "SET SCHEMA "+d.Quote(c.Schema))

		if c.To != "" && c.To != c.Table.Name {
			out = append(
				out,
				alter(
					schema.Ref{Schema: c.Schema, Name: c.Table.Name},
					"RENAME TO "+d.Quote(c.To),
				)...)
		}

		return out, nil
	case schema.AddColumn:
		if c.References != nil {
			// Added NULL, for the rows to be filled, then made NOT NULL: see
			// the change.
			open := *c.Column
			open.Nullable = true

			return append(alter(c.Table, "ADD COLUMN "+d.column(&open)),
				alter(c.Table, "ALTER COLUMN "+d.Quote(c.Column.Name)+" SET NOT NULL")...), nil
		}

		if c.Backfill == "" {
			return alter(c.Table, "ADD COLUMN "+d.column(c.Column)), nil
		}
		// The default fills existing rows, then goes, so the column ends up as
		// the model declares it.
		return append(alter(c.Table, "ADD COLUMN "+d.column(c.Column)+" DEFAULT "+c.Backfill),
			alter(c.Table, "ALTER COLUMN "+d.Quote(c.Column.Name)+" DROP DEFAULT")...), nil
	case schema.DropColumn:
		return alter(c.Table, "DROP COLUMN "+d.Quote(c.Column.Name)), nil
	case schema.RenameColumn:
		return alter(c.Table, "RENAME COLUMN "+d.Quote(c.From)+" TO "+d.Quote(c.To)), nil
	case schema.AlterColumn:
		return d.alterColumn(c), nil
	case schema.AddPrimaryKey:
		return alter(
			c.Table,
			fmt.Sprintf(
				"ADD CONSTRAINT %s PRIMARY KEY (%s)",
				d.Quote(c.Key.Name),
				d.names(c.Key.Columns),
			),
		), nil
	case schema.DropPrimaryKey:
		return alter(c.Table, "DROP CONSTRAINT "+d.Quote(c.Key.Name)), nil
	case schema.AddUnique:
		return alter(
			c.Table,
			fmt.Sprintf(
				"ADD CONSTRAINT %s UNIQUE (%s)",
				d.Quote(c.Key.Name),
				d.names(c.Key.Columns),
			),
		), nil
	case schema.AddCheck:
		return alter(
			c.Table,
			fmt.Sprintf("ADD CONSTRAINT %s CHECK (%s)", d.Quote(c.Check.Name), c.Check.Expr),
		), nil
	case schema.DropConstraint:
		return alter(c.Table, "DROP CONSTRAINT "+d.Quote(c.Name)), nil
	case schema.RenameConstraint:
		return alter(
			c.Table,
			fmt.Sprintf("RENAME CONSTRAINT %s TO %s", d.Quote(c.From), d.Quote(c.To)),
		), nil
	case schema.CreateIndex:
		create := d.createIndex(c.Table, c.Index, c.RunsOnline())

		// A concurrent build that fails leaves an invalid index of the name, and
		// running the file again then failed with "already exists" (#97).
		if c.RunsOnline() {
			return []string{
				"DROP INDEX CONCURRENTLY IF EXISTS " + d.indexRef(c.Table, c.Index.Name),
				create,
			}, nil
		}

		return []string{create}, nil
	case schema.DropIndex:
		out := "DROP INDEX "

		if c.RunsOnline() {
			out += "CONCURRENTLY "
		}

		return []string{out + d.indexRef(c.Table, c.Index.Name)}, nil
	case schema.RenameIndex:
		return []string{
			"ALTER INDEX " + d.indexRef(c.Table, c.From) + " RENAME TO " + d.Quote(c.To),
		}, nil
	case schema.AddForeignKey:
		return []string{d.foreignKey(c.Table, c.Key)}, nil
	case schema.DropForeignKey:
		return alter(c.Table, "DROP CONSTRAINT "+d.Quote(c.Key.Name)), nil
	case schema.CreateEnum:
		values := make([]string, len(c.Enum.Values))

		for i, value := range c.Enum.Values {
			values[i] = literal(value)
		}

		return []string{
			"CREATE TYPE " + d.Quote(c.Enum.Name) + " AS ENUM (" + strings.Join(values, ", ") + ")",
		}, nil
	case schema.AddEnumValue:
		out := "ALTER TYPE " + d.Quote(c.Enum) + " ADD VALUE IF NOT EXISTS " + literal(c.Value)

		if c.Before != "" {
			out += " BEFORE " + literal(c.Before)
		}

		return []string{out}, nil
	case schema.RemoveEnumValue, schema.ReorderEnum, schema.Repartition:
		return nil, nil // only a warning: see the change
	case schema.AlterSequence:
		out := "ALTER SEQUENCE " + d.Quote(c.To.Name)

		// Zero means the default, 1.
		if cmp.Or(c.From.Increment, 1) != cmp.Or(c.To.Increment, 1) {
			out += fmt.Sprintf(" INCREMENT BY %d", cmp.Or(c.To.Increment, 1))
		}

		if cmp.Or(c.From.Start, 1) != cmp.Or(c.To.Start, 1) {
			out += fmt.Sprintf(" START WITH %d", cmp.Or(c.To.Start, 1))
		}

		return []string{out}, nil
	case schema.DropEnum:
		return []string{"DROP TYPE " + d.Quote(c.Enum)}, nil
	case schema.CreateSequence:
		out := "CREATE SEQUENCE " + d.Quote(c.Sequence.Name)

		if c.Sequence.Increment != 0 && c.Sequence.Increment != 1 {
			out += fmt.Sprintf(" INCREMENT BY %d", c.Sequence.Increment)
		}

		if c.Sequence.Start != 0 && c.Sequence.Start != 1 {
			out += fmt.Sprintf(" START WITH %d", c.Sequence.Start)
		}

		return []string{out}, nil
	case schema.DropSequence:
		return []string{"DROP SEQUENCE " + d.Quote(c.Sequence)}, nil
	case schema.SetTableComment:
		return []string{
			"COMMENT ON TABLE " + d.table(c.Table) + " IS " + commentText(c.Comment),
		}, nil
	case schema.SetColumnComment:
		return []string{
			"COMMENT ON COLUMN " + d.table(
				c.Table,
			) + "." + d.Quote(
				c.Column,
			) + " IS " + commentText(
				c.Comment,
			),
		}, nil
	}

	return nil, fmt.Errorf("postgres: no SQL for change %T", change)
}

func literal(text string) string { return "'" + strings.ReplaceAll(text, "'", "''") + "'" }

func commentText(text string) string {
	if text == "" {
		return "NULL"
	}

	return literal(text)
}

// EnsureTable returns statements that create t only if it is missing.
func (d impl) EnsureTable(t *schema.Table) ([]string, error) {
	var out []string

	if t.Schema != "" {
		out = append(out, "CREATE SCHEMA IF NOT EXISTS "+d.Quote(t.Schema))
	}

	return append(out, d.createTable(t, true)), nil
}

// Guarded returns a DO block that runs scripts if condition holds. EXECUTE takes
// a whole script, and a DO block is one transaction.
func (impl) Guarded(condition string, scripts ...string) string {
	var b strings.Builder
	b.WriteString("DO $ant$\nBEGIN\n  IF ")
	b.WriteString(condition)
	b.WriteString(" THEN\n")

	for _, script := range scripts {
		tag := "$ant_sql$"
		for n := 1; strings.Contains(script, tag); n++ {
			tag = fmt.Sprintf("$ant_sql_%d$", n)
		}

		b.WriteString("    EXECUTE ")
		b.WriteString(tag)
		b.WriteByte('\n')
		b.WriteString(script)
		b.WriteByte('\n')
		b.WriteString(tag)
		b.WriteString(";\n")
	}

	b.WriteString("  END IF;\nEND\n$ant$")

	return b.String()
}

// Wipe drops and recreates schemas, which removes every object in them,
// including those no model knows about.
func (d impl) Wipe(schemas []string) []string {
	var out []string

	for _, name := range schemas {
		if name != "" {
			out = append(
				out,
				"DROP SCHEMA IF EXISTS "+d.Quote(name)+" CASCADE",
				"CREATE SCHEMA "+d.Quote(name),
			)

			continue
		}

		out = append(out, `DO $ant$
DECLARE
  name text := current_schema();
BEGIN
  EXECUTE format('DROP SCHEMA %I CASCADE', name);
  EXECUTE format('CREATE SCHEMA %I', name);
END
$ant$`)
	}

	return out
}
