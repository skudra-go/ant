package ant

import (
	"cmp"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/netip"
	"reflect"
	"slices"
	"strings"
	"time"

	"github.com/skudra-go/ant/dialect"
	"github.com/skudra-go/ant/internal/exec"
	"github.com/skudra-go/ant/internal/liveschema"
	"github.com/skudra-go/ant/internal/meta"
	"github.com/skudra-go/ant/schema"
)

// modelTable is implemented by every Table instantiation.
type modelTable interface {
	tableModel() *meta.Model
}

func (t *Table[T]) tableModel() *meta.Model { return configure[T]() }

// SchemaOf returns the schema DB's models describe: its Table fields, and models
// reached through their relations. It needs no connection. Views and models
// excluded from migrations have no table in the result. Options are those the
// application gives Open: WithNaming and WithConvention name the models, and a
// program that needs their schema before it opens a context, such as the
// migrate commands, passes them here.
func SchemaOf[DB any](d dialect.Dialect, options ...Option) (*schema.Schema, error) {
	if _, ok := any(new(DB)).(Handle); !ok {
		return nil, noContext[DB]()
	}

	unused := defaultSettings()

	for _, option := range options {
		option(&unused)
	}

	ddl, err := schema.Of(d)
	if err != nil {
		return nil, err
	}

	return buildSchema(ddl, new(DB))
}

// TableOf returns the database schema and table of model T, as configured.
func TableOf[T any]() (schemaName, table string, err error) {
	model := configure[T]()

	return model.Table.Schema, model.Table.Name, model.Err()
}

// models returns the models of a context struct, followed by those its relations
// reach, each configured.
func models(db any) ([]*meta.Model, error) {
	var out []*meta.Model

	add := func(m *meta.Model) error {
		if slices.Contains(out, m) {
			return nil
		}

		configureModel(m)
		out = append(out, m)

		return m.Err()
	}

	err := eachTable(db, func(table any) error {
		return add(table.(modelTable).tableModel())
	})
	if err != nil {
		return nil, err
	}

	for i := 0; i < len(out); i++ {
		for _, r := range out[i].Relations {
			if err := add(meta.OfType(r.Target)); err != nil {
				return nil, err
			}
		}
	}

	return out, sameTable(out)
}

// checkModels checks every model of a context struct and every model its
// relations reach, and resolves each relation, so that a model ant can't use
// fails when the context opens rather than on first use, as the docs promise.
// A relation that found no foreign key, or a join table with no name, passed
// Open and failed the first request that touched it (#203). It reports every
// problem it finds, not only the first, so a large model set is fixed in one go.
func checkModels(db any, d dialect.Dialect) error {
	var (
		seen     []*meta.Model
		problems []error
	)

	add := func(m *meta.Model) {
		if slices.Contains(seen, m) {
			return
		}

		configureModel(m)
		seen = append(seen, m)

		if err := m.Err(); err != nil {
			problems = append(problems, err)
		}
	}

	if err := eachTable(db, func(table any) error {
		add(table.(modelTable).tableModel())

		return nil
	}); err != nil {
		problems = append(problems, err)
	}

	tables := slices.Clone(seen)

	for i := 0; i < len(seen); i++ {
		if seen[i].Err() != nil {
			continue
		}

		for _, r := range seen[i].Relations {
			target := meta.OfType(r.Target)
			add(target)

			if target.Err() != nil {
				continue
			}

			if _, err := linkFor(r); err != nil {
				problems = append(problems, err)
			}
		}
	}

	if err := sameTable(tables); err != nil {
		problems = append(problems, err)
	}

	for _, m := range seen {
		if m.Err() == nil {
			problems = append(problems, plainValues(m, d))
		}
	}

	return errors.Join(problems...)
}

// plainValues refuses the fields of m that database/sql can't write or read, on
// a dialect whose driver converts nothing more, so that the mistake shows at
// Open with the field named, not at the first write with the driver's message,
// which names none (#300). A field only read needs only to be read, and one only
// written, to be written.
func plainValues(m *meta.Model, d dialect.Dialect) error {
	if d == nil || !dialect.SyntaxOf(d).PlainValues {
		return nil
	}

	var problems []error

	for _, f := range m.Fields {
		writes, reads := f.Plain()
		writes = writes || m.ReadOnly || f.Computed || f.ReadOnly
		reads = reads || f.WriteOnly

		var cannot string

		switch {
		case !writes && !reads:
			cannot = "write or read"
		case !writes:
			cannot = "write"
		case !reads:
			cannot = "read"
		default:
			continue
		}

		problems = append(problems, fmt.Errorf(
			"%w: %s.%s has Go type %s, which the %s driver can't %s: implement "+
				"driver.Valuer and sql.Scanner on it, store it as JSON with ant.JSON[%s], "+
				"or leave it out with ant:\"-\"",
			ErrNoColumnType,
			m.Type.Name(),
			f.Name,
			f.Type,
			d.Name(),
			cannot,
			f.Type,
		))
	}

	return errors.Join(problems...)
}

