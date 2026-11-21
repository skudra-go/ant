// Package meta is the model registry. It reflects each struct once and maps
// probe field addresses to columns.
package meta

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"fmt"
	"maps"
	"math"
	"reflect"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode"
	"unsafe"

	"github.com/skudra-go/ant/internal/errs"
	"github.com/skudra-go/ant/qb"
)

// Field is a mapped column.
type Field struct {
	Name    string       // Go path, such as "Base.ID"
	Short   string       // Go name, such as "ID"
	Column  string       // database column
	Type    reflect.Type // Go type
	Offset  uintptr      // offset from the start of the model
	Primary bool
	Unique  bool
	// Generated means the database assigns the value when the field is zero.
	// This is the only place ant reads meaning into a zero value.
	Generated bool
	// Computed fields are never written, and are read only when a query asks.
	Computed bool
	// ReadOnly fields are filled by the database and never written.
	ReadOnly bool
	// WriteOnly fields are written and never read, such as password hashes.
	WriteOnly bool
	// Default fields use the column's default when zero, as generated keys do.
	Default bool
	// Immutable fields are written on insert and never updated.
	Immutable bool
	// Token fields are compared on update and delete: the row must still hold
	// the value that was loaded.
	Token bool
	// Version is a token that goes up by one with every write. It must be a
	// signed integer.
	Version bool
	// Named means the column name was set explicitly, so naming leaves it alone.
	Named bool
	// Prefix is what an embedded struct adds to the column name.
	Prefix string
	// Sensitive values stay out of logs, even with LogValues.
	Sensitive bool
	// Generator makes a key for a new entity in Go.
	Generator func(entity unsafe.Pointer)
	// Equal compares two values of this field for change tracking, in place of
	// the default deep comparison.
	Equal func(a, b unsafe.Pointer) bool
	// Storage is how migrations declare the column.
	Storage Storage
	// Indirect means a copy of the value can share memory with it, as a pointer
	// or a slice does, so a snapshot copies what it refers to.
	Indirect bool
	// Comparer is the change tracker's comparison of two values of Type, kept
	// here so that it is found once and not per row.
	Comparer atomic.Pointer[func(a, b unsafe.Pointer) bool]

	pointer func(unsafe.Pointer) any
	// utc moves a scanned value of the field to UTC, nil if it holds no time.
	utc func(unsafe.Pointer)
	// read makes the Scanner the field is read through, nil for none (see
	// ReaderFor).
	read func(unsafe.Pointer) any
	// driverOnly is DriverOnly.
	driverOnly bool
	// index is the field's position in its model's Fields.
	index int
	// key reads the field as Key returns it, for the types that need no
	// reflection, or is nil.
	key func(unsafe.Pointer) (any, bool)
	// intKey reads an integer key as an int64, or is nil.
	intKey func(unsafe.Pointer) (int64, bool)
	// copy copies a value of a common type with no reflection, or is nil.
	copy func(dst, src unsafe.Pointer)
}

// Storage is how a column is declared in migrations. Queries ignore it. Unset
// values come from the Go type.
type Storage struct {
	Type       string // database type, used as written
	Length     int
	Fixed      bool // Length is exact, not a limit
	Precision  int
	Scale      int
	Nullable   *bool
	Default    string // SQL expression
	Generated  string // SQL expression of a stored generated column
	Collation  string
	UniqueName string
	Comment    string
	Sequence   *Sequence
	Enum       *Enum
}

// Sequence is a database sequence a column draws from.
type Sequence struct {
	Name      string
	Start     int64
	Increment int64
}

// Enum is a native enum type a column uses.
type Enum struct {
	Name   string
	Values []string
}

// FromDatabase reports whether the database fills this field when it is zero.
func (f *Field) FromDatabase() bool { return f.Generated || f.Default }

// Addr returns a pointer to this field of the model value at entity, typed as
// *Field.Type. Offsets come from reflect, so the result stays within entity.
func (f *Field) Addr(entity unsafe.Pointer) any {
	return f.pointer(unsafe.Add(entity, f.Offset))
}

// Value returns the value of this field of the model value at entity.
func (f *Field) Value(entity unsafe.Pointer) reflect.Value {
	return reflect.NewAt(f.Type, unsafe.Add(entity, f.Offset)).Elem()
}

// Key returns this field of the model value at entity in a form that compares
// equal across keys and foreign keys of different Go types, or false if NULL.
func (f *Field) Key(entity unsafe.Pointer) (any, bool) {
	return f.KeyAt(unsafe.Add(entity, f.Offset))
}

// KeyAt is Key for the value at p, which holds this field's value wherever it
// is kept, as in a snapshot.
func (f *Field) KeyAt(p unsafe.Pointer) (any, bool) {
	if f.key != nil {
		return f.key(p)
	}

	return keyOf(reflect.NewAt(f.Type, p).Elem())
}

// CopyAt copies the value of this field at src to dst, which hold values of its
// type. The common types are copied with no reflection.
func (f *Field) CopyAt(dst, src unsafe.Pointer) {
	if f.copy != nil {
		f.copy(dst, src)

		return
	}

	reflect.NewAt(f.Type, dst).Elem().Set(reflect.NewAt(f.Type, src).Elem())
}

// intKeyReader returns the function that reads a key of integer type t as an
// int64 with no reflection, or nil if t is not one. Only types that keyOf widens
// to int64 as they are have one: they implement no driver.Valuer. The bool is
// false for a nil pointer, which is NULL. A uint or uint64 past math.MaxInt64
// comes out negative, which IntKey and keyReader turn back into its uint64.
func intKeyReader(t reflect.Type) func(unsafe.Pointer) (int64, bool) {
	switch t {
	case reflect.TypeFor[int64]():
		return func(p unsafe.Pointer) (int64, bool) { return *(*int64)(p), true }
	case reflect.TypeFor[int]():
		return func(p unsafe.Pointer) (int64, bool) { return int64(*(*int)(p)), true }
	case reflect.TypeFor[int32]():
		return func(p unsafe.Pointer) (int64, bool) { return int64(*(*int32)(p)), true }
	case reflect.TypeFor[uint]():
		return func(p unsafe.Pointer) (int64, bool) { return int64(*(*uint)(p)), true }
	case reflect.TypeFor[uint32]():
		return func(p unsafe.Pointer) (int64, bool) { return int64(*(*uint32)(p)), true }
	case reflect.TypeFor[uint64]():
		return func(p unsafe.Pointer) (int64, bool) { return int64(*(*uint64)(p)), true }
	case reflect.TypeFor[*int64]():
		return func(p unsafe.Pointer) (int64, bool) {
			if v := *(**int64)(p); v != nil {
				return *v, true
			}

			return 0, false
		}
	case reflect.TypeFor[*int]():
		return func(p unsafe.Pointer) (int64, bool) {
			if v := *(**int)(p); v != nil {
				return int64(*v), true
			}

			return 0, false
		}
	}

	return nil
}

