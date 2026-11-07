package ant

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"unsafe"

	"github.com/skudra-go/ant/internal/meta"
	"github.com/skudra-go/ant/qb"
)

// Entity is implemented by models that override conventions. Configure runs
// once, when a context with the model opens, or on first use by Raw.
type Entity[E Entity[E]] interface {
	Configure(b *ModelBuilder[E])
}

// Action is what happens to dependent rows when their parent is deleted or its
// key changes.
type Action = meta.Action

// Actions.
const (
	NoAction Action = iota
	Cascade
	SetNull
	Restrict
)

// configure applies T's Configure method, if it has one.
func configure[T any]() *meta.Model {
	model := meta.Of[T]()
	configureModel(model)

	return model
}

// MixinConfigurer is implemented by a struct meant to be embedded in many models,
// to add the configuration that goes with its fields. It works by column name,
// since it does not know which model embeds it.
type MixinConfigurer interface {
	ConfigureMixin(m *Mixin)
}

// Mixin configures whichever model embeds a MixinConfigurer.
type Mixin struct {
	model *meta.Model
}

// Table returns the model's table name.
func (m *Mixin) Table() string { return m.model.Table.Name }

// Index adds an index on columns of the model, named after them.
func (m *Mixin) Index(columns ...string) *IndexBuilder {
	index := new(meta.Index)

	for _, name := range columns {
		index.Parts = append(index.Parts, meta.IndexPart{Field: m.model.Column(name)})
	}

	m.model.Indexes = append(m.model.Indexes, index)

	return &IndexBuilder{index: index, resolve: func(FieldRef) *meta.Field { return nil }}
}

// Check adds a check constraint.
func (m *Mixin) Check(name, expr string) {
	m.model.Checks = append(m.model.Checks, meta.Check{Name: name, Expr: expr})
}

// Eq is a condition on a column by name, for use in a Filter. It works in joins,
// as ant.Eq does, because it knows which table it belongs to.
func (m *Mixin) Eq(column string, value any) Cond {
	f := m.model.Column(column)
	if f == nil {
		return Cond{
			err: fmt.Errorf("%w: no column %s on %s", ErrNotColumn, column, m.model.Type.Name()),
		}
	}

	return Cond{
		node: qb.Compare{Left: m.model.Col(f), Op: qb.OpEq, Right: qb.Param{Value: value}},
		refs: []*meta.Model{m.model},
	}
}

// Filter adds a global filter, as ModelBuilder.Filter does.
func (m *Mixin) Filter(name string, filter func(context.Context) Cond) {
	m.model.Filters = append(m.model.Filters, meta.Filter{
		Name: name,
		Build: func(ctx context.Context) (qb.Cond, error) {
			c := filter(ctx)

			return c.node, c.err
		},
	})
}

// configureMixins runs ConfigureMixin on each embedded struct that has it.
func configureMixins(model *meta.Model) {
	for sf := range model.Type.Fields() {
		if !sf.Anonymous || sf.Type.Kind() != reflect.Struct {
			continue
		}

		if mixin, ok := reflect.New(sf.Type).Interface().(MixinConfigurer); ok {
			mixin.ConfigureMixin(&Mixin{model})
		}
	}
}

// builderSetup is implemented by every ModelBuilder instantiation.
type builderSetup interface {
	start(*meta.Model)
	result() error
}

// configureModel runs a model's Configure method once. It finds the method by
// reflection, so a model reached only through a relation is configured too.
func configureModel(model *meta.Model) {
	model.Configure(func() error {
		applyConventions(model)
		configureMixins(model)

		method, ok := reflect.PointerTo(model.Type).MethodByName("Configure")
		if !ok || method.Type.NumIn() != 2 || method.Type.In(1).Kind() != reflect.Pointer {
			return nil
		}

		builder := reflect.New(method.Type.In(1).Elem())

		setup, ok := builder.Interface().(builderSetup)
		if !ok {
			return nil
		}

		setup.start(model)
		method.Func.Call([]reflect.Value{reflect.New(model.Type), builder})

		return setup.result()
	})
}

// ModelBuilder collects configuration for model E. Errors surface from Open.
type ModelBuilder[E any] struct {
	model *meta.Model
	err   error
}

func (b *ModelBuilder[E]) start(model *meta.Model) { b.model = model }

func (b *ModelBuilder[E]) result() error { return b.err }

