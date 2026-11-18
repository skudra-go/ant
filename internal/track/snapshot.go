package track

import (
	"bytes"
	"encoding/json"
	"reflect"
	"strconv"
	"sync"
	"time"
	"unsafe"

	"github.com/skudra-go/ant/internal/meta"
)

// snapshotOf copies the columns of entity into a snapshot of the model's
// compact layout (meta.Snapshot), deep-copying what a field points to, so that
// changes through a pointer or slice show up in a diff.
func snapshotOf(m *meta.Model, entity unsafe.Pointer) unsafe.Pointer {
	out := reflect.New(m.SnapshotLayout().Type).UnsafePointer()
	snapshotInto(m, out, entity)

	return out
}

// snapshotInto is snapshotOf into memory the caller made, which holds a zero
// snapshot.
func snapshotInto(m *meta.Model, out, entity unsafe.Pointer) {
	layout := m.SnapshotLayout()

	for _, f := range layout.Fields {
		f.CopyAt(layout.At(out, f), unsafe.Add(entity, f.Offset))
	}

	for _, f := range layout.Shared {
		v := reflect.NewAt(f.Type, layout.At(out, f)).Elem()
		v.Set(deepCopy(v))
	}
}

// entityOf returns a new entity holding the values of the snapshot at snapshot,
// deep-copied, with no relations and no computed values.
func entityOf(m *meta.Model, snapshot unsafe.Pointer) unsafe.Pointer {
	layout := m.SnapshotLayout()
	out := reflect.New(m.Type).UnsafePointer()

	for _, f := range layout.Fields {
		f.CopyAt(unsafe.Add(out, f.Offset), layout.At(snapshot, f))
	}

	for _, f := range layout.Shared {
		v := f.Value(out)
		v.Set(deepCopy(v))
	}

	return out
}

// copyOf returns a private copy of entity, deep-copying what a field points to,
// with its relations left out.
func copyOf(m *meta.Model, entity unsafe.Pointer) unsafe.Pointer {
	out := reflect.New(m.Type).UnsafePointer()
	reflect.NewAt(m.Type, out).Elem().Set(reflect.NewAt(m.Type, entity).Elem())

	for _, f := range m.Shared() {
		v := f.Value(out)
		v.Set(deepCopy(v))
	}

	for _, r := range m.Relations {
		reflect.NewAt(r.Type, unsafe.Add(out, r.Offset)).Elem().SetZero()
	}

	return out
}

// block hands out the entries and snapshots of the rows of one load from memory
// allocated a block at a time, as the rows themselves are, which saves two
// allocations per row. Blocks start small and double, so a load that finds most
// of its rows tracked already wastes little. A block lives as long as any entry
// or snapshot in it does.
type block struct {
	entries   []Entry
	snapshots unsafe.Pointer // the block's first snapshot
	used      int
}

const blockLimit = 512

// next returns an entry and the memory of its snapshot. left is how many rows
// the load may still track, this one included.
func (b *block) next(m *meta.Model, left int) (*Entry, unsafe.Pointer) {
	if b.used == len(b.entries) {
		size := min(max(len(b.entries)*2, 8), blockLimit, left)
		b.entries = make([]Entry, size)
		b.snapshots = reflect.MakeSlice(m.SnapshotLayout().Slice, size, size).UnsafePointer()
		b.used = 0
	}

	// The address is worked out from the first snapshot each time, so that no
	// pointer is ever kept to just past the block's end.
	e := &b.entries[b.used]
	snapshot := unsafe.Add(b.snapshots, uintptr(b.used)*m.SnapshotLayout().Type.Size())
	b.used++

	return e, snapshot
}

// deepCopy copies v and whatever it refers to. Unexported struct fields are
// shared, because reflect cannot set them.
func deepCopy(v reflect.Value) reflect.Value {
	switch v.Kind() {
	case reflect.Pointer:
		if v.IsNil() {
			return v
		}

		out := reflect.New(v.Type().Elem())
		out.Elem().Set(deepCopy(v.Elem()))

		return out
	case reflect.Slice:
		if v.IsNil() {
			return v
		}

		out := reflect.MakeSlice(v.Type(), v.Len(), v.Len())

		for i := range v.Len() {
			out.Index(i).Set(deepCopy(v.Index(i)))
		}

		return out
	case reflect.Map:
		if v.IsNil() {
			return v
		}

		out := reflect.MakeMapWithSize(v.Type(), v.Len())

		for iter := v.MapRange(); iter.Next(); {
			out.SetMapIndex(iter.Key(), deepCopy(iter.Value()))
		}

		return out
	case reflect.Interface:
		// Such as the maps and slices that JSON decodes into an any.
		if v.IsNil() {
			return v
		}

		out := reflect.New(v.Type()).Elem()
		out.Set(deepCopy(v.Elem()))

		return out
	case reflect.Struct:
		if !meta.Indirect(v.Type()) {
			return v
		}

		out := reflect.New(v.Type()).Elem()
		out.Set(v)

		for i := range v.NumField() {
			if v.Type().Field(i).IsExported() {
				out.Field(i).Set(deepCopy(v.Field(i)))
			}
		}

		return out
	}

	return v
}