// keyReader returns the function that reads a key of type t with no reflection,
// which saves two allocations per row, or nil.
func keyReader(t reflect.Type) func(unsafe.Pointer) (any, bool) {
	if read := intKeyReader(t); read != nil {
		unsigned := isUnsigned(t)

		return func(p unsafe.Pointer) (any, bool) {
			n, ok := read(p)
			if !ok {
				return nil, false
			}

			// A uint past math.MaxInt64 read as an int64 is negative: it is the
			// uint64 keyOf gives it as, not that int64 (#206).
			if n < 0 && unsigned {
				return uint64(n), true
			}

			return n, true
		}
	}

	switch t {
	case reflect.TypeFor[string]():
		return func(p unsafe.Pointer) (any, bool) { return *(*string)(p), true }
	case reflect.TypeFor[*string]():
		return func(p unsafe.Pointer) (any, bool) {
			if v := *(**string)(p); v != nil {
				return *v, true
			}

			return nil, false
		}
	}

	return nil
}

// Indirect reports whether a value of t can share memory with its copy.
func Indirect(t reflect.Type) bool {
	if t == timeType {
		return false
	}

	switch t.Kind() {
	case reflect.Pointer, reflect.Slice, reflect.Map, reflect.Interface:
		return true
	case reflect.Struct:
		for sf := range t.Fields() {
			if sf.IsExported() && Indirect(sf.Type) {
				return true
			}
		}
	case reflect.Array:
		return Indirect(t.Elem())
	}

	return false
}

// KeyValue returns v in the form Field.Key uses.
func KeyValue(v any) (any, bool) { return keyOf(reflect.ValueOf(v)) }

func keyOf(v reflect.Value) (any, bool) {
	if !v.IsValid() {
		return nil, false
	}

	for v.Kind() == reflect.Pointer {
		if v.IsNil() {
			return nil, false
		}

		v = v.Elem()
	}

	if valuer, ok := v.Interface().(driver.Valuer); ok {
		value, err := valuer.Value()
		if err != nil || value == nil {
			return nil, false
		}

		return keyOf(reflect.ValueOf(value))
	}

	switch {
	case v.CanInt():
		return v.Int(), true
	case v.CanUint():
		// A key that fits is an int64, as a signed one is, so an int64 foreign
		// key finds a uint64 key. One past math.MaxInt64 stays a uint64: as an
		// int64 it wrapped, and that value was written to foreign keys (#206).
		if u := v.Uint(); u > math.MaxInt64 {
			return u, true
		}

		return int64(v.Uint()), true
	case v.Kind() == reflect.String:
		return v.String(), true
	case v.Kind() == reflect.Slice && v.Type().Elem().Kind() == reflect.Uint8:
		return string(v.Bytes()), true
	case v.Kind() == reflect.Struct && v.Type().ConvertibleTo(timeType):
		// One instant is one key, whatever its zone or monotonic reading, to the
		// microsecond, which is what PostgreSQL and MySQL keep and what ant's
		// own timestamps hold: a key read back is the one that was written.
		t, _ := reflect.TypeAssert[time.Time](v.Convert(timeType))

		return t.UTC().Truncate(time.Microsecond), true
	}

	return v.Interface(), true
}

// pointerTo returns the function that types a field pointer. Common types need
// no reflection, which saves an allocation per column per row.
func pointerTo(t reflect.Type) func(unsafe.Pointer) any {
	if f, ok := pointers[t]; ok {
		return f
	}

	return func(p unsafe.Pointer) any { return reflect.NewAt(t, p).Interface() }
}

// copiers copy values of the common types with no reflection.
var copiers = func() map[reflect.Type]func(dst, src unsafe.Pointer) {
	out := map[reflect.Type]func(dst, src unsafe.Pointer){}

	for t := range pointers {
		switch t {
		case reflect.TypeFor[string]():
			out[t] = copier[string]
		case reflect.TypeFor[int]():
			out[t] = copier[int]
		case reflect.TypeFor[int16]():
			out[t] = copier[int16]
		case reflect.TypeFor[int32]():
			out[t] = copier[int32]
		case reflect.TypeFor[int64]():
			out[t] = copier[int64]
		case reflect.TypeFor[uint]():
			out[t] = copier[uint]
		case reflect.TypeFor[uint32]():
			out[t] = copier[uint32]
		case reflect.TypeFor[uint64]():
			out[t] = copier[uint64]
		case reflect.TypeFor[bool]():
			out[t] = copier[bool]
		case reflect.TypeFor[float32]():
			out[t] = copier[float32]
		case reflect.TypeFor[float64]():
			out[t] = copier[float64]
		case reflect.TypeFor[[]byte]():
			out[t] = copier[[]byte]
		case reflect.TypeFor[time.Time]():
			out[t] = copier[time.Time]
		case reflect.TypeFor[*string]():
			out[t] = copier[*string]
		case reflect.TypeFor[*int]():
			out[t] = copier[*int]
		case reflect.TypeFor[*int32]():
			out[t] = copier[*int32]
		case reflect.TypeFor[*int64]():
			out[t] = copier[*int64]
		case reflect.TypeFor[*bool]():
			out[t] = copier[*bool]
		case reflect.TypeFor[*float64]():
			out[t] = copier[*float64]
		case reflect.TypeFor[*time.Time]():
			out[t] = copier[*time.Time]
		}
	}

	return out
}()

func copier[T any](dst, src unsafe.Pointer) { *(*T)(dst) = *(*T)(src) }

func typed[T any]() (reflect.Type, func(unsafe.Pointer) any) {
	return reflect.TypeFor[T](), func(p unsafe.Pointer) any { return (*T)(p) }
}

var pointers = func() map[reflect.Type]func(unsafe.Pointer) any {
	out := map[reflect.Type]func(unsafe.Pointer) any{}
	add := func(t reflect.Type, f func(unsafe.Pointer) any) { out[t] = f }
	add(typed[string]())
	add(typed[int]())
	add(typed[int16]())
	add(typed[int32]())
	add(typed[int64]())
	add(typed[uint]())
	add(typed[uint32]())
	add(typed[uint64]())
	add(typed[bool]())
	add(typed[float32]())
	add(typed[float64]())
	add(typed[[]byte]())
	add(typed[time.Time]())
	add(typed[*string]())
	add(typed[*int]())
	add(typed[*int32]())
	add(typed[*int64]())
	add(typed[*bool]())
	add(typed[*float64]())
	add(typed[*time.Time]())

	return out
}()

// RelationKind is the shape of a relation field.
type RelationKind int

// Relation kinds.
const (
	Many RelationKind = iota // []T
	One                      // *T
)

// Action is what the database does to dependent rows.
type Action int

// Slot returns a pointer to the relation field of the model value at entity.
func (r *Relation) Slot(entity unsafe.Pointer) unsafe.Pointer {
	return unsafe.Add(entity, r.Offset)
}

// Related returns the entities this relation holds on the model value at
// entity.
func (r *Relation) Related(entity unsafe.Pointer) []unsafe.Pointer {
	if r.Kind == One {
		if p := *(*unsafe.Pointer)(r.Slot(entity)); p != nil {
			return []unsafe.Pointer{p}
		}

		return nil
	}

	v := reflect.NewAt(r.Type, r.Slot(entity)).Elem()

	out := make([]unsafe.Pointer, v.Len())
	for i := range out {
		out[i] = v.Index(i).Addr().UnsafePointer()
	}

	return out
}