func (b *ModelBuilder[E]) fail(err error) {
	b.err = errors.Join(b.err, fmt.Errorf("ant: configure %s: %w", b.model.Type.Name(), err))
}

// Table sets the table name.
func (b *ModelBuilder[E]) Table(name string) {
	if name == "" {
		b.fail(errors.New("empty table name"))

		return
	}

	b.model.Table.Name = name
}

// Schema sets the database schema.
func (b *ModelBuilder[E]) Schema(name string) { b.model.Table.Schema = name }

// View maps the model to a view, which can be queried but not written.
func (b *ModelBuilder[E]) View(name string) {
	b.Table(name)
	b.model.ReadOnly = true
}

// NoKey removes the primary key, for rows with no identity.
func (b *ModelBuilder[E]) NoKey() { b.model.SetKey() }

// NoTimestamps stops CreatedAt and UpdatedAt being maintained.
func (b *ModelBuilder[E]) NoTimestamps() { b.model.Created, b.model.Updated = nil, nil }

// Filter adds a global filter, applied to every query of this model, including
// joins and subqueries. It receives the call's context, which suits tenancy. A
// filter that returns a Cond with an error fails the query. Query.IgnoreFilters
// switches it off.
func (b *ModelBuilder[E]) Filter(name string, filter func(context.Context) Cond) {
	b.model.Filters = append(b.model.Filters, meta.Filter{
		Name: name,
		Build: func(ctx context.Context) (qb.Cond, error) {
			c := filter(ctx)

			return c.node, c.err
		},
	})
}

func (b *ModelBuilder[E]) keyField(ref FieldRef) *meta.Field {
	switch {
	case ref.err != nil:
		b.fail(ref.err)

		return nil
	case ref.model != b.model:
		b.fail(ErrWrongModel)

		return nil
	}

	return ref.field
}

// Key sets the primary key. An integer key is database-generated unless
// NotGenerated says otherwise.
func (b *ModelBuilder[E]) Key[V any](field *V) *KeyBuilder {
	key := &KeyBuilder{}

	if f := b.keyField(F(field)); f != nil {
		b.model.SetKey(f)
		key.field = f
	}

	return key
}

// CompositeKey sets a primary key of several columns. Rows are read with FindKey.
func (b *ModelBuilder[E]) CompositeKey(fields ...FieldRef) {
	var keys []*meta.Field

	for _, ref := range fields {
		if f := b.keyField(ref); f != nil {
			keys = append(keys, f)
		}
	}

	b.model.SetKey(keys...)
}

// GenerateKey sets a primary key that fn fills in Go when it is zero, such as a
// UUID.
func GenerateKey[E, V any](b *ModelBuilder[E], field *V, fn func() V) {
	f := b.keyField(F(field))
	if f == nil {
		return
	}

	b.model.SetKey(f)
	f.Generated = false

	f.Generator = func(entity unsafe.Pointer) {
		*(*V)(unsafe.Add(entity, f.Offset)) = fn()
	}
}

// Comment describes the table in the database.
func (b *ModelBuilder[E]) Comment(text string) { b.model.Comment = text }

// Discriminator shares a table between several models: rows of this one hold
// value in field. Queries see only those rows, and inserts set it. It is
// single-table inheritance, without the inheritance Go does not have:
//
//	func (Dog) Configure(b *ant.ModelBuilder[Dog]) {
//		b.Table("animals")
//		b.Discriminator(&D.Kind, "dog")
//	}
//
// Migrations merge the models into one table, and a column only some of them
// declare allows NULL.
func (b *ModelBuilder[E]) Discriminator[V comparable](field *V, value V) {
	ref := F(field)

	f := b.keyField(ref)
	if f == nil {
		return
	}

	b.model.Discriminator, b.model.DiscriminatorValue = f, value
	column := b.model.Col(f)
	b.model.Filters = append(b.model.Filters, meta.Filter{
		Name: "ant.discriminator",
		Build: func(context.Context) (qb.Cond, error) {
			return qb.Compare{Left: column, Op: qb.OpEq, Right: qb.Param{Value: value}}, nil
		},
	})
}

