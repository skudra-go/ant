package meta

import (
	"bytes"
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
	"unsafe"
)

// Dest returns what a row's column is scanned into for this field of the
// entity at entity: the field itself, or a Scanner that converts what
// database/sql cannot (see ReaderFor).
func (f *Field) Dest(entity unsafe.Pointer) any {
	if f.read != nil {
		return f.read(unsafe.Add(entity, f.Offset))
	}

	return f.Addr(entity)
}

// DriverOnly reports whether only the driver converts a column into this field,
// as pgx does netip.Addr or []string: database/sql would refuse it from the
// value a Scanner is given, so a column that may be NULL for want of a row has
// to be scanned into a pointer, not through a Scanner.
func (f *Field) DriverOnly() bool { return f.driverOnly }

// ReaderFor returns the function that makes the Scanner a value of type t at a
// pointer is read through, or nil when database/sql's own conversion is right
// for t. These are the types database/sql cannot fill from what a driver gives:
//
//   - time.Duration, *time.Duration and sql.Null[time.Duration], from the text of
//     a PostgreSQL interval, as well as from a count of nanoseconds;
//   - net.IP, *net.IP and sql.Null[net.IP], from the text of an inet or cidr, as
//     well as from its bytes;
//   - a named byte slice, such as json.RawMessage, from NULL, which leaves it
//     nil as it does a []byte, and from text;
//   - a float64, *float64 or sql.Null[float64], from the text of an interval, as
//     its nanoseconds, as AVG of an interval column gives one;
//   - a map with string keys, from JSON, its numbers in interfaces as
//     json.Number, as ant.JSON reads them.
func ReaderFor(t reflect.Type) func(unsafe.Pointer) any {
	if read, ok := readers[t]; ok {
		return read
	}

	switch {
	case namedBytes(t):
		return func(p unsafe.Pointer) any { return &bytesDest{t: t, p: p} }
	case t.Kind() == reflect.Pointer && namedBytes(t.Elem()):
		return func(p unsafe.Pointer) any { return &bytesPointerDest{t: t.Elem(), p: p} }
	case jsonMap(t):
		return func(p unsafe.Pointer) any { return &mapDest{t: t, p: p} }
	}

	return nil
}

var readers = map[reflect.Type]func(unsafe.Pointer) any{
	reflect.TypeFor[time.Duration](): func(p unsafe.Pointer) any {
		return (*durationDest)(p)
	},
	reflect.TypeFor[*time.Duration](): func(p unsafe.Pointer) any {
		return (*durationPointerDest)(p)
	},
	reflect.TypeFor[sql.Null[time.Duration]](): func(p unsafe.Pointer) any {
		return (*durationNullDest)(p)
	},
	reflect.TypeFor[net.IP](): func(p unsafe.Pointer) any {
		return (*ipDest)(p)
	},
	reflect.TypeFor[*net.IP](): func(p unsafe.Pointer) any {
		return (*ipPointerDest)(p)
	},
	reflect.TypeFor[sql.Null[net.IP]](): func(p unsafe.Pointer) any {
		return (*ipNullDest)(p)
	},
	reflect.TypeFor[float64](): func(p unsafe.Pointer) any {
		return (*floatDest)(p)
	},
	reflect.TypeFor[*float64](): func(p unsafe.Pointer) any {
		return (*floatPointerDest)(p)
	},
	reflect.TypeFor[sql.Null[float64]](): func(p unsafe.Pointer) any {
		return (*floatNullDest)(p)
	},
}

var (
	bytesType    = reflect.TypeFor[[]byte]()
	rawBytesType = reflect.TypeFor[sql.RawBytes]()
)

// namedBytes reports whether t is a named byte slice that database/sql fills
// with its own conversion, which refuses NULL. sql.RawBytes keeps its meaning,
// and a type that scans itself does what it says.
func namedBytes(t reflect.Type) bool {
	return t.Kind() == reflect.Slice && t.Elem().Kind() == reflect.Uint8 && t.Name() != "" &&
		t != bytesType && t != rawBytesType && !reflect.PointerTo(t).Implements(scannerType) &&
		readers[t] == nil
}

// convert is database/sql's conversion, for what a reader leaves to it, so that
// its errors say what they always said.
func convert(dest any, src any) error { return sql.ConvertAssign(driver.ScanContext{}, dest, src) }