// Relation is a field that refers to another model and is not a column.
type Relation struct {
	Name   string
	Kind   RelationKind
	Target reflect.Type
	Type   reflect.Type
	Offset uintptr
	Owner  *Model
	// Declarer is the struct the field is declared in: the owner's type, or a
	// struct it embeds, whose relations it inherits.
	Declarer reflect.Type

	ForeignKey *Field // set by configuration, on either side
	References *Field // set by configuration
	OnDelete   Action
	OnUpdate   Action
	Pivot      *Pivot // set for many-to-many, by configuration

	// ConstraintName, NoForeignKey, and NoIndex shape what migrations declare.
	ConstraintName string
	NoForeignKey   bool
	NoIndex        bool

	// Scope holds conditions on the target that every use of the relation applies.
	Scope []qb.Cond
	// MorphField and MorphValue make the relation polymorphic: MorphField says
	// which model a row's foreign key refers to. It is on whichever side holds the
	// foreign key, and has no database constraint.
	MorphField *Field
	MorphValue any
	// Pick chooses one row of many for a has-one relation.
	Pick *qb.Order
	// ThroughKey is the column on the target that refers to the intermediate
	// table of a has-many-through or has-one-through relation.
	ThroughKey *Field
	// Touch updates the parent's UpdatedAt when a child is written.
	Touch bool
	// Inverse is the relation on the target that points back at the owner, filled
	// when this relation loads.
	Inverse *Relation

	link     Link
	linkErr  error
	linkOnce sync.Once
}

// Pivot is the join table of a many-to-many relation.
type Pivot struct {
	Table   *qb.TableRef
	Local   string // column referring to the relation's owner
	Foreign string // column referring to its target
	// ReadOnly means the table is a model of its own, reached in passing by a
	// through relation, and is never written through it.
	ReadOnly bool
}

// Link is how a relation's rows connect. Local is on the relation's owner, and
// Foreign is on its target. BelongsTo means the owner holds the foreign key.
type Link struct {
	Target    *Model
	Local     *Field
	Foreign   *Field
	BelongsTo bool
}

// Link resolves the relation from configuration and conventions, once.
// Configure both models first.
func (r *Relation) Link() (Link, error) {
	r.linkOnce.Do(func() { r.link, r.linkErr = r.resolve() })

	return r.link, r.linkErr
}

func (r *Relation) resolve() (Link, error) {
	target := ofType(r.Target)

	missing := fmt.Errorf("%w: %s.%s", errs.ErrNoForeignKey, r.Owner.Type.Name(), r.Name)

	if p := r.Pivot; p != nil {
		local, foreign := r.Owner.Key(), target.Key()
		if local == nil || (foreign == nil && r.ThroughKey == nil) {
			return Link{}, missing
		}

		if p.Local == "" {
			p.Local = ColumnName(r.Owner.Type.Name()) + "_id"
		}

		if p.Foreign == "" {
			p.Foreign = ColumnName(target.Type.Name()) + "_id"
		}

		// Checked once the defaults are in: a join table with no name joined
		// "", and a model linked to itself took one column for both sides, so
		// only its links to itself matched (#203).
		switch {
		case p.ReadOnly: // a through relation's table, a model of its own
		case p.Table.Name == "":
			return Link{}, fmt.Errorf(
				"%w: %s.%s is many-to-many with no join table: name it with Through",
				errs.ErrBadSchema,
				r.Owner.Type.Name(),
				r.Name,
			)
		case p.Local == p.Foreign:
			return Link{}, fmt.Errorf(
				"%w: %s.%s uses %s for both sides of its join table: name them with PivotColumns",
				errs.ErrBadSchema,
				r.Owner.Type.Name(),
				r.Name,
				p.Local,
			)
		}

		if r.ThroughKey != nil {
			foreign = r.ThroughKey
		}

		return Link{Target: target, Local: local, Foreign: foreign}, nil
	}

	child := func(fk *Field) (Link, error) {
		local := r.References
		if local == nil {
			local = r.Owner.Key()
		}

		if local == nil {
			return Link{}, missing
		}

		return Link{Target: target, Local: local, Foreign: fk}, nil
	}

	parent := func(fk *Field) (Link, error) {
		foreign := r.References
		if foreign == nil {
			foreign = target.Key()
		}

		if foreign == nil {
			return Link{}, missing
		}

		return Link{
			Target:    target,
			Local:     fk,
			Foreign:   foreign,
			BelongsTo: true,
		}, nil
	}

	if fk := r.ForeignKey; fk != nil {
		if r.Kind == One && r.Owner.Has(fk) {
			return parent(fk)
		}

		return child(fk)
	}

	if r.Kind == One {
		if fk := r.Owner.field(r.Name + "ID"); fk != nil {
			return parent(fk)
		}
	}

	// A relation inherited from an embedded model, as User.Posts in a model that
	// embeds User, keeps the key its own model's conventions give it.
	owners := []reflect.Type{r.Owner.Type}

	if r.Declarer != nil && r.Declarer != r.Owner.Type {
		owners = append(owners, r.Declarer)
	}

	for _, owner := range owners {
		if fk := target.field(owner.Name() + "ID"); fk != nil {
			if err := r.shared(target, fk, false); err != nil {
				return Link{}, err
			}

			return child(fk)
		}
	}

	if r.Kind == Many {
		for _, owner := range owners {
			if fk := target.backReference(owner); fk != nil {
				if err := r.shared(target, fk, true); err != nil {
					return Link{}, err
				}

				return child(fk)
			}
		}
	}

	return Link{}, missing
}

// shared refuses a foreign key that convention gives to r and to another
// relation of the same owner: the two would read and write each other's rows.
// A relation that names its key, or that scopes or picks rows of the target,
// is not a guess. manyOnly is for a key only a has-many can be given.
func (r *Relation) shared(target *Model, fk *Field, manyOnly bool) error {
	if !r.guessed() {
		return nil
	}

	for _, other := range r.Owner.Relations {
		if other == r || other.Target != r.Target || !other.guessed() {
			continue
		}

		if other.Kind == One && (manyOnly || r.Owner.field(other.Name+"ID") != nil) {
			continue // it is found another way, or not at all
		}

		return fmt.Errorf(
			"%w: %s.%s and %s.%s would both use %s.%s; set ForeignKey on at least one of them",
			errs.ErrNoForeignKey,
			r.Owner.Type.Name(),
			r.Name,
			r.Owner.Type.Name(),
			other.Name,
			target.Type.Name(),
			fk.Name,
		)
	}

	return nil
}

// guessed reports whether only conventions say which key the relation uses.
func (r *Relation) guessed() bool {
	return r.ForeignKey == nil && r.Pivot == nil && len(r.Scope) == 0 && r.Pick == nil
}