// Validate adds a rule for a field, checked before Insert and SaveChanges write
// it, as a Validate hook is. A failure is a *ValidationError. Rules do not run for
// bulk writes, which load no entities. Package validate has common ones.
func (b *ModelBuilder[E]) Validate[V any](field *V, rule func(V) error) {
	f := b.keyField(F(field))
	if f == nil {
		return
	}

	b.model.Rules = append(b.model.Rules, meta.Rule{
		Field: f,
		Check: func(entity unsafe.Pointer) error {
			return rule(*(*V)(unsafe.Add(entity, f.Offset)))
		},
	})
}

// ForeignKey declares a foreign key on its own, for one no relation can express,
// such as a key of several columns. It adds a constraint and nothing else, so
// relations and queries do not know about it.
//
//	b.ForeignKey(ant.F(&L.OrderID), ant.F(&L.Region)).References(ant.F(&O.ID), ant.F(&O.Region))
func (b *ModelBuilder[E]) ForeignKey(fields ...FieldRef) *ForeignKeyBuilder {
	key := &meta.ForeignKey{}

	for _, ref := range fields {
		if f := b.keyField(ref); f != nil {
			key.Columns = append(key.Columns, f)
		}
	}

	b.model.Constraints = append(b.model.Constraints, key)

	return &ForeignKeyBuilder{key: key, fail: b.fail}
}

// PartitionByRange declares the table partitioned by ranges of the columns. A
// partitioned table's primary key and unique constraints must include them, so a
// time-partitioned table usually has CompositeKey(id, time). Migrations create the
// parent, and package postgres manages the partitions themselves. See
// postgres.Partitions.
func (b *ModelBuilder[E]) PartitionByRange(fields ...FieldRef) { b.partition("RANGE", fields) }

// PartitionByList declares the table partitioned by lists of column values.
func (b *ModelBuilder[E]) PartitionByList(fields ...FieldRef) { b.partition("LIST", fields) }

// PartitionByHash declares the table partitioned by a hash of the columns.
func (b *ModelBuilder[E]) PartitionByHash(fields ...FieldRef) { b.partition("HASH", fields) }

func (b *ModelBuilder[E]) partition(method string, fields []FieldRef) {
	p := &meta.Partition{Method: method}

	for _, ref := range fields {
		if f := b.keyField(ref); f != nil {
			p.Columns = append(p.Columns, f)
		}
	}

	b.model.Partition = p
}

// KeyName names the primary key constraint, in place of table_pkey. It is for
// matching an existing database.
func (b *ModelBuilder[E]) KeyName(name string) { b.model.KeyName = name }

// ExcludeFromMigrations means migrations leave this model's table alone, because
// something else owns it. Queries still work.
func (b *ModelBuilder[E]) ExcludeFromMigrations() { b.model.Unmanaged = true }

// Check adds a check constraint. expr is SQL, as in age >= 0.
func (b *ModelBuilder[E]) Check(name, expr string) {
	b.model.Checks = append(b.model.Checks, meta.Check{Name: name, Expr: expr})
}

// Comparer sets how change tracking compares a field, in place of a deep
// comparison. It suits a slice whose order does not matter, or a map with volatile
// entries. A change the comparer does not see is not saved.
func (b *ModelBuilder[E]) Comparer[V any](field *V, equal func(a, b V) bool) {
	if f := b.keyField(F(field)); f != nil {
		f.Equal = func(x, y unsafe.Pointer) bool { return equal(*(*V)(x), *(*V)(y)) }
	}
}

// Column configures how a column is declared in migrations.
func (b *ModelBuilder[E]) Column[V any](field *V) *ColumnBuilder {
	return &ColumnBuilder{field: b.keyField(F(field))}
}

// Index declares an index on field. Add more columns with And, and more options
// with the other IndexBuilder methods.
func (b *ModelBuilder[E]) Index[V any](field *V) *IndexBuilder {
	index := b.addIndex()

	if f := b.keyField(F(field)); f != nil {
		index.index.Parts = []meta.IndexPart{{Field: f}}
	}

	return index
}

// IndexExpr declares an index on a SQL expression, such as lower(email). Name it
// with Name.
func (b *ModelBuilder[E]) IndexExpr(expr string) *IndexBuilder {
	index := b.addIndex()
	index.index.Parts = []meta.IndexPart{{Expr: expr}}

	return index
}

func (b *ModelBuilder[E]) addIndex() *IndexBuilder {
	index := new(meta.Index)
	b.model.Indexes = append(b.model.Indexes, index)

	return &IndexBuilder{index: index, resolve: b.keyField}
}

