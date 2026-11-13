package ant

import (
	"context"
	"database/sql"
	"fmt"
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
// excluded from migrations have no table in the result.
func SchemaOf[DB any](d dialect.Dialect) (*schema.Schema, error) {
	if _, ok := any(new(DB)).(Handle); !ok {
		return nil, fmt.Errorf("%w: %T", ErrNoContext, new(DB))
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

	v := reflect.ValueOf(db).Elem()
	for i := range v.NumField() {
		f := v.Field(i)
		if !f.CanAddr() || !f.Addr().CanInterface() {
			continue
		}

		if t, ok := f.Addr().Interface().(modelTable); ok {
			if err := add(t.tableModel()); err != nil {
				return nil, err
			}
		}
	}

	for i := 0; i < len(out); i++ {
		for _, r := range out[i].Relations {
			if err := add(meta.OfType(r.Target)); err != nil {
				return nil, err
			}
		}
	}

	return out, nil
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
		case !m.ReadOnly:
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

	for _, t := range b.out.Tables {
		if t.Schema != "" {
			b.out.Schemas = append(b.out.Schemas, t.Schema)
		}
	}

	b.out.Sort()

	return b.out, nil
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
	valueType, nullable := columnValue(f.Type)

	if f.Storage.Nullable != nil {
		nullable = *f.Storage.Nullable
	}

	storage := f.Storage

	c := &schema.Column{
		Name:      f.Column,
		Type:      storage.Type,
		Nullable:  nullable && !f.Primary,
		Default:   storage.Default,
		Identity:  f.Generated,
		Generated: storage.Generated,
		Collation: storage.Collation,
		Comment:   storage.Comment,
	}

	if q := storage.Sequence; q != nil {
		c.Default = "nextval('" + strings.ReplaceAll(q.Name, "'", "''") + "')"
	}

	label := m.Type.Name() + "." + f.Name

	if c.Type == "" {
		c.Type = b.columnType(valueType, storage)
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
// no table yet, directly from the structs, and leaves existing tables as they are.
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

	existing.Tables = slices.DeleteFunc(
		existing.Tables,
		func(t *schema.Table) bool { return live.Table(t.Ref()) == nil },
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