var (
	tableType   = reflect.TypeFor[tableBinder]()
	contextType = reflect.TypeFor[Context]()
)

// eachTable calls visit with a pointer to each Table field of the context struct
// db points at, including those in its embedded and nested structs, which
// group a domain's tables. A Table that ant cannot reach, in an unexported
// field, is an error: it would be left unbound, and fail on first use.
func eachTable(db any, visit func(table any) error) error {
	return walkTables(reflect.ValueOf(db).Elem(), "", true, visit)
}

func walkTables(v reflect.Value, path string, reachable bool, visit func(any) error) error {
	for i := range v.NumField() {
		field, f := v.Type().Field(i), v.Field(i)
		name := path + field.Name

		switch {
		case field.Type.Kind() == reflect.Pointer && field.Type.Implements(tableType):
			return fmt.Errorf(
				"%w: table %s is a pointer, %s, which ant does not bind: declare it as %s",
				ErrBadSchema,
				name,
				field.Type,
				field.Type.Elem(),
			)
		case reflect.PointerTo(field.Type).Implements(tableType):
			if !reachable || !field.IsExported() {
				return fmt.Errorf(
					"%w: table %s is unexported, so ant cannot bind it",
					ErrBadSchema,
					name,
				)
			}

			if err := visit(f.Addr().Interface()); err != nil {
				return err
			}
		case field.Type.Kind() == reflect.Struct && field.Type != contextType:
			err := walkTables(f, name+".", reachable && field.IsExported(), visit)
			if err != nil {
				return err
			}
		}
	}

	return nil
}

// sameTable refuses two models that reach one table by convention alone, as two
// types of the same name in different packages do: each would read the other's
// rows. A table shared on purpose is named with b.Table, or split by
// b.Discriminator.
func sameTable(list []*meta.Model) error {
	seen := map[[2]string]*meta.Model{}

	for _, m := range list {
		if m.Named {
			continue
		}

		at := [2]string{m.Table.Schema, m.Table.Name}

		other, ok := seen[at]
		if !ok {
			seen[at] = m

			continue
		}

		if other == m || other.Discriminator != nil && m.Discriminator != nil {
			continue
		}

		return fmt.Errorf(
			"ant: models %s and %s both map to table %q by convention: "+
				"name one of the tables with b.Table, or share the table with b.Discriminator",
			other.Type,
			m.Type,
			m.Table.Name,
		)
	}

	return nil
}

// schemaBuilder turns configured models into a schema.
type schemaBuilder struct {
	d     schema.Dialect
	out   *schema.Schema
	owner map[*meta.Model]*schema.Table
	keys  map[string]*foreignKey
}

// foreignKey is a foreign key seen from either side of a relation.
type foreignKey struct {
	child, parent *meta.Model
	column, ref   *meta.Field
	name          string
	onDelete      meta.Action
	onUpdate      meta.Action
	skip, noIndex bool
}

func buildSchema(d schema.Dialect, db any) (*schema.Schema, error) {
	list, err := models(db)
	if err != nil {
		return nil, err
	}

	b := &schemaBuilder{
		d:     d,
		out:   &schema.Schema{Format: schema.Format, Dialect: d.Name()},
		owner: map[*meta.Model]*schema.Table{},
		keys:  map[string]*foreignKey{},
	}

	for _, m := range list {
		switch {
		case m.Unmanaged:
			b.out.Unmanaged = append(
				b.out.Unmanaged,
				schema.Ref{Schema: m.Table.Schema, Name: m.Table.Name},
			)
		case m.ReadOnly:
			b.view(m)
		default:
			if err := b.table(m); err != nil {
				return nil, err
			}
		}
	}

	for _, m := range list {
		if err := b.relations(m); err != nil {
			return nil, err
		}
	}

	if err := b.foreignKeys(); err != nil {
		return nil, err
	}

	if err := b.constraints(list); err != nil {
		return nil, err
	}

	if err := distinctNames(b.out); err != nil {
		return nil, err
	}

	for _, t := range b.out.Tables {
		if t.Schema != "" {
			b.out.Schemas = append(b.out.Schemas, t.Schema)
		}
	}

	b.out.Sort()

	return b.out, nil
}