// HasManyThrough configures a read-only relation that crosses a table in between:
// a Country has many Posts through Users. parentKey is the column on the middle
// model that refers to the owner, and throughKey the column on the target that
// refers to the middle model's key.
//
//	b.HasManyThrough(&C.Posts, &U.CountryID, &P.UserID)
func (b *ModelBuilder[E]) HasManyThrough[R, A, C any](
	rel *[]R,
	parentKey *A,
	throughKey *C,
) *RelationBuilder {
	return configureThrough(b, rel, parentKey, throughKey)
}

// HasOneThrough is HasManyThrough for a single related row.
func (b *ModelBuilder[E]) HasOneThrough[R, A, C any](
	rel **R,
	parentKey *A,
	throughKey *C,
) *RelationBuilder {
	return configureThrough(b, rel, parentKey, throughKey)
}

func configureThrough[E, V, A, C any](
	b *ModelBuilder[E],
	rel *V,
	parentKey *A,
	throughKey *C,
) *RelationBuilder {
	out := configureRelation(b, rel)

	parent, middle, err := fieldOf(parentKey)
	if err != nil {
		b.fail(err)

		return out
	}

	through, _, err := fieldOf(throughKey)
	if err != nil {
		b.fail(err)

		return out
	}

	configureModel(middle)

	key := middle.Key()
	if key == nil {
		b.fail(ErrNoKey)

		return out
	}

	if out.relation != nil {
		out.relation.Pivot = &meta.Pivot{
			Table:    middle.Table,
			Local:    parent.Column,
			Foreign:  key.Column,
			ReadOnly: true,
		}

		out.relation.ThroughKey = through
	}

	return out
}

// HasMany configures a one-to-many relation.
func (b *ModelBuilder[E]) HasMany[R any](
	rel *[]R,
) *RelationBuilder {
	return configureRelation(b, rel)
}

// HasOne configures a one-to-one relation.
func (b *ModelBuilder[E]) HasOne[R any](
	rel **R,
) *RelationBuilder {
	return configureRelation(b, rel)
}

// ManyToMany configures a many-to-many relation through a join table, named with
// Through. Its columns default to the singular model names plus _id, such as
// user_id and role_id. For extra columns on the join table, model it instead and
// use two one-to-many relations.
func (b *ModelBuilder[E]) ManyToMany[R any](rel *[]R) *RelationBuilder {
	out := configureRelation(b, rel)
	if out.relation != nil {
		out.relation.Pivot = &meta.Pivot{Table: &qb.TableRef{}}
	}

	return out
}

// BelongsTo configures a many-to-one relation.
func (b *ModelBuilder[E]) BelongsTo[R any](rel **R) *RelationBuilder {
	return configureRelation(b, rel)
}

func configureRelation[E, V any](b *ModelBuilder[E], rel *V) *RelationBuilder {
	r, err := relation(rel)
	if err != nil {
		b.fail(err)

		return &RelationBuilder{fail: func(error) {}}
	}

	if r.Owner != b.model {
		b.fail(ErrWrongModel)
	}

	return &RelationBuilder{relation: r, fail: b.fail}
}

// KeyBuilder configures a primary key.
type KeyBuilder struct {
	field *meta.Field
}

// NotGenerated means the application always supplies the key, so a zero key is
// written as zero.
func (k *KeyBuilder) NotGenerated() *KeyBuilder {
	if k.field != nil {
		k.field.Generated = false
	}

	return k
}

// ColumnBuilder configures how a column is declared in migrations. Queries are
// not affected, except by Default and Generated, which say where a value comes
// from.
type ColumnBuilder struct {
	field *meta.Field
}

func (c *ColumnBuilder) set(change func(*meta.Field)) *ColumnBuilder {
	if c.field != nil {
		change(c.field)
	}

	return c
}

// Type sets the database type, written as it would be in SQL. It overrides the
// type that comes from the Go type, and is how a custom Go type joins migrations.
func (c *ColumnBuilder) Type(sql string) *ColumnBuilder {
	return c.set(func(f *meta.Field) { f.Storage.Type = sql })
}

// Length limits a string column to n characters.
func (c *ColumnBuilder) Length(n int) *ColumnBuilder {
	return c.set(func(f *meta.Field) { f.Storage.Length = n })
}