var timeType = reflect.TypeFor[time.Time]()

type comparer func(a, b unsafe.Pointer) bool

var comparers sync.Map // reflect.Type to comparer

func same[T comparable](a, b unsafe.Pointer) bool { return *(*T)(a) == *(*T)(b) }

func samePointer[T comparable](a, b unsafe.Pointer) bool {
	x, y := *(**T)(a), *(**T)(b)
	if x == nil || y == nil {
		return x == y
	}

	return *x == *y
}

func comparerFor(t reflect.Type) comparer {
	if c, ok := comparers.Load(t); ok {
		return c.(comparer)
	}

	c := buildComparer(t)
	comparers.Store(t, c)

	return c
}

func buildComparer(t reflect.Type) comparer {
	switch t {
	case reflect.TypeFor[string]():
		return same[string]
	case reflect.TypeFor[int]():
		return same[int]
	case reflect.TypeFor[int32]():
		return same[int32]
	case reflect.TypeFor[int64]():
		return same[int64]
	case reflect.TypeFor[bool]():
		return same[bool]
	case reflect.TypeFor[float64]():
		return func(a, b unsafe.Pointer) bool { return sameFloat(*(*float64)(a), *(*float64)(b)) }
	case reflect.TypeFor[*string]():
		return samePointer[string]
	case reflect.TypeFor[*int]():
		return samePointer[int]
	case reflect.TypeFor[*int64]():
		return samePointer[int64]
	case reflect.TypeFor[*bool]():
		return samePointer[bool]
	case reflect.TypeFor[[]byte]():
		return func(a, b unsafe.Pointer) bool {
			x, y := *(*[]byte)(a), *(*[]byte)(b)

			return (x == nil) == (y == nil) && bytes.Equal(x, y)
		}
	case timeType:
		return func(a, b unsafe.Pointer) bool { return (*time.Time)(a).Equal(*(*time.Time)(b)) }
	case reflect.TypeFor[*time.Time]():
		return func(a, b unsafe.Pointer) bool {
			x, y := *(**time.Time)(a), *(**time.Time)(b)
			if x == nil || y == nil {
				return x == y
			}

			return x.Equal(*y)
		}
	}

	return func(a, b unsafe.Pointer) bool {
		return sameValue(reflect.NewAt(t, a).Elem(), reflect.NewAt(t, b).Elem())
	}
}

// sameFloat reports whether a and b are the same value. NaN is not equal to
// itself, but a NaN that stays one is no change.
func sameFloat(a, b float64) bool { return a == b || a != a && b != b }