// view records the columns a read-only model reads from its view, which drift
// checks are there (#232). Two models may read one view.
func (b *schemaBuilder) view(m *meta.Model) {
	ref := schema.Ref{Schema: m.Table.Schema, Name: m.Table.Name}
	i := slices.IndexFunc(b.out.Views, func(v *schema.View) bool { return v.Ref() == ref })

	if i < 0 {
		i = len(b.out.Views)
		b.out.Views = append(b.out.Views, &schema.View{Schema: ref.Schema, Name: ref.Name})
	}

	v := b.out.Views[i]

	for _, f := range m.Stored() {
		if !slices.Contains(v.Columns, f.Column) {
			v.Columns = append(v.Columns, f.Column)
		}
	}
}

func (b *schemaBuilder) table(m *meta.Model) error {
	t := &schema.Table{
		Schema:    m.Table.Schema,
		Name:      m.Table.Name,
		Comment:   m.Comment,
		Partition: partitionOf(m),
	}

	for _, f := range m.Fields {
		if f.Computed {
			continue // filled by queries, with no column behind it
		}

		c, err := b.column(m, f)
		if err != nil {
			return err
		}

		t.Columns = append(t.Columns, c)

		if e := f.Storage.Enum; e != nil && b.out.Enum(e.Name) == nil {
			b.out.Enums = append(b.out.Enums, &schema.Enum{Name: e.Name, Values: e.Values})
		}

		if q := f.Storage.Sequence; q != nil && b.out.Sequence(q.Name) == nil {
			b.out.Sequences = append(
				b.out.Sequences,
				&schema.Sequence{Name: q.Name, Start: q.Start, Increment: q.Increment},
			)
		}

		if f.Unique {
			name := f.Storage.UniqueName
			if name == "" {
				name = schema.UniqueName(b.d, t.Name, f.Column)
			}

			t.Uniques = append(t.Uniques, &schema.Key{Name: name, Columns: []string{f.Column}})
		}
	}

	if len(m.Keys) > 0 {
		key := &schema.Key{Name: m.KeyName}
		if key.Name == "" {
			key.Name = schema.PrimaryKeyName(b.d, t.Name)
		}

		for _, f := range m.Keys {
			key.Columns = append(key.Columns, f.Column)
		}

		t.PrimaryKey = key
	}

	for _, check := range m.Checks {
		t.Checks = append(t.Checks, &schema.Check{Name: check.Name, Expr: check.Expr})
	}

	for _, index := range m.Indexes {
		converted, err := b.index(t, index)
		if err != nil {
			return err
		}

		t.Indexes = append(t.Indexes, converted)
	}

	if existing := b.out.Table(t.Ref()); existing != nil {
		b.owner[m] = existing

		return b.mergeTable(existing, t)
	}

	b.owner[m] = t
	b.out.Tables = append(b.out.Tables, t)

	return nil
}

func partitionOf(m *meta.Model) *schema.Partition {
	p := m.Partition
	if p == nil {
		return nil
	}

	out := &schema.Partition{Method: p.Method}

	for _, f := range p.Columns {
		out.Columns = append(out.Columns, f.Column)
	}

	return out
}

// mergeTable combines models on one table, which is how table splitting and
// single-table inheritance look to a database. A row of one model has nothing for
// a column only another declares, so such a column allows NULL.
func (b *schemaBuilder) mergeTable(into, t *schema.Table) error {
	for _, c := range t.Columns {
		existing := into.Column(c.Name)
		if existing == nil {
			c.Nullable = true
			into.Columns = append(into.Columns, c)

			continue
		}

		if existing.Type != c.Type {
			return fmt.Errorf(
				"%w: models on %s disagree about column %s: %s and %s",
				ErrBadSchema,
				into.Name,
				c.Name,
				existing.Type,
				c.Type,
			)
		}
	}

	for _, c := range into.Columns {
		if t.Column(c.Name) == nil {
			c.Nullable = true
		}
	}

	for _, k := range t.Uniques {
		if !slices.ContainsFunc(
			into.Uniques,
			func(o *schema.Key) bool { return o.Name == k.Name },
		) {
			into.Uniques = append(into.Uniques, k)
		}
	}

	for _, c := range t.Checks {
		if !slices.ContainsFunc(
			into.Checks,
			func(o *schema.Check) bool { return o.Name == c.Name },
		) {
			into.Checks = append(into.Checks, c)
		}
	}

	for _, i := range t.Indexes {
		if !slices.ContainsFunc(
			into.Indexes,
			func(o *schema.Index) bool { return o.Name == i.Name },
		) {
			into.Indexes = append(into.Indexes, i)
		}
	}

	if into.PrimaryKey == nil {
		into.PrimaryKey = t.PrimaryKey
	}

	return nil
}