// Precision makes the column an exact number with the given digits in total and
// after the point.
func (c *ColumnBuilder) Precision(precision, scale int) *ColumnBuilder {
	return c.set(func(f *meta.Field) { f.Storage.Precision, f.Storage.Scale = precision, scale })
}

// Default sets the column's default, a SQL expression such as now() or 'draft'.
// As with the ant:"default" tag, a zero field is left for the database to fill.
func (c *ColumnBuilder) Default(expr string) *ColumnBuilder {
	return c.set(func(f *meta.Field) { f.Storage.Default, f.Default = expr, true })
}

// Generated makes the column a stored generated column, computed by the database
// from expr. The field is read-only.
func (c *ColumnBuilder) Generated(expr string) *ColumnBuilder {
	return c.set(func(f *meta.Field) { f.Storage.Generated, f.ReadOnly = expr, true })
}

// Nullable allows NULL in a column whose Go type has no way to hold it.
func (c *ColumnBuilder) Nullable() *ColumnBuilder {
	return c.set(func(f *meta.Field) { f.Storage.Nullable = new(true) })
}

// NotNull refuses NULL in a column whose Go type could hold it.
func (c *ColumnBuilder) NotNull() *ColumnBuilder {
	return c.set(func(f *meta.Field) { f.Storage.Nullable = new(false) })
}

// Collation sets the column's collation.
func (c *ColumnBuilder) Collation(name string) *ColumnBuilder {
	return c.set(func(f *meta.Field) { f.Storage.Collation = name })
}

// Fixed makes a string column exactly n characters.
func (c *ColumnBuilder) Fixed(n int) *ColumnBuilder {
	return c.set(func(f *meta.Field) { f.Storage.Length, f.Storage.Fixed = n, true })
}

// Comment describes the column in the database.
func (c *ColumnBuilder) Comment(text string) *ColumnBuilder {
	return c.set(func(f *meta.Field) { f.Storage.Comment = text })
}

// Sensitive keeps the column's values out of logs, even with LogValues.
func (c *ColumnBuilder) Sensitive() *ColumnBuilder {
	return c.set(func(f *meta.Field) { f.Sensitive = true })
}

// Sequence fills the column from a database sequence, which migrations create. As
// with Default, a zero field is left for the database.
func (c *ColumnBuilder) Sequence(name string, options ...SequenceOption) *ColumnBuilder {
	return c.set(func(f *meta.Field) {
		sequence := &meta.Sequence{Name: name, Start: 1, Increment: 1}

		for _, option := range options {
			option(sequence)
		}

		f.Storage.Sequence = sequence
		f.Default = true
	})
}

// SequenceOption configures a sequence.
type SequenceOption func(*meta.Sequence)

// StartAt sets a sequence's first value.
func StartAt(n int64) SequenceOption { return func(s *meta.Sequence) { s.Start = n } }

// IncrementBy sets a sequence's step.
func IncrementBy(n int64) SequenceOption { return func(s *meta.Sequence) { s.Increment = n } }

// Enum stores the column as a native enum type, which migrations create and
// extend. Removing a value takes a migration by hand, since rows may hold it.
func (c *ColumnBuilder) Enum(name string, values ...string) *ColumnBuilder {
	return c.set(func(f *meta.Field) {
		f.Storage.Type = name
		f.Storage.Enum = &meta.Enum{Name: name, Values: values}
	})
}

// Unique adds a unique constraint on the column, like the ant:"unique" tag. An
// optional name replaces table_column_key.
func (c *ColumnBuilder) Unique(name ...string) *ColumnBuilder {
	return c.set(func(f *meta.Field) {
		f.Unique = true

		if len(name) > 0 {
			f.Storage.UniqueName = name[0]
		}
	})
}

// IndexBuilder configures an index.
type IndexBuilder struct {
	index   *meta.Index
	resolve func(FieldRef) *meta.Field
}

// And adds a column to the index.
func (i *IndexBuilder) And[V any](field *V) *IndexBuilder {
	if f := i.resolve(F(field)); f != nil {
		i.index.Parts = append(i.index.Parts, meta.IndexPart{Field: f})
	}

	return i
}

// AndExpr adds a SQL expression to the index.
func (i *IndexBuilder) AndExpr(expr string) *IndexBuilder {
	i.index.Parts = append(i.index.Parts, meta.IndexPart{Expr: expr})

	return i
}