// textOf returns the text in src, a driver's string or bytes.
func textOf(src any) (string, bool) {
	switch v := src.(type) {
	case string:
		return v, true
	case []byte:
		return string(v), true
	}

	return "", false
}

// durationDest reads a time.Duration. A whole number is nanoseconds, as ant
// wrote a duration to an integer column, and other text is an interval.
type durationDest time.Duration

// Scan implements sql.Scanner.
func (d *durationDest) Scan(src any) error {
	text, ok := textOf(src)
	if !ok {
		return convert((*time.Duration)(d), src)
	}

	if _, err := strconv.ParseInt(strings.TrimSpace(text), 10, 64); err == nil {
		return convert((*time.Duration)(d), src)
	}

	out, err := ParseDuration(text)
	if err != nil {
		return err
	}

	*d = durationDest(out)

	return nil
}

type durationPointerDest struct{ v *time.Duration }

// Scan implements sql.Scanner.
func (d *durationPointerDest) Scan(src any) error {
	if src == nil {
		d.v = nil

		return nil
	}

	v := new(time.Duration)
	if err := (*durationDest)(v).Scan(src); err != nil {
		return err
	}

	d.v = v

	return nil
}

type durationNullDest sql.Null[time.Duration]

// Scan implements sql.Scanner.
func (d *durationNullDest) Scan(src any) error {
	if src == nil {
		*d = durationNullDest{}

		return nil
	}

	var v time.Duration
	if err := (*durationDest)(&v).Scan(src); err != nil {
		return err
	}

	*d = durationNullDest{V: v, Valid: true}

	return nil
}

// ipDest reads a net.IP: bytes as they are, as a bytea holds them, and text as
// an inet or cidr writes it, which must be a single address. NULL leaves it nil.
type ipDest net.IP

// Scan implements sql.Scanner.
func (d *ipDest) Scan(src any) error {
	text, ok := src.(string)

	switch {
	case src == nil:
		*d = nil

		return nil
	case !ok:
		return convert((*net.IP)(d), src)
	}

	addr, err := netip.ParseAddr(text)
	if err != nil {
		// A cidr writes its mask even when it covers every bit.
		prefix, err := netip.ParsePrefix(text)
		if err != nil || prefix.Bits() != prefix.Addr().BitLen() {
			return fmt.Errorf(
				"ant: %q is not one address, so a net.IP cannot hold it: read it into a netip.Prefix",
				text,
			)
		}

		addr = prefix.Addr()
	}

	*d = ipDest(addr.AsSlice())

	return nil
}

type ipPointerDest struct{ v *net.IP }

// Scan implements sql.Scanner.
func (d *ipPointerDest) Scan(src any) error {
	if src == nil {
		d.v = nil

		return nil
	}

	v := new(net.IP)
	if err := (*ipDest)(v).Scan(src); err != nil {
		return err
	}

	d.v = v

	return nil
}

type ipNullDest sql.Null[net.IP]

// Scan implements sql.Scanner.
func (d *ipNullDest) Scan(src any) error {
	if src == nil {
		*d = ipNullDest{}

		return nil
	}

	var v net.IP
	if err := (*ipDest)(&v).Scan(src); err != nil {
		return err
	}

	*d = ipNullDest{V: v, Valid: true}

	return nil
}

// bytesDest reads a named byte slice of type t at p: NULL as nil, text and
// bytes as a copy, and anything else as database/sql would.
type bytesDest struct {
	t reflect.Type
	p unsafe.Pointer
}

// Scan implements sql.Scanner.
func (d *bytesDest) Scan(src any) error {
	switch v := src.(type) {
	case nil:
		*(*[]byte)(d.p) = nil
	case []byte:
		*(*[]byte)(d.p) = bytes.Clone(v)
	case string:
		*(*[]byte)(d.p) = []byte(v)
	default:
		return convert(reflect.NewAt(d.t, d.p).Interface(), src)
	}

	return nil
}

// bytesPointerDest reads a pointer at p to a named byte slice of type t.
type bytesPointerDest struct {
	t reflect.Type
	p unsafe.Pointer
}

// Scan implements sql.Scanner.
func (d *bytesPointerDest) Scan(src any) error {
	if src == nil {
		*(*unsafe.Pointer)(d.p) = nil

		return nil
	}

	v := reflect.New(d.t)
	if err := (&bytesDest{t: d.t, p: v.UnsafePointer()}).Scan(src); err != nil {
		return err
	}

	*(*unsafe.Pointer)(d.p) = v.UnsafePointer()

	return nil
}