func (b *schemaBuilder) index(t *schema.Table, index *meta.Index) (*schema.Index, error) {
	out := &schema.Index{
		Name:       index.Name,
		Unique:     index.Unique,
		Method:     index.Method,
		Where:      index.Where,
		Concurrent: index.Concurrent,
	}

	if len(index.Parts) == 0 {
		return nil, fmt.Errorf("%w: index on %s has no columns", ErrBadSchema, t.Name)
	}

	var names []string

	for _, part := range index.Parts {
		nulls := ""

		if part.NullsFirst != nil {
			nulls = schema.NullsOrder(part.Desc, *part.NullsFirst)
		}

		if part.Field != nil {
			out.Parts = append(
				out.Parts,
				schema.IndexPart{Column: part.Field.Column, Desc: part.Desc, Nulls: nulls},
			)

			names = append(names, part.Field.Column)

			continue
		}

		out.Parts = append(
			out.Parts,
			schema.IndexPart{Expr: part.Expr, Desc: part.Desc, Nulls: nulls},
		)
	}

	for _, f := range index.Include {
		out.Include = append(out.Include, f.Column)
	}

	switch {
	case out.Name != "":
	case len(names) < len(index.Parts):
		return nil, fmt.Errorf(
			"%w: index on the expression %q of %s needs a name: add .Name(...)",
			ErrBadSchema,
			index.Parts[0].Expr,
			t.Name,
		)
	default:
		out.Name = schema.IndexName(b.d, t.Name, names...)
	}

	return out, nil
}

// column declares one field. Whether it can hold NULL follows the Go type, so a
// model says it in one place.
func (b *schemaBuilder) column(m *meta.Model, f *meta.Field) (*schema.Column, error) {
	valueType, _ := columnValue(f.Type)

	storage := f.Storage

	c := &schema.Column{
		Name:      f.Column,
		Type:      storage.Type,
		Nullable:  canBeNull(f),
		Default:   storage.Default,
		Identity:  f.Generated,
		Generated: storage.Generated,
		Collation: storage.Collation,
		Comment:   storage.Comment,
	}

	if q := storage.Sequence; q != nil {
		c.Default = "nextval('" + strings.ReplaceAll(typeName(b.d, q.Name), "'", "''") + "')"
	}

	if e := storage.Enum; e != nil && c.Type == e.Name {
		c.Type = typeName(b.d, e.Name)
	}

	label := m.Type.Name() + "." + f.Name

	if c.Type == "" {
		c.Type = b.columnType(generatedKey(f, valueType), storage)
	}

	switch {
	case c.Type == "":
		return nil, fmt.Errorf(
			"%w: %s has Go type %s: set it with b.Column(&...).Type(...), or implement schema.Typed",
			ErrNoColumnType,
			label,
			f.Type,
		)
	case f.Default && c.Default == "" && !f.Generated:
		return nil, fmt.Errorf(
			`%w: %s is ant:"default" but says nothing about the default: use ant:"default=expr" or b.Column(&...).Default(expr)`,
			ErrBadSchema,
			label,
		)
	}

	return c, nil
}

// typeName writes the name of an enum or a sequence where a column uses it, as
// CREATE TYPE and CREATE SEQUENCE named it: quoted. PostgreSQL folds a name
// without quotes to lower case, so a column of type UserStatus looked for
// userstatus, which did not exist (#100). A lower-case name stays as it is,
// as the database gives it back.
func typeName(d schema.Dialect, name string) string {
	plain := name != ""

	for i, r := range name {
		plain = plain && (r == '_' || r >= 'a' && r <= 'z' || i > 0 && r >= '0' && r <= '9')
	}

	if plain {
		return name
	}

	return d.Quote(name)
}

// generatedKey returns the type a key the database generates is stored as. An
// identity is a smallint, integer or bigint, and counts up from 1, so a uint64
// key of one is bigint, which holds every value it gives, where any other
// uint64 is numeric(20,0) (decision 138).
func generatedKey(f *meta.Field, t reflect.Type) reflect.Type {
	if f.Generated && t.Kind() == reflect.Uint64 {
		return reflect.TypeFor[int64]()
	}

	return t
}

func (b *schemaBuilder) columnType(t reflect.Type, s meta.Storage) string {
	return columnTypeFor(b.d, t, s)
}

// columnTypeFor returns the database type of a field, or "" if there is no
// obvious one.
func columnTypeFor(d schema.Dialect, t reflect.Type, s meta.Storage) string {
	if s.Type != "" {
		return s.Type
	}

	if typed, ok := reflect.New(t).Interface().(schema.Typed); ok {
		return typed.ColumnType(d.Name())
	}

	out, _ := d.ColumnType(t, schema.Facets{
		Length:    s.Length,
		Fixed:     s.Fixed,
		Precision: s.Precision,
		Scale:     s.Scale,
	})

	return out
}