// backReference returns the foreign key of the only relation from m to owner.
func (m *Model) backReference(owner reflect.Type) *Field {
	var found *Field

	for _, other := range m.Relations {
		if other.Kind != One || other.Target != owner {
			continue
		}

		fk := other.ForeignKey
		if fk == nil {
			fk = m.field(other.Name + "ID")
		}

		if fk == nil || !m.Has(fk) {
			continue
		}

		if found != nil {
			return nil
		}

		found = fk
	}

	return found
}

// Empty reports whether this relation holds nothing on the model value at
// entity.
func (r *Relation) Empty(entity unsafe.Pointer) bool {
	v := reflect.NewAt(r.Type, unsafe.Add(entity, r.Offset)).Elem()

	if r.Kind == Many {
		return v.Len() == 0
	}

	return v.IsNil()
}

// Model is the metadata for one struct type.
type Model struct {
	Type  reflect.Type
	Table *qb.TableRef // shared by AST nodes, so a rename reaches them
	// Named means the table name was set explicitly, rather than by convention.
	Named     bool
	Fields    []*Field
	Relations []*Relation

	// Keys holds the primary key, of several fields for a composite one.
	Keys []*Field
	// ReadOnly models, such as views, refuse writes.
	ReadOnly bool
	// Created and Updated are maintained on writes.
	Created, Updated *Field
	// Discriminator and DiscriminatorValue mark a type sharing its table: rows of
	// this model hold that value, and queries see only those.
	Discriminator      *Field
	DiscriminatorValue any
	// Version counts writes, and detects conflicting ones.
	Version *Field
	// SoftDelete marks rows deleted instead of removing them.
	SoftDelete *Field
	// Filters apply to every query of this model.
	Filters []Filter
	// Rules check an entity's fields before it is written.
	Rules []Rule
	// Indexes and Checks are declared by migrations.
	Indexes []*Index
	Checks  []Check
	// KeyName overrides the name of the primary key constraint.
	KeyName string
	// Unmanaged models have a table that migrations leave alone.
	Unmanaged bool
	// Comment describes the table in the database.
	Comment string
	// Partition splits the table.
	Partition *Partition
	// Constraints are foreign keys declared on their own, for those no relation
	// can express, such as a key of several columns.
	Constraints []*ForeignKey

	columns   map[string]*Field
	configure sync.Once
	err       error
	// frozen is set once configuration is over and the fields no longer change,
	// and lists holds what is derived from them from then on.
	frozen   atomic.Bool
	lists    atomic.Pointer[fieldLists]
	slice    atomic.Pointer[reflect.Type]
	snapshot atomic.Pointer[Snapshot]
}

// Snapshot is the layout of the change tracker's copy of an entity: every
// column but the computed ones, packed, with no relations. Computed values are
// never written, and relations are compared through the tracker's records of
// them, so a snapshot of a model with relations is smaller than the entity.
type Snapshot struct {
	Type   reflect.Type // a struct of the fields' types
	Slice  reflect.Type // a slice of Type, which blocks of snapshots are made of
	Fields []*Field     // the fields kept, in the model's order
	Shared []*Field     // those whose values can share memory with a copy

	offsets []uintptr // by field index; meaningful for the fields kept
	kept    []bool    // by field index
}

// Holds reports whether the snapshot keeps f.
func (s *Snapshot) Holds(f *Field) bool { return f.index < len(s.kept) && s.kept[f.index] }

// At returns the address of f's value in the snapshot at p. f must be kept.
func (s *Snapshot) At(p unsafe.Pointer, f *Field) unsafe.Pointer {
	return unsafe.Add(p, s.offsets[f.index])
}

// Value returns f's value in the snapshot at p, or the zero Value if f is not
// kept.
func (s *Snapshot) Value(p unsafe.Pointer, f *Field) reflect.Value {
	if !s.Holds(f) {
		return reflect.Value{}
	}

	return reflect.NewAt(f.Type, s.At(p, f)).Elem()
}

// Key returns the model's key as KeyOf does, read from the snapshot at p.
func (s *Snapshot) Key(m *Model, p unsafe.Pointer) (any, bool) {
	return m.keyFrom(func(f *Field) (any, bool) {
		if !s.Holds(f) {
			return nil, false
		}

		return f.KeyAt(s.At(p, f))
	})
}

// SnapshotLayout returns the layout of the model's snapshots. Whether a field
// is computed is fixed when the model is built, so it never changes.
func (m *Model) SnapshotLayout() *Snapshot {
	if s := m.snapshot.Load(); s != nil {
		return s
	}

	s := &Snapshot{
		offsets: make([]uintptr, len(m.Fields)),
		kept:    make([]bool, len(m.Fields)),
	}

	var fields []reflect.StructField

	for _, f := range m.Fields {
		if f.Computed {
			continue
		}

		fields = append(fields, reflect.StructField{
			Name: "F" + strconv.Itoa(len(fields)),
			Type: f.Type,
		})

		s.Fields = append(s.Fields, f)

		if f.Indirect {
			s.Shared = append(s.Shared, f)
		}
	}

	s.Type = reflect.StructOf(fields)
	s.Slice = reflect.SliceOf(s.Type)

	for i, f := range s.Fields {
		s.offsets[f.index], s.kept[f.index] = s.Type.Field(i).Offset, true
	}

	m.snapshot.CompareAndSwap(nil, s)

	return m.snapshot.Load()
}

// fieldLists holds the field lists a model is asked for on every query and
// every tracked row. Each is clipped, so that appending to one copies it.
type fieldLists struct {
	stored, writable, updatable, tokens, indirect []*Field
	// selection is the select list of the stored columns, and self the model
	// alone, as the list of models a condition on one of its columns refers to.
	selection []qb.Selected
	self      []*Model
	// columns holds each field's column, in m.Fields order, already in a
	// qb.Expr, so a condition on it puts nothing new on the heap.
	columns []qb.Expr
}

// Partition says how a table is split, declared with b.PartitionBy.
type Partition struct {
	Method  string
	Columns []*Field
}

// ForeignKey is a foreign key declared with b.ForeignKey.
type ForeignKey struct {
	Name     string
	Columns  []*Field
	Target   *Model
	Refs     []*Field
	OnDelete Action
	OnUpdate Action
	NoIndex  bool
}

// Rule is a validation of one field. Check returns an error for a bad value.
type Rule struct {
	Field *Field
	Check func(entity unsafe.Pointer) error
}

// Validators returns the model's field rules.
func (m *Model) Validators() []Rule { return m.Rules }

// Index is an index declared in Configure.
type Index struct {
	Name       string
	Parts      []IndexPart
	Unique     bool
	Method     string
	Where      string
	Include    []*Field
	Concurrent bool
}

// IndexPart is one indexed column or expression.
type IndexPart struct {
	Field *Field
	Expr  string
	Desc  bool
	// NullsFirst, when set, says whether NULLs come first, over the direction's
	// default.
	NullsFirst *bool
}

// Check is a check constraint declared in Configure.
type Check struct {
	Name string
	Expr string
}