// Desc sorts the most recently added column in descending order.
func (i *IndexBuilder) Desc() *IndexBuilder {
	if n := len(i.index.Parts); n > 0 {
		i.index.Parts[n-1].Desc = true
	}

	return i
}

// NullsFirst puts NULLs first in the most recently added column, which only a
// column going up needs, as one going down has them first already.
func (i *IndexBuilder) NullsFirst() *IndexBuilder { return i.nulls(true) }

// NullsLast puts NULLs last in the most recently added column, which only a
// column going down needs, as one going up has them last already.
func (i *IndexBuilder) NullsLast() *IndexBuilder { return i.nulls(false) }

func (i *IndexBuilder) nulls(first bool) *IndexBuilder {
	if n := len(i.index.Parts); n > 0 {
		i.index.Parts[n-1].NullsFirst = &first
	}

	return i
}

// Unique makes the index unique.
func (i *IndexBuilder) Unique() *IndexBuilder {
	i.index.Unique = true

	return i
}

// Name sets the index name, in place of table_columns_idx.
func (i *IndexBuilder) Name(name string) *IndexBuilder {
	i.index.Name = name

	return i
}

// Where makes the index partial, covering rows that match a SQL condition.
func (i *IndexBuilder) Where(condition string) *IndexBuilder {
	i.index.Where = condition

	return i
}

// Using sets the index method, such as gin.
func (i *IndexBuilder) Using(method string) *IndexBuilder {
	i.index.Method = method

	return i
}

// Include stores a column in the index without indexing it.
func (i *IndexBuilder) Include[V any](field *V) *IndexBuilder {
	if f := i.resolve(F(field)); f != nil {
		i.index.Include = append(i.index.Include, f)
	}

	return i
}

// Concurrently builds the index without blocking writes, when it is added to a
// table that already exists. PostgreSQL cannot do that in a transaction, so the
// migration holding it runs without one.
func (i *IndexBuilder) Concurrently() *IndexBuilder {
	i.index.Concurrent = true

	return i
}

// ForeignKeyBuilder configures a foreign key from ModelBuilder.ForeignKey.
type ForeignKeyBuilder struct {
	key  *meta.ForeignKey
	fail func(error)
}

// References names the columns the key points to, in order.
func (k *ForeignKeyBuilder) References(fields ...FieldRef) *ForeignKeyBuilder {
	for _, ref := range fields {
		if ref.err != nil {
			k.fail(ref.err)

			continue
		}

		k.key.Target = ref.model
		k.key.Refs = append(k.key.Refs, ref.field)
	}

	return k
}

// Name names the constraint, in place of table_columns_fkey.
func (k *ForeignKeyBuilder) Name(name string) *ForeignKeyBuilder {
	k.key.Name = name

	return k
}

// OnDelete sets the action when the parent is deleted.
func (k *ForeignKeyBuilder) OnDelete(action Action) *ForeignKeyBuilder {
	k.key.OnDelete = action

	return k
}

// OnUpdate sets the action when the parent's key changes.
func (k *ForeignKeyBuilder) OnUpdate(action Action) *ForeignKeyBuilder {
	k.key.OnUpdate = action

	return k
}

// NoIndex leaves out the index migrations otherwise add on the columns.
func (k *ForeignKeyBuilder) NoIndex() *ForeignKeyBuilder {
	k.key.NoIndex = true

	return k
}

// RelationBuilder configures a relation.
type RelationBuilder struct {
	relation *meta.Relation
	fail     func(error)
}

// Through names the join table of a many-to-many relation.
func (r *RelationBuilder) Through(table string) *RelationBuilder {
	if r.relation != nil && r.relation.Pivot != nil {
		r.relation.Pivot.Table.Name = table
	}

	return r
}

// PivotColumns names the join table's columns, for the owner and the target.
func (r *RelationBuilder) PivotColumns(local, foreign string) *RelationBuilder {
	if r.relation != nil && r.relation.Pivot != nil {
		r.relation.Pivot.Local, r.relation.Pivot.Foreign = local, foreign
	}

	return r
}

// ForeignKey sets the foreign key column, on either model.
func (r *RelationBuilder) ForeignKey[V any](field *V) *RelationBuilder {
	f, _, err := fieldOf(field)
	if err != nil {
		r.fail(err)
	} else if r.relation != nil {
		r.relation.ForeignKey = f
	}

	return r
}