var nullWrappers = map[reflect.Type]reflect.Type{
	reflect.TypeFor[sql.NullString]():  reflect.TypeFor[string](),
	reflect.TypeFor[sql.NullInt64]():   reflect.TypeFor[int64](),
	reflect.TypeFor[sql.NullInt32]():   reflect.TypeFor[int32](),
	reflect.TypeFor[sql.NullInt16]():   reflect.TypeFor[int16](),
	reflect.TypeFor[sql.NullByte]():    reflect.TypeFor[byte](),
	reflect.TypeFor[sql.NullFloat64](): reflect.TypeFor[float64](),
	reflect.TypeFor[sql.NullBool]():    reflect.TypeFor[bool](),
	reflect.TypeFor[sql.NullTime]():    reflect.TypeFor[time.Time](),
	reflect.TypeFor[DeletedAt]():       reflect.TypeFor[time.Time](),
	// The zero address and prefix are no address, which pgx writes as NULL and
	// reads NULL as.
	reflect.TypeFor[netip.Addr]():   reflect.TypeFor[netip.Addr](),
	reflect.TypeFor[netip.Prefix](): reflect.TypeFor[netip.Prefix](),
}

// canBeNull reports whether a field's column can hold NULL: its Go type can,
// unless Nullable or NotNull says otherwise, and a primary key never does.
func canBeNull(f *meta.Field) bool {
	_, nullable := columnValue(f.Type)

	if f.Storage.Nullable != nil {
		nullable = *f.Storage.Nullable
	}

	return nullable && !f.Primary
}

// columnValue returns the type a field stores, and whether it can hold NULL.
func columnValue(t reflect.Type) (reflect.Type, bool) {
	nullable := false

	for t.Kind() == reflect.Pointer {
		t, nullable = t.Elem(), true
	}

	if inner, ok := nullWrappers[t]; ok {
		return inner, true
	}

	if t.PkgPath() == "database/sql" && strings.HasPrefix(t.Name(), "Null[") {
		field, _ := t.FieldByName("V")

		return field.Type, true
	}

	if t.Kind() == reflect.Slice || t.Kind() == reflect.Map {
		nullable = true // a nil slice is written as NULL
	}

	return t, nullable
}

func actionSQL(a meta.Action) string {
	switch a {
	case Cascade:
		return "CASCADE"
	case SetNull:
		return "SET NULL"
	case Restrict:
		return "RESTRICT"
	}

	return ""
}

func (b *schemaBuilder) relations(m *meta.Model) error {
	if m.ReadOnly {
		return nil
	}

	for _, r := range m.Relations {
		link, err := r.Link()
		if err != nil {
			return err
		}

		if r.Pivot != nil {
			if r.Pivot.ReadOnly {
				continue
			}

			if err := b.pivot(m, r, link); err != nil {
				return err
			}

			continue
		}

		child, column, parent, ref := link.Target, link.Foreign, m, link.Local

		if link.BelongsTo {
			child, column, parent, ref = m, link.Local, link.Target, link.Foreign
		}

		if b.owner[child] == nil || parent.ReadOnly {
			continue // a view, or a table that belongs to something else
		}

		if err := b.merge(child, column, parent, ref, r); err != nil {
			return err
		}

		if r.MorphField != nil {
			b.keys[keyID(child, column)].skip = true // it can point at several tables
		}
	}

	return nil
}

// merge records a foreign key. User.Posts and Post.User are one key, so
// configuration from either side counts, and they must agree.
func (b *schemaBuilder) merge(
	child *meta.Model,
	column *meta.Field,
	parent *meta.Model,
	ref *meta.Field,
	r *meta.Relation,
) error {
	id := keyID(child, column)

	key := b.keys[id]
	if key == nil {
		key = &foreignKey{
			child:  child,
			parent: parent,
			column: column,
			ref:    ref,
		}
		b.keys[id] = key
	}

	if r.MorphField != nil {
		return nil // a morph key has a parent per target, and no constraint
	}

	if key.parent != parent || key.ref != ref {
		return fmt.Errorf(
			"%w: %s.%s refers to both %s.%s and %s.%s",
			ErrBadSchema,
			child.Table.Name,
			column.Column,
			key.parent.Table.Name,
			key.ref.Column,
			parent.Table.Name,
			ref.Column,
		)
	}

	for _, pair := range []struct {
		into *meta.Action
		from meta.Action
	}{{&key.onDelete, r.OnDelete}, {&key.onUpdate, r.OnUpdate}} {
		if *pair.into != 0 && pair.from != 0 && *pair.into != pair.from {
			return fmt.Errorf(
				"%w: the two sides of %s.%s set different actions",
				ErrBadSchema,
				child.Table.Name,
				column.Column,
			)
		}

		if pair.from != 0 {
			*pair.into = pair.from
		}
	}

	key.skip = key.skip || r.NoForeignKey

	key.noIndex = key.noIndex || r.NoIndex

	if r.ConstraintName != "" {
		key.name = r.ConstraintName
	}

	return nil
}