// Filter is a global query filter. A nil condition from Build matches every row.
type Filter struct {
	Name  string
	Build func(context.Context) (qb.Cond, error)
	// Static means Build gives the same condition whatever the context, with
	// no bound values, so the SQL of a query that applies it can be kept.
	Static bool
}

// SoftDeleteFilter names the built-in filter of a soft-deleted model.
const SoftDeleteFilter = "ant.soft_delete"

// DeletedAt is the type of a soft delete field. A model with one is deleted by
// setting it, and queries skip rows where it is set.
type DeletedAt struct {
	sql.Null[time.Time]
}

// UnixTime is a time stored as whole seconds since 1970, for a schema that keeps
// integers. On CreatedAt and UpdatedAt, it is maintained like a time.Time. The
// zero time is stored as 0, so 0, and the Unix epoch with it, reads back as the
// zero time.
type UnixTime struct {
	time.Time
}

// ColumnType implements schema.Typed.
func (UnixTime) ColumnType(string) string { return "bigint" }

// Value implements driver.Valuer.
func (t UnixTime) Value() (driver.Value, error) {
	if t.IsZero() {
		return int64(0), nil
	}

	return t.Unix(), nil
}

// Scan implements sql.Scanner.
func (t *UnixTime) Scan(src any) error {
	var seconds sql.NullInt64
	if err := seconds.Scan(src); err != nil {
		return err
	}

	t.Time = time.Time{}

	if seconds.Int64 != 0 {
		t.Time = time.Unix(seconds.Int64, 0).UTC()
	}

	return nil
}

func (m *Model) field(short string) *Field {
	for _, f := range m.Fields {
		if f.Short == short {
			return f
		}
	}

	return nil
}

// Has reports whether f is a field of m.
func (m *Model) Has(f *Field) bool { return slices.Contains(m.Fields, f) }

// Configure runs f once, recording the error it returns.
func (m *Model) Configure(f func() error) {
	m.configure.Do(func() {
		if m.err == nil {
			m.err = f()
		}

		if m.err == nil {
			m.err = m.distinctColumns()
		}

		if m.err == nil {
			m.err = m.roles()
		}

		m.frozen.Store(true)
	})
}

// roles refuses a field whose tags and configuration ask ant for two things it
// can't both do, and two fields that take a role a model has once, of which ant
// kept the last without a word (#208). It runs once Configure has, since
// .Generated makes a column readonly, and .Unique and .Default add to the tags.
func (m *Model) roles() error {
	var version, deleted *Field

	for _, f := range m.Fields {
		if a, b, why := conflict(f); a != "" {
			return fmt.Errorf(
				"%w: %s.%s: %s and %s can't go together: %s",
				errs.ErrBadSchema,
				m.Type.Name(),
				f.Name,
				a,
				b,
				why,
			)
		}

		for _, role := range []struct {
			has   bool
			first **Field
			what  string
		}{
			{f.Version, &version, "version fields: a model has one version"},
			{f.Type == deletedAtType, &deleted, "ant.DeletedAt fields: a model has one soft delete"},
		} {
			if !role.has {
				continue
			}

			if *role.first != nil {
				return fmt.Errorf(
					"%w: %s.%s and %s.%s are both %s",
					errs.ErrBadSchema,
					m.Type.Name(),
					(*role.first).Name,
					m.Type.Name(),
					f.Name,
					role.what,
				)
			}

			*role.first = f
		}
	}

	return nil
}

// conflict returns two keys of f that ask for opposite things, and why, or ""
// if it has none. readonly, computed and a generated column are never written,
// and writeonly is never read.
func conflict(f *Field) (a, b, why string) {
	unwritten := ""

	switch {
	case f.Computed:
		unwritten = "computed"
	case f.Storage.Generated != "":
		unwritten = "a generated column"
	case f.ReadOnly:
		unwritten = "readonly"
	}

	check := "token"

	if f.Version {
		check = "version"
	}

	switch {
	case unwritten != "" && f.WriteOnly:
		return unwritten, "writeonly", "the field would never be written or read"
	case unwritten != "" && f.Immutable:
		return unwritten, "immutable", "immutable is written on insert, and " + unwritten + " never is"
	case f.Version && unwritten != "":
		return "version", unwritten, "ant writes the version on every update"
	case f.Version && f.Immutable:
		return "version", "immutable", "ant writes the version on every update"
	case f.Token && f.WriteOnly:
		return check, "writeonly", "ant reads the " + check + " back to compare it"
	case f.Computed && f.Token:
		return "computed", "token", "a computed field has no column to compare"
	case f.Computed && f.Unique:
		return "computed", "unique", "a computed field has no column to keep unique"
	case f.Computed && f.Default:
		return "computed", "default", "a computed field has no column to give a default"
	case f.Computed && f.Storage.Generated != "":
		return "computed", ".Generated", "a computed field has no column to generate"
	}

	return "", "", ""
}

// distinctColumns refuses two fields that map to one column, which kept the
// last one and never read or wrote the other (#99). It runs once the names are
// final: tags, naming conventions and configuration all give them.
func (m *Model) distinctColumns() error {
	seen := make(map[string]*Field, len(m.Fields))

	for _, f := range m.Fields {
		if f.Computed {
			continue
		}

		if other, ok := seen[f.Column]; ok {
			return fmt.Errorf(
				"%w: %s.%s and %s.%s both map to column %s: name one with ant:\"column=...\"",
				errs.ErrBadSchema,
				m.Type.Name(),
				other.Name,
				m.Type.Name(),
				f.Name,
				f.Column,
			)
		}

		seen[f.Column] = f
	}

	return nil
}

// derived returns the model's field lists. They are built once configuration is
// over, and on every call until then, while the fields may still change. The
// lists are shared, so a caller must not change one in place.
func (m *Model) derived() *fieldLists {
	if l := m.lists.Load(); l != nil {
		return l
	}

	l := new(fieldLists)

	for _, f := range m.Fields {
		if !f.Computed && !f.WriteOnly {
			l.stored = append(l.stored, f)
		}

		if !f.Computed && !f.ReadOnly {
			l.writable = append(l.writable, f)

			if !f.Primary && !f.Immutable && f != m.Created {
				l.updatable = append(l.updatable, f)
			}
		}

		if f.Token {
			l.tokens = append(l.tokens, f)
		}

		if f.Indirect {
			l.indirect = append(l.indirect, f)
		}
	}

	l.stored = slices.Clip(l.stored)
	l.writable = slices.Clip(l.writable)
	l.updatable = slices.Clip(l.updatable)
	l.tokens = slices.Clip(l.tokens)
	l.indirect = slices.Clip(l.indirect)

	l.selection = make([]qb.Selected, len(l.stored))

	for i, f := range l.stored {
		l.selection[i] = qb.Selected{Value: m.Col(f)}
	}

	l.self = []*Model{m}

	l.columns = make([]qb.Expr, len(m.Fields))

	for i, f := range m.Fields {
		l.columns[i] = m.Col(f)
	}

	if m.frozen.Load() {
		m.lists.Store(l)
	}

	return l
}