// floatDest reads a float64, and an interval as its nanoseconds, as an average
// of a time.Duration is from an interval column. Text that is neither is
// database/sql's error.
type floatDest float64

// Scan implements sql.Scanner.
func (d *floatDest) Scan(src any) error {
	if text, ok := textOf(src); ok {
		if _, err := strconv.ParseFloat(strings.TrimSpace(text), 64); err != nil {
			if out, err := ParseDuration(text); err == nil {
				*d = floatDest(out)

				return nil
			}
		}
	}

	return convert((*float64)(d), src)
}

type floatPointerDest struct{ v *float64 }

// Scan implements sql.Scanner.
func (d *floatPointerDest) Scan(src any) error {
	if src == nil {
		d.v = nil

		return nil
	}

	v := new(float64)
	if err := (*floatDest)(v).Scan(src); err != nil {
		return err
	}

	d.v = v

	return nil
}

type floatNullDest sql.Null[float64]

// Scan implements sql.Scanner.
func (d *floatNullDest) Scan(src any) error {
	if src == nil {
		*d = floatNullDest{}

		return nil
	}

	var v float64
	if err := (*floatDest)(&v).Scan(src); err != nil {
		return err
	}

	*d = floatNullDest{V: v, Valid: true}

	return nil
}

// jsonMap reports whether t is a map ant reads from JSON: string keys, and no
// conversion of its own.
func jsonMap(t reflect.Type) bool {
	return t.Kind() == reflect.Map && t.Key().Kind() == reflect.String &&
		!reflect.PointerTo(t).Implements(scannerType)
}

// mapDest reads a map of type t at p from JSON with UseNumber, so a large
// integer in an interface keeps its digits (decision 109). NULL is nil.
type mapDest struct {
	t reflect.Type
	p unsafe.Pointer
}

// Scan implements sql.Scanner.
func (d *mapDest) Scan(src any) error {
	v := reflect.NewAt(d.t, d.p)
	text, ok := textOf(src)

	switch {
	case src == nil:
		v.Elem().SetZero()

		return nil
	case !ok:
		return convert(v.Interface(), src)
	}

	fresh := reflect.New(d.t)
	decoder := json.NewDecoder(strings.NewReader(text))
	decoder.UseNumber()

	if err := decoder.Decode(fresh.Interface()); err != nil {
		return fmt.Errorf("ant: read %s from JSON: %w", d.t, err)
	}

	v.Elem().Set(fresh.Elem())

	return nil
}

// sqlConverts reports whether database/sql converts a driver's value into a t
// by itself, as it does what pgx's stdlib driver leaves to it: a Scanner, a
// scalar, bytes, a time, an interface, or a pointer to one of these.
// Plain reports whether database/sql alone writes the field, and whether it or
// ant's own readers read it: what a driver that converts nothing of its own,
// unlike pgx, takes and gives.
func (f *Field) Plain() (writes, reads bool) { return sqlWrites(f.Type), !f.driverOnly }

// sqlWrites reports whether database/sql passes a value of type t to any
// driver: a driver.Valuer, or a kind its default converter takes, behind any
// number of pointers.
func sqlWrites(t reflect.Type) bool {
	for range 64 {
		if t.Implements(valuerType) || reflect.PointerTo(t).Implements(valuerType) {
			return true
		}

		if t.Kind() != reflect.Pointer {
			break
		}

		t = t.Elem()
	}

	switch t.Kind() {
	case reflect.Bool, reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64,
		reflect.Float32, reflect.Float64, reflect.String, reflect.Interface:
		return true
	case reflect.Slice:
		return t.Elem().Kind() == reflect.Uint8
	case reflect.Struct:
		// database/sql takes time.Time itself, not a type defined on it.
		return t == timeType
	}

	return false
}

func sqlConverts(t reflect.Type) bool {
	for range 64 {
		if reflect.PointerTo(t).Implements(scannerType) {
			return true
		}

		if t.Kind() != reflect.Pointer {
			break
		}

		t = t.Elem()
	}

	switch t.Kind() {
	case reflect.Bool, reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64,
		reflect.Float32, reflect.Float64, reflect.String, reflect.Interface:
		return true
	case reflect.Slice:
		return t.Elem().Kind() == reflect.Uint8
	case reflect.Struct:
		return t.ConvertibleTo(timeType)
	}

	return false
}