// foreignKeys adds the merged keys, in a stable order, each with an index unless
// one already covers it. PostgreSQL does not index foreign keys by itself, and a
// delete on the parent then scans the child table.
func (b *schemaBuilder) foreignKeys() error {
	ids := make([]string, 0, len(b.keys))

	for id := range b.keys {
		ids = append(ids, id)
	}

	slices.Sort(ids)

	for _, id := range ids {
		key := b.keys[id]
		if key.skip {
			continue
		}

		t := b.owner[key.child]
		column := t.Column(key.column.Column)

		parent := b.owner[key.parent]
		if parent != nil && !isUnique(parent, key.ref.Column) {
			return fmt.Errorf(
				`%w: %s.%s refers to %s.%s, which is not unique: add ant:"unique" to it`,
				ErrBadSchema,
				t.Name,
				column.Name,
				parent.Name,
				key.ref.Column,
			)
		}

		if key.onDelete == SetNull && !column.Nullable {
			return fmt.Errorf(
				"%w: %s.%s is set to NULL on delete, but cannot hold NULL: make the field a pointer",
				ErrBadSchema,
				t.Name,
				column.Name,
			)
		}

		typeAsTarget(column, key.column, key.ref, parent)

		name := key.name
		if name == "" {
			name = schema.ForeignKeyName(b.d, t.Name, column.Name)
		}

		t.ForeignKeys = append(t.ForeignKeys, &schema.ForeignKey{
			Name:       name,
			Columns:    []string{column.Name},
			RefSchema:  key.parent.Table.Schema,
			RefTable:   key.parent.Table.Name,
			RefColumns: []string{key.ref.Column},
			OnDelete:   actionSQL(key.onDelete),
			OnUpdate:   actionSQL(key.onUpdate),
		})

		if !key.noIndex && !leads(t, column.Name) {
			t.Indexes = append(t.Indexes, &schema.Index{
				Name:  schema.IndexName(b.d, t.Name, column.Name),
				Parts: []schema.IndexPart{{Column: column.Name}},
			})
		}
	}

	return nil
}

func keyID(child *meta.Model, column *meta.Field) string {
	return child.Table.Schema + "." + child.Table.Name + "." + column.Column
}

// constraints adds foreign keys declared on their own, with their indexes.
func (b *schemaBuilder) constraints(models []*meta.Model) error {
	for _, m := range models {
		t := b.owner[m]
		if t == nil {
			continue
		}

		for _, key := range m.Constraints {
			if key.Target == nil || len(key.Refs) != len(key.Columns) || len(key.Columns) == 0 {
				return fmt.Errorf(
					"%w: a foreign key on %s needs as many References as columns",
					ErrBadSchema,
					t.Name,
				)
			}

			columns := make([]string, len(key.Columns))
			parts := make([]schema.IndexPart, len(key.Columns))

			refs := make([]string, len(key.Refs))

			for i := range key.Columns {
				columns[i], refs[i] = key.Columns[i].Column, key.Refs[i].Column
				parts[i] = schema.IndexPart{Column: columns[i]}

				if column := t.Column(columns[i]); column != nil {
					typeAsTarget(column, key.Columns[i], key.Refs[i], b.owner[key.Target])
				}
			}

			name := key.Name
			if name == "" {
				name = schema.ForeignKeyName(b.d, t.Name, columns...)
			}

			t.ForeignKeys = append(t.ForeignKeys, &schema.ForeignKey{
				Name:       name,
				Columns:    columns,
				RefSchema:  key.Target.Table.Schema,
				RefTable:   key.Target.Table.Name,
				RefColumns: refs,
				OnDelete:   actionSQL(key.OnDelete),
				OnUpdate:   actionSQL(key.OnUpdate),
			})

			if !key.NoIndex && !leads(t, columns[0]) {
				t.Indexes = append(
					t.Indexes,
					&schema.Index{Name: schema.IndexName(b.d, t.Name, columns...), Parts: parts},
				)
			}
		}
	}

	return nil
}