// SliceType returns the type of a slice of the model, which blocks of entities
// and of snapshots are made of.
func (m *Model) SliceType() reflect.Type {
	if t := m.slice.Load(); t != nil {
		return *t
	}

	t := reflect.SliceOf(m.Type)
	m.slice.Store(&t)

	return t
}

// Err returns the error from building or configuring the model.
func (m *Model) Err() error { return m.err }

// KeyOf returns the primary key of the model value at entity, false if the
// model has none or part of it is NULL. A composite key comes as a string.
func (m *Model) KeyOf(entity unsafe.Pointer) (any, bool) {
	if len(m.Keys) == 1 {
		return m.Keys[0].Key(entity)
	}

	return m.keyFrom(func(f *Field) (any, bool) { return f.Key(entity) })
}

// keyFrom is KeyOf with each part read by key.
func (m *Model) keyFrom(key func(*Field) (any, bool)) (any, bool) {
	switch len(m.Keys) {
	case 0:
		return nil, false
	case 1:
		return key(m.Keys[0])
	}

	var parts strings.Builder

	for _, f := range m.Keys {
		v, ok := key(f)
		if !ok {
			return nil, false
		}

		text := fmt.Sprint(v)
		fmt.Fprintf(&parts, "%d:%s,", len(text), text)
	}

	return parts.String(), true
}

// IntegerKey reports whether the model has a single key of an integer type.
func (m *Model) IntegerKey() bool {
	return len(m.Keys) == 1 && IsInteger(m.Keys[0].Type)
}

// IntKey reads the model's key as an int64 with no allocation, when it is a
// single key of a plain integer type, which fast says. ok is false if the key is
// NULL. The value is the one KeyOf returns, unboxed.
func (m *Model) IntKey(entity unsafe.Pointer) (n int64, ok, fast bool) {
	if len(m.Keys) != 1 || m.Keys[0].intKey == nil {
		return 0, false, false
	}

	f := m.Keys[0]
	n, ok = f.intKey(unsafe.Add(entity, f.Offset))

	// A uint past math.MaxInt64 has no int64 of its own: KeyOf gives it as a
	// uint64.
	if n < 0 && isUnsigned(f.Type) {
		return 0, false, false
	}

	return n, ok, true
}

// isUnsigned reports whether t is an unsigned integer that can pass
// math.MaxInt64.
func isUnsigned(t reflect.Type) bool {
	return t.Kind() == reflect.Uint || t.Kind() == reflect.Uint64 || t.Kind() == reflect.Uintptr
}

// Key returns the primary key field, or nil if the model has none or the key is
// composite.
func (m *Model) Key() *Field {
	if len(m.Keys) == 1 {
		return m.Keys[0]
	}

	return nil
}

// SetKey makes fields the primary key. A single integer key is generated.
func (m *Model) SetKey(fields ...*Field) {
	for _, other := range m.Fields {
		other.Primary, other.Generated = false, false
	}

	m.Keys = fields

	for _, f := range fields {
		f.Primary = true
	}

	if len(fields) == 1 {
		fields[0].Generated = IsInteger(fields[0].Type)
	}
}

// Col returns the AST column for f.
func (m *Model) Col(f *Field) qb.Column {
	return qb.Column{Table: m.Table, Name: f.Column}
}

// ColumnOf returns the AST column for f as an expression, made once for the
// model and shared, which saves putting a new qb.Column on the heap for every
// condition and ordering on it. It is the same value Col returns.
func (m *Model) ColumnOf(f *Field) qb.Expr {
	if l := m.derived(); f.index < len(l.columns) && m.Fields[f.index] == f {
		return l.columns[f.index]
	}

	return m.Col(f)
}

// Stored returns the fields queries read, in declaration order. This list and
// the ones below are shared: a caller that changes one copies it first.
func (m *Model) Stored() []*Field { return m.derived().stored }

// Writable returns the fields inserts write, in declaration order.
func (m *Model) Writable() []*Field { return m.derived().writable }

// Tokens returns the fields compared on writes.
func (m *Model) Tokens() []*Field { return m.derived().tokens }

// Updatable returns the fields updates can write.
func (m *Model) Updatable() []*Field { return m.derived().updatable }

// Selection returns the select list of the stored columns, as a query with no
// columns chosen or left out reads them.
func (m *Model) Selection() []qb.Selected { return m.derived().selection }

// Self returns the model alone, as the models a condition or an expression on
// one of its columns refers to.
func (m *Model) Self() []*Model { return m.derived().self }

// Shared returns the fields whose values can share memory with a copy, which a
// snapshot copies in depth.
func (m *Model) Shared() []*Field { return m.derived().indirect }

// Column returns the field mapped to the given column name.
func (m *Model) Column(name string) *Field { return m.columns[name] }

// Rename applies naming functions to a model not yet used, keeping names set
// explicitly. Nil functions keep the conventions.
func (m *Model) Rename(table, column func(string) string) {
	if table != nil {
		m.Table.Name = table(m.Type.Name())
	}

	if column == nil {
		return
	}

	clear(m.columns)

	for _, f := range m.Fields {
		if !f.Named {
			f.Column = f.Prefix + column(f.Short)
		}

		m.columns[f.Column] = f
	}
}

// Of returns the metadata for T, building it on first use.
func Of[T any]() *Model {
	return ofType(reflect.TypeFor[T]())
}

// The registry changes once per model type and is then read by every query, from
// every goroutine. Readers load an immutable snapshot and take no lock, so they
// never contend. A writer copies the snapshot, adds to the copy and stores it,
// under mu so that two writers cannot lose each other's addition.
var (
	mu     sync.Mutex
	models atomic.Pointer[map[reflect.Type]*Model]
	probes atomic.Pointer[[]probe]

	// pinner pins every probe for the life of the process. It is never
	// unpinned, and it must stay a package variable: the runtime panics if a
	// Pinner that still holds pinned objects becomes unreachable. It is not
	// safe for concurrent use, so mu guards it.
	pinner runtime.Pinner
)

// loadModels returns the current models. The map must not be written to.
func loadModels() map[reflect.Type]*Model {
	if m := models.Load(); m != nil {
		return *m
	}

	return nil
}

// loadProbes returns the current probes. The slice must not be written to.
func loadProbes() []probe {
	if p := probes.Load(); p != nil {
		return *p
	}

	return nil
}

// probe keeps a probe value reachable, and with it the address range that
// identifies its fields. Probes are pinned with runtime.Pinner, so their
// addresses stay fixed even under a moving collector, and the pin also tells
// the current GC about the pointer arithmetic done on them.
// TestProbeSurvivesGC covers this use.
type probe struct {
	ptr   unsafe.Pointer
	base  uintptr
	model *Model
}

// Probe returns the probe for T, allocating and registering it on first use.
// There is one per type, so calling this in a loop costs nothing and leaks nothing.
func Probe[T any]() *T {
	model := Of[T]()

	if p := probeOf(loadProbes(), model); p != nil {
		return (*T)(p)
	}

	mu.Lock()
	defer mu.Unlock()

	current := loadProbes()

	// Another goroutine may have registered it since the check above.
	if p := probeOf(current, model); p != nil {
		return (*T)(p)
	}

	p := new(T)
	pinner.Pin(p)

	// Clip makes append copy, so readers of current never see it change.
	next := append(
		slices.Clip(current),
		probe{unsafe.Pointer(p), uintptr(unsafe.Pointer(p)), model},
	)
	probes.Store(&next)

	return p
}

