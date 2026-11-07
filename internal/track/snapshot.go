package track

import (
	"bytes"
	"reflect"
	"sync"
	"time"
	"unsafe"

	"github.com/skudra-go/ant/internal/meta"
)

// snapshotOf copies entity, deep-copying what a field points to, so changes
// through a pointer or slice show up in a diff. Relations are left out.
func snapshotOf(m *meta.Model, entity unsafe.Pointer) unsafe.Pointer {
	copied := reflect.New(m.Type)
	copied.Elem().Set(reflect.NewAt(m.Type, entity).Elem())
	out := copied.UnsafePointer()

	for _, f := range m.Fields {
		if indirect(f.Type) {
			v := f.Value(out)
			v.Set(deepCopy(v))
		}
	}

	for _, r := range m.Relations {
		reflect.NewAt(r.Type, unsafe.Add(out, r.Offset)).Elem().SetZero()
	}

	return out
}

// indirect reports whether a value of t can share memory with its copy.
func indirect(t reflect.Type) bool {
	if t == timeType {
		return false
	}

	switch t.Kind() {
	case reflect.Pointer, reflect.Slice, reflect.Map, reflect.Interface:
		return true
	case reflect.Struct:
		for sf := range t.Fields() {
			if sf.IsExported() && indirect(sf.Type) {
				return true
			}
		}
	case reflect.Array:
		return indirect(t.Elem())
	}

	return false
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
		if !indirect(v.Type()) {
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
	if a.IsValid() != b.IsValid() || a.IsValid() && a.Type() != b.Type() {
		return false
	}

	if !a.IsValid() {
		return true
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

func equal(f *meta.Field, a, b unsafe.Pointer) bool {
	x, y := unsafe.Add(a, f.Offset), unsafe.Add(b, f.Offset)

	if f.Equal != nil {
		return f.Equal(x, y)
	}

	return comparerFor(f.Type)(x, y)
}