// distinctNames refuses two tables, keys or indexes of one schema with one name,
// which PostgreSQL keeps in one namespace. The default names join columns with
// underscores, so an index on a_b and one on (a, b) both became t_a_b_idx, and
// the second CREATE INDEX failed when the migration ran, after check had passed
// (#112). Naming one with .Name(...) settles it.
func distinctNames(s *schema.Schema) error {
	owners := map[[2]string]string{}

	claim := func(schemaName, name, what string) error {
		at := [2]string{schemaName, name}
		if other, ok := owners[at]; ok {
			return fmt.Errorf(
				"%w: %s and %s are both named %s: name one of them with .Name(...)",
				ErrBadSchema,
				other,
				what,
				name,
			)
		}

		owners[at] = what

		return nil
	}

	for _, t := range s.Tables {
		ref := t.Ref().String()

		if err := claim(t.Schema, t.Name, "table "+ref); err != nil {
			return err
		}

		if k := t.PrimaryKey; k != nil {
			if err := claim(t.Schema, k.Name, "the primary key of "+ref); err != nil {
				return err
			}
		}

		for _, k := range t.Uniques {
			what := fmt.Sprintf("the unique key on %s (%s)", ref, strings.Join(k.Columns, ", "))
			if err := claim(t.Schema, k.Name, what); err != nil {
				return err
			}
		}

		for _, i := range t.Indexes {
			columns := make([]string, len(i.Parts))

			for n, part := range i.Parts {
				columns[n] = cmp.Or(part.Column, part.Expr)
			}

			what := fmt.Sprintf("the index on %s (%s)", ref, strings.Join(columns, ", "))
			if err := claim(t.Schema, i.Name, what); err != nil {
				return err
			}
		}
	}

	return nil
}

// typeAsTarget gives a foreign key column with no type of its own the type its
// target was given, as a join table's columns take theirs. A string key typed
// uuid left a string foreign key text, which PostgreSQL cannot join to uuid,
// so the migration failed on its first run (#187). A serial target is the
// integer it draws from a sequence, which the referring column holds. A uint64
// on either side takes the target's type too: a key the database generates is
// bigint and any other uint64 numeric(20,0), and PostgreSQL refuses a foreign
// key from one to the other (decision 138).
func typeAsTarget(column *schema.Column, field, ref *meta.Field, target *schema.Table) {
	follows := ref.Storage.Type != "" || isUint64(field) || isUint64(ref)

	if target == nil || field.Storage.Type != "" || field.Storage.Enum != nil || !follows {
		return
	}

	to := target.Column(ref.Column)
	if to == nil {
		return
	}

	column.Type = to.Type

	if plain, ok := map[string]string{
		"smallserial": "smallint",
		"serial":      "integer",
		"bigserial":   "bigint",
	}[strings.ToLower(to.Type)]; ok {
		column.Type = plain
	}
}

// isUint64 reports whether f holds a uint64, as a pointer or an sql.Null too.
func isUint64(f *meta.Field) bool {
	t, _ := columnValue(f.Type)

	return t.Kind() == reflect.Uint64
}

func isUnique(t *schema.Table, column string) bool {
	single := func(columns []string) bool { return len(columns) == 1 && columns[0] == column }
	if t.PrimaryKey != nil && single(t.PrimaryKey.Columns) {
		return true
	}

	return slices.ContainsFunc(t.Uniques, func(k *schema.Key) bool { return single(k.Columns) }) ||
		slices.ContainsFunc(t.Indexes, func(i *schema.Index) bool {
			return i.Unique && i.Where == "" && len(i.Parts) == 1 && i.Parts[0].Column == column
		})
}

// leads reports whether some index starts with column, so it can serve lookups.
func leads(t *schema.Table, column string) bool {
	if t.PrimaryKey != nil && t.PrimaryKey.Columns[0] == column {
		return true
	}

	return slices.ContainsFunc(
		t.Uniques,
		func(k *schema.Key) bool { return k.Columns[0] == column },
	) ||
		slices.ContainsFunc(t.Indexes, func(i *schema.Index) bool {
			return i.Where == "" && i.Parts[0].Column == column
		})
}