// probeOf returns model's probe in list, or nil.
func probeOf(list []probe, model *Model) unsafe.Pointer {
	for _, existing := range list {
		if existing.model == model {
			return existing.ptr
		}
	}

	return nil
}

// Ref is a resolved field pointer.
type Ref struct {
	Model    *Model
	Field    *Field
	Relation *Relation
}

// Resolve identifies a probe field by address and type. The type is needed
// because a struct and its first field share an address.
func Resolve[V any](ptr *V) (Ref, bool) {
	addr := uintptr(unsafe.Pointer(ptr))
	t := reflect.TypeFor[V]()

	for _, p := range loadProbes() {
		if addr < p.base || addr >= p.base+p.model.Type.Size() {
			continue
		}

		off := addr - p.base

		for _, f := range p.model.Fields {
			if f.Offset == off && (f.Type == t || f.deletionTime(t)) {
				return Ref{Model: p.model, Field: f}, true
			}
		}

		for _, r := range p.model.Relations {
			if r.Offset == off && r.Type == t {
				return Ref{Model: p.model, Relation: r}, true
			}
		}
	}

	return Ref{}, false
}

// deletionTime reports whether t is the time inside a DeletedAt field, so that
// &Notes.DeletedAt.V names the column for a condition on when a row was deleted.
func (f *Field) deletionTime(t reflect.Type) bool {
	return f.Type == deletedAtType && t == timeType
}

// OfType returns the metadata for a struct type.
func OfType(t reflect.Type) *Model { return ofType(t) }

func ofType(t reflect.Type) *Model {
	if m := loadModels()[t]; m != nil {
		return m
	}

	m := build(t)

	mu.Lock()
	defer mu.Unlock()

	current := loadModels()

	// Another goroutine may have built it meanwhile. Its model is kept, so
	// every caller shares one.
	if existing := current[t]; existing != nil {
		return existing
	}

	next := make(map[reflect.Type]*Model, len(current)+1)
	maps.Copy(next, current)
	next[t] = m
	models.Store(&next)

	return m
}

func build(t reflect.Type) *Model {
	m := &Model{
		Type:    t,
		Table:   &qb.TableRef{Name: TableName(t.Name())},
		columns: map[string]*Field{},
	}
	m.err = walk(m, t, 0, "", "")

	for _, f := range m.Fields {
		m.columns[f.Column] = f

		switch {
		case (f.Type == timeType || f.Type == unixTimeType) && f.Short == "CreatedAt":
			m.Created = f
		case (f.Type == timeType || f.Type == unixTimeType) && f.Short == "UpdatedAt":
			m.Updated = f
		case f.Type == deletedAtType:
			m.SoftDelete = f
		}

		if f.Version {
			m.Version = f
		}
	}

	for _, f := range m.Fields {
		if f.Name == "ID" || strings.HasSuffix(f.Name, ".ID") {
			m.SetKey(f)

			break
		}
	}

	if soft := m.SoftDelete; soft != nil {
		m.Filters = append(
			m.Filters,
			Filter{
				Name: SoftDeleteFilter,
				Build: func(context.Context) (qb.Cond, error) {
					return qb.IsNull{Left: m.Col(soft)}, nil
				},
				Static: true,
			},
		)
	}

	return m
}

// signedInt reports whether k is one of the signed integer kinds.
func signedInt(k reflect.Kind) bool {
	return k >= reflect.Int && k <= reflect.Int64
}

func walk(m *Model, t reflect.Type, offset uintptr, prefix, columnPrefix string) error {
	for sf := range t.Fields() {
		tag, tagErr := parseTag(sf.Tag.Get("ant"))
		if !sf.IsExported() || tag.skip {
			continue
		}

		off := offset + sf.Offset

		name := prefix + sf.Name

		// A tag ant can't read whole would leave the schema silently wrong:
		// a misspelled unique is no constraint (#98).
		if err := fieldError(m, name, sf, tag, tagErr); err != nil {
			return err
		}

		switch {
		case sf.Anonymous && sf.Type.Kind() == reflect.Struct && !IsValue(sf.Type):
			if err := walk(m, sf.Type, off, name+".", columnPrefix+tag.prefix); err != nil {
				return err
			}

			continue
		case tag.embedded:
			if sf.Type.Kind() != reflect.Struct {
				return fmt.Errorf(
					"ant: embedded field %s.%s must be a struct value, got %s",
					m.Type.Name(),
					name,
					sf.Type,
				)
			}

			if err := walk(m, sf.Type, off, name+".", columnPrefix+tag.prefix); err != nil {
				return err
			}

			continue
		}

		if target, kind, ok := relation(sf.Type); ok {
			m.Relations = append(m.Relations, &Relation{
				Name:     name,
				Kind:     kind,
				Target:   target,
				Type:     sf.Type,
				Offset:   off,
				Owner:    m,
				Declarer: t,
			})

			continue
		}

		column := tag.column
		if column == "" {
			column = ColumnName(sf.Name)
		}

		// A write moves the version on by one, which needs a signed integer.
		if tag.version && !signedInt(sf.Type.Kind()) {
			return fmt.Errorf(
				"ant: version field %s.%s must be a signed integer, got %s; use ant:\"token\" for a value the application manages",
				m.Type.Name(),
				name,
				sf.Type,
			)
		}

		read := ReaderFor(sf.Type)

		m.Fields = append(m.Fields, &Field{
			index:      len(m.Fields),
			Name:       name,
			Short:      sf.Name,
			Column:     columnPrefix + column,
			Type:       sf.Type,
			Offset:     off,
			Named:      tag.column != "",
			Prefix:     columnPrefix,
			Unique:     tag.unique,
			Computed:   tag.computed,
			ReadOnly:   tag.readOnly,
			WriteOnly:  tag.writeOnly,
			Default:    tag.defaulted,
			Immutable:  tag.immutable,
			Token:      tag.token || tag.version,
			Version:    tag.version,
			Storage:    Storage{Default: tag.defaultSQL},
			Indirect:   Indirect(sf.Type),
			pointer:    pointerTo(sf.Type),
			key:        keyReader(sf.Type),
			intKey:     intKeyReader(sf.Type),
			copy:       copiers[sf.Type],
			utc:        utcMovers[sf.Type],
			read:       read,
			driverOnly: read == nil && !sqlConverts(sf.Type),
		})
	}

	return nil
}