// References sets the column the foreign key points to, in place of the
// primary key.
func (r *RelationBuilder) References[V any](field *V) *RelationBuilder {
	f, _, err := fieldOf(field)
	if err != nil {
		r.fail(err)
	} else if r.relation != nil {
		r.relation.References = f
	}

	return r
}

// OnDelete sets the action when the parent is deleted.
func (r *RelationBuilder) OnDelete(action Action) *RelationBuilder {
	if r.relation != nil {
		r.relation.OnDelete = action
	}

	return r
}

// OnUpdate sets the action when the parent's key changes.
func (r *RelationBuilder) OnUpdate(action Action) *RelationBuilder {
	if r.relation != nil {
		r.relation.OnUpdate = action
	}

	return r
}

// Where scopes the relation: Include, Has, joins, and counts see only the target
// rows that meet conds.
//
//	b.HasMany(&U.LivePosts).ForeignKey(&P.UserID).Where(ant.Eq(&P.Published, true))
func (r *RelationBuilder) Where(conds ...Cond) *RelationBuilder {
	if r.relation == nil {
		return r
	}

	for _, c := range conds {
		if c.err != nil {
			r.fail(c.err)

			continue
		}

		r.relation.Scope = append(r.relation.Scope, c.node)
	}

	return r
}

// Morph makes the relation polymorphic: the foreign key can refer to several
// tables, and field says which. Set it on each relation with the value for that
// target, which is also the place to keep a map of type names:
//
//	b.HasMany(&Post{}.Comments).ForeignKey(&C.OwnerID).Morph(&C.OwnerType, "post")
//	b.BelongsTo(&C.Post).ForeignKey(&C.OwnerID).Morph(&C.OwnerType, "post")
//
// Saving through the relation sets field. A foreign key cannot point at several
// tables, so migrations declare none.
func (r *RelationBuilder) Morph[V comparable](field *V, value V) *RelationBuilder {
	if r.relation == nil {
		return r
	}

	f, model, err := fieldOf(field)
	if err != nil {
		r.fail(err)

		return r
	}

	r.relation.MorphField, r.relation.MorphValue = f, value

	r.relation.Scope = append(
		r.relation.Scope,
		qb.Compare{Left: model.Col(f), Op: qb.OpEq, Right: qb.Param{Value: value}},
	)

	return r
}

// Latest makes a has-one relation the newest of many, by field: Include reads
// that row for each parent.
func (r *RelationBuilder) Latest[V Ordered](field *V) *RelationBuilder {
	return r.pick(Col(field).Desc())
}

// Oldest makes a has-one relation the oldest of many, by field.
func (r *RelationBuilder) Oldest[V Ordered](field *V) *RelationBuilder {
	return r.pick(Col(field).Asc())
}

func (r *RelationBuilder) pick(by Sort) *RelationBuilder {
	if by.err != nil {
		r.fail(by.err)
	} else if r.relation != nil {
		r.relation.Pick = &by.order
	}

	return r
}

// Touch updates the parent's UpdatedAt whenever SaveChanges writes this child, as
// a comment makes its post look recently active. Set it on the child's relation to
// the parent.
func (r *RelationBuilder) Touch() *RelationBuilder {
	if r.relation != nil {
		r.relation.Touch = true
	}

	return r
}

// Inverse fills a pointer on each loaded child back to its parent, so a Post from
// Include(&U.Posts) has its User without another query.
func (r *RelationBuilder) Inverse[P any](back **P) *RelationBuilder {
	inverse, err := relation(back)
	if err != nil {
		r.fail(err)
	} else if r.relation != nil {
		r.relation.Inverse = inverse
	}

	return r
}

// ConstraintName names the foreign key, in place of table_column_fkey.
func (r *RelationBuilder) ConstraintName(name string) *RelationBuilder {
	if r.relation != nil {
		r.relation.ConstraintName = name
	}

	return r
}

// NoForeignKey keeps the relation in queries and leaves its foreign key out of
// migrations. It suits a relation across databases, or a very busy table.
func (r *RelationBuilder) NoForeignKey() *RelationBuilder {
	if r.relation != nil {
		r.relation.NoForeignKey = true
	}

	return r
}

// NoIndex leaves out the index migrations otherwise add on the foreign key.
func (r *RelationBuilder) NoIndex() *RelationBuilder {
	if r.relation != nil {
		r.relation.NoIndex = true
	}

	return r
}