// sameValue is reflect.DeepEqual as change tracking needs it: a NaN is the same
// as a NaN, and a time is the same as another at that instant, in any zone, as
// in a time.Time field. Nil and empty differ, as NULL and empty do.
func sameValue(a, b reflect.Value) bool {
	if a.IsValid() != b.IsValid() {
		return false
	}

	if !a.IsValid() {
		return true
	}

	if a.Type() != b.Type() {
		return sameNumber(a, b)
	}

	if x, y, ok := times(a, b); ok {
		return x.Equal(y)
	}

	switch a.Kind() {
	case reflect.Float32, reflect.Float64:
		return sameFloat(a.Float(), b.Float())
	case reflect.Complex64, reflect.Complex128:
		x, y := a.Complex(), b.Complex()

		return sameFloat(real(x), real(y)) && sameFloat(imag(x), imag(y))
	case reflect.Pointer:
		if a.IsNil() || b.IsNil() || a.UnsafePointer() == b.UnsafePointer() {
			return a.IsNil() == b.IsNil()
		}

		return sameValue(a.Elem(), b.Elem())
	case reflect.Interface:
		if a.IsNil() || b.IsNil() {
			return a.IsNil() == b.IsNil()
		}

		return sameValue(a.Elem(), b.Elem())
	case reflect.Slice:
		if a.IsNil() != b.IsNil() || a.Len() != b.Len() {
			return false
		}

		// Bytes, as of a named []byte, at once.
		if a.Type().Elem().Kind() == reflect.Uint8 {
			return bytes.Equal(a.Bytes(), b.Bytes())
		}

		fallthrough
	case reflect.Array:
		for i := range a.Len() {
			if !sameValue(a.Index(i), b.Index(i)) {
				return false
			}
		}

		return true
	case reflect.Map:
		if a.IsNil() != b.IsNil() || a.Len() != b.Len() {
			return false
		}

		for iter := a.MapRange(); iter.Next(); {
			other := b.MapIndex(iter.Key())
			if !other.IsValid() || !sameValue(iter.Value(), other) {
				return false
			}
		}

		return true
	case reflect.Struct:
		for i := range a.NumField() {
			if !sameValue(a.Field(i), b.Field(i)) {
				return false
			}
		}

		return true
	case reflect.Func, reflect.Chan, reflect.UnsafePointer:
		return a.Pointer() == b.Pointer()
	case reflect.Bool:
		return a.Bool() == b.Bool()
	case reflect.String:
		return a.String() == b.String()
	}

	switch {
	case a.CanInt():
		return a.Int() == b.Int()
	case a.CanUint():
		return a.Uint() == b.Uint()
	}

	return false
}

// numberType is what JSON reads a number into an interface as (decision 109).
var numberType = reflect.TypeFor[json.Number]()

// sameNumber reports whether a and b, of different types, are a json.Number and
// a number of a predeclared type that JSON writes as the same text, so that they
// are the same as stored: as when the application puts 2.0 in a map that a JSON
// column read as json.Number("2"). A named number type may write itself in its
// own way, so it is a change.
func sameNumber(a, b reflect.Value) bool {
	if b.Type() == numberType {
		a, b = b, a
	}

	if a.Type() != numberType || b.Type().PkgPath() != "" {
		return false
	}

	var (
		text []byte
		err  error
	)

	switch b.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		text = strconv.AppendInt(nil, b.Int(), 10)
	case reflect.Uint,
		reflect.Uint8,
		reflect.Uint16,
		reflect.Uint32,
		reflect.Uint64,
		reflect.Uintptr:
		text = strconv.AppendUint(nil, b.Uint(), 10)
	case reflect.Float32:
		text, err = json.Marshal(float32(b.Float()))
	case reflect.Float64:
		text, err = json.Marshal(b.Float())
	default:
		return false
	}

	return err == nil && string(text) == a.String()
}

// times returns a and b as times if they are. A time in an unexported field is
// read through its address, which reflect allows where it forbids Interface; one
// that has neither is compared field by field.
func times(a, b reflect.Value) (time.Time, time.Time, bool) {
	switch {
	case a.Type() != timeType:
		return time.Time{}, time.Time{}, false
	case a.CanInterface() && b.CanInterface():
		x, _ := reflect.TypeAssert[time.Time](a)
		y, _ := reflect.TypeAssert[time.Time](b)

		return x, y, true
	case a.CanAddr() && b.CanAddr():
		return *(*time.Time)(a.Addr().UnsafePointer()), *(*time.Time)(b.Addr().UnsafePointer()), true
	}

	return time.Time{}, time.Time{}, false
}

// unchanged reports whether f's value in entity is the one in the snapshot. A field
// the snapshot does not keep is never a change.
func unchanged(m *meta.Model, f *meta.Field, entity, snapshot unsafe.Pointer) bool {
	layout := m.SnapshotLayout()
	if !layout.Holds(f) {
		return true
	}

	return equalAt(f, unsafe.Add(entity, f.Offset), layout.At(snapshot, f))
}

// equalAt compares two values of f, at x and y.
func equalAt(f *meta.Field, x, y unsafe.Pointer) bool {
	if f.Equal != nil {
		return f.Equal(x, y)
	}

	// The field keeps its comparer, so that it is looked up once and not for
	// every row of every save.
	if c := f.Comparer.Load(); c != nil {
		return (*c)(x, y)
	}

	c := (func(a, b unsafe.Pointer) bool)(comparerFor(f.Type))
	f.Comparer.Store(&c)

	return c(x, y)
}