// fieldError refuses a field ant would otherwise misread without a word: a tag
// it can't read, a struct embedded by pointer, which became a relation and lost
// its columns, a slice of pointers to a model, which became a column, and a
// kind no database stores (#98, #203, #204).
func fieldError(m *Model, name string, sf reflect.StructField, tag tag, tagErr error) error {
	fail := func(format string, args ...any) error {
		return fmt.Errorf(
			"%w: %s.%s: %s",
			errs.ErrBadSchema,
			m.Type.Name(),
			name,
			fmt.Sprintf(format, args...),
		)
	}

	t := sf.Type

	switch {
	case tagErr != nil:
		return fail("%v", tagErr)
	case tag.prefix != "" && !tag.embedded && !sf.Anonymous:
		return fail("prefix= goes with embedded, as ant:\"embedded,prefix=%s\"", tag.prefix)
	}

	if _, kind, ok := relation(t); ok && kind == One && sf.Anonymous {
		return fail("an embedded struct must be a value: embed %s, not %s", t.Elem().Name(), t)
	}

	embeds := tag.embedded || sf.Anonymous && t.Kind() == reflect.Struct && !IsValue(t)
	if key := tag.columnKey(); embeds && key != "" {
		return fail("an embedded struct has no column of its own for %s: tag its fields", key)
	}

	switch t.Kind() {
	case reflect.Slice:
		if _, _, ok := relation(t); !ok && t.Elem().Kind() == reflect.Pointer &&
			!t.Implements(valuerType) && !reflect.PointerTo(t).Implements(scannerType) &&
			isModel(t.Elem().Elem()) {
			return fail(
				"a relation slice holds values, as []%s, not pointers",
				t.Elem().Elem().Name(),
			)
		}
	case reflect.Func, reflect.Chan, reflect.UnsafePointer:
		return fail("a %s is no column: tag it ant:\"-\"", t.Kind())
	}

	return nil
}

var (
	timeType      = reflect.TypeFor[time.Time]()
	deletedAtType = reflect.TypeFor[DeletedAt]()
	unixTimeType  = reflect.TypeFor[UnixTime]()
	scannerType   = reflect.TypeFor[sql.Scanner]()
	valuerType    = reflect.TypeFor[driver.Valuer]()
)

// relation reports whether t is a relation field: a slice of structs or a
// pointer to a struct. Value types such as time.Time are columns.
func relation(t reflect.Type) (reflect.Type, RelationKind, bool) {
	// A type that converts itself is one column's value, though it is a slice
	// of structs or a pointer to one.
	if t.Implements(valuerType) || reflect.PointerTo(t).Implements(scannerType) {
		return nil, 0, false
	}

	switch t.Kind() {
	case reflect.Slice:
		if isModel(t.Elem()) {
			return t.Elem(), Many, true
		}
	case reflect.Pointer:
		if isModel(t.Elem()) {
			return t.Elem(), One, true
		}
	}

	return nil, 0, false
}

// isModel reports whether t can be a relation's target: a struct that is no
// value, with a field to map. One with no exported field, such as netip.Addr,
// is a value the driver converts, so *netip.Addr is a nullable column.
func isModel(t reflect.Type) bool {
	if IsValue(t) {
		return false
	}

	for sf := range t.Fields() {
		if sf.IsExported() {
			return true
		}
	}

	return false
}

// IsValue reports whether t holds a single column's value rather than a row:
// anything that is not a struct, plus time.Time and types that convert
// themselves, such as sql.Null[T].
func IsValue(t reflect.Type) bool {
	return t.Kind() != reflect.Struct || t == timeType ||
		t.Implements(valuerType) || reflect.PointerTo(t).Implements(scannerType)
}

// IsInteger reports whether t is an integer type.
func IsInteger(t reflect.Type) bool {
	switch t.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return true
	}

	return false
}

type tag struct {
	column     string
	prefix     string
	defaultSQL string
	unique     bool
	skip       bool
	computed   bool
	readOnly   bool
	writeOnly  bool
	defaulted  bool
	immutable  bool
	embedded   bool
	token      bool
	version    bool
}

// columnKey returns the first key of the tag that describes a column, or "".
func (t tag) columnKey() string {
	for _, key := range []struct {
		name string
		set  bool
	}{
		{"unique", t.unique},
		{"computed", t.computed},
		{"readonly", t.readOnly},
		{"writeonly", t.writeOnly},
		{"immutable", t.immutable},
		{"version", t.version},
		{"token", t.token},
		{"default", t.defaulted},
		{"column", t.column != ""},
	} {
		if key.set {
			return key.name
		}
	}

	return ""
}

// tagParts splits a tag on commas, except inside parentheses or quotes, so a
// default can be an expression such as coalesce(a, 0).
func tagParts(s string) []string {
	var parts []string

	depth, start := 0, 0
	quoted := false

	for i, r := range s {
		switch {
		case r == '\'':
			quoted = !quoted
		case quoted:
		case r == '(':
			depth++
		case r == ')':
			depth--
		case r == ',' && depth == 0:
			parts = append(parts, s[start:i])
			start = i + 1
		}
	}

	return append(parts, s[start:])
}

func parseTag(s string) (tag, error) {
	var out tag

	for _, part := range tagParts(s) {
		key, value, valued := strings.Cut(strings.TrimSpace(part), "=")
		key = strings.TrimSpace(key)

		flag := map[string]*bool{
			"-":         &out.skip,
			"unique":    &out.unique,
			"computed":  &out.computed,
			"readonly":  &out.readOnly,
			"writeonly": &out.writeOnly,
			"immutable": &out.immutable,
			"embedded":  &out.embedded,
			"version":   &out.version,
			"token":     &out.token,
		}[key]

		switch {
		case key == "" && !valued: // an empty part, as after a trailing comma
		case flag != nil && valued:
			return out, fmt.Errorf("tag key %s takes no value, got %s=%s", key, key, value)
		case flag != nil:
			*flag = true
		case key == "default":
			out.defaulted = true
			out.defaultSQL = value
		case key == "prefix" || key == "column":
			if strings.TrimSpace(value) == "" {
				return out, fmt.Errorf("tag key %s needs a value, as %s=name", key, key)
			}

			if key == "prefix" {
				out.prefix = value
			} else {
				out.column = value
			}
		default:
			return out, fmt.Errorf("unknown tag key %q", key)
		}
	}

	return out, nil
}

// ColumnName converts a Go field name to snake_case, keeping initialisms
// together: UserID becomes user_id.
func ColumnName(name string) string {
	var b strings.Builder

	runes := []rune(name)
	for i, r := range runes {
		if i > 0 && unicode.IsUpper(r) {
			prevLower := unicode.IsLower(runes[i-1])

			nextLower := i+1 < len(runes) && unicode.IsLower(runes[i+1])
			if prevLower || nextLower {
				b.WriteByte('_')
			}
		}

		b.WriteRune(unicode.ToLower(r))
	}

	return b.String()
}

// TableName converts a type name to its conventional table name.
func TableName(name string) string {
	return plural(ColumnName(name))
}

func plural(s string) string {
	switch {
	case strings.HasSuffix(s, "s"),
		strings.HasSuffix(s, "x"),
		strings.HasSuffix(s, "ch"),
		strings.HasSuffix(s, "sh"):
		return s + "es"
	case strings.HasSuffix(s, "y") && len(s) > 1 && !strings.ContainsAny(s[len(s)-2:len(s)-1], "aeiou"):
		return s[:len(s)-1] + "ies"
	}

	return s + "s"
}