// pivot adds the join table of a many-to-many relation. Both sides usually
// declare it, and a modelled join table takes precedence.
func (b *schemaBuilder) pivot(m *meta.Model, r *meta.Relation, link meta.Link) error {
	p := r.Pivot
	if p.Table.Name == "" {
		return fmt.Errorf(
			"%w: many-to-many %s.%s has no join table: add .Through(name)",
			ErrBadSchema,
			m.Type.Name(),
			r.Name,
		)
	}

	ref := schema.Ref{Schema: p.Table.Schema, Name: p.Table.Name}
	columns := []string{p.Local, p.Foreign}

	if existing := b.out.Table(ref); existing != nil {
		if existing.Column(p.Local) == nil || existing.Column(p.Foreign) == nil {
			return fmt.Errorf(
				"%w: join table %s has no columns %s and %s",
				ErrBadSchema,
				ref,
				p.Local,
				p.Foreign,
			)
		}

		return nil
	}

	if m.Unmanaged || link.Target.Unmanaged || b.owner[m] == nil || b.owner[link.Target] == nil {
		return nil
	}

	t := &schema.Table{Schema: ref.Schema, Name: ref.Name}

	sides := []struct {
		column string
		key    *meta.Field
		model  *meta.Model
	}{{p.Local, link.Local, m}, {p.Foreign, link.Foreign, link.Target}}

	for _, side := range sides {
		source := b.owner[side.model].Column(side.key.Column)
		t.Columns = append(t.Columns, &schema.Column{Name: side.column, Type: source.Type})

		t.ForeignKeys = append(t.ForeignKeys, &schema.ForeignKey{
			Name:       schema.ForeignKeyName(b.d, t.Name, side.column),
			Columns:    []string{side.column},
			RefSchema:  side.model.Table.Schema,
			RefTable:   side.model.Table.Name,
			RefColumns: []string{side.key.Column},
			OnDelete:   "CASCADE",
		})
	}

	t.PrimaryKey = &schema.Key{Name: schema.PrimaryKeyName(b.d, t.Name), Columns: columns}

	t.Indexes = []*schema.Index{
		{
			Name:  schema.IndexName(b.d, t.Name, p.Foreign),
			Parts: []schema.IndexPart{{Column: p.Foreign}},
		},
	}

	b.out.Tables = append(b.out.Tables, t)

	return nil
}

// ddl returns the context's dialect and the schema its models describe.
func (c *Context) ddl() (schema.Dialect, *schema.Schema, error) {
	s := c.state
	if s == nil {
		return nil, nil, ErrNotOpen
	}

	d, err := schema.Of(s.dialect)
	if err != nil {
		return nil, nil, err
	}

	target, err := buildSchema(d, s.self)

	return d, target, err
}

// EnsureCreated creates the tables, indexes, and foreign keys of models that have
// no table yet, and the enums and sequences they use that are not there, directly
// from the structs, and leaves existing ones as they are.
// It is for tests and prototypes. Use migrations for anything with data to keep.
func (c *Context) EnsureCreated(ctx context.Context) error {
	d, target, err := c.ddl()
	if err != nil {
		return err
	}

	// Inspect names the default schema "", which a model may name outright.
	target, err = liveschema.Relative(ctx, d, c.state.exec, target)
	if err != nil {
		return err
	}

	live, err := d.Inspect(ctx, c.state.exec, target.Schemas)
	if err != nil {
		return err
	}

	existing := target.Clone()
	existing.Schemas = live.Schemas

	// What the models declare counts as there only if the database has it, so
	// the diff creates what is missing and leaves the rest as it is: tables, and
	// the enums and sequences they use (#101).
	existing.Tables = slices.DeleteFunc(
		existing.Tables,
		func(t *schema.Table) bool { return live.Table(t.Ref()) == nil },
	)

	existing.Enums = slices.DeleteFunc(
		existing.Enums,
		func(e *schema.Enum) bool { return live.Enum(e.Name) == nil },
	)

	existing.Sequences = slices.DeleteFunc(
		existing.Sequences,
		func(s *schema.Sequence) bool { return live.Sequence(s.Name) == nil },
	)

	plan, err := schema.Diff(existing, target)
	if err != nil {
		return err
	}

	statements, err := schema.Statements(d, plan)
	if err != nil {
		return err
	}

	return c.apply(ctx, statements)
}

// EnsureDeleted drops the tables of the context's models, with their data and
// whatever depends on them. It is for tests.
func (c *Context) EnsureDeleted(ctx context.Context) error {
	d, target, err := c.ddl()
	if err != nil {
		return err
	}

	var statements []string

	for _, t := range target.Tables {
		drop, err := d.Statements(schema.DropTable{Table: t.Ref(), IfExists: true, Cascade: true})
		if err != nil {
			return err
		}

		statements = append(statements, drop...)
	}

	return c.apply(ctx, statements)
}

func (c *Context) apply(ctx context.Context, statements []string) error {
	return c.state.transact(ctx, true, func(e exec.Executor) error {
		for _, statement := range statements {
			if _, err := e.ExecContext(ctx, statement); err != nil {
				return fmt.Errorf("ant: %w: %s", err, statement)
			}
		}

		return nil
	})
}
