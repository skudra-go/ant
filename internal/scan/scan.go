// Package scan maps database rows onto Go values.
package scan

import (
	"database/sql"
	"database/sql/driver"
	"fmt"
	"reflect"
	"unsafe"

	"github.com/skudra-go/ant/internal/meta"
)

// For returns a function that scans the current row into a *T. A value type
// such as int takes the single result column. A struct takes columns by name:
// columns with no field are ignored, and fields with no column are left alone.
// Two columns for one field are an error: SELECT * over a join returns an id
// from each table, and taking the last would give the row the other's id.
// A time is held in UTC, whatever zone the driver decoded it in, and a type
// database/sql cannot fill by itself is read as meta.ReaderFor says.
func For[T any](rows *sql.Rows) (func(*T) error, error) { return reader[T](rows, nil) }

// Projected is For for a projection's result: a NULL into a field that zero
// says to read as its zero value leaves that field zero, where database/sql
// would refuse it. An aggregate over no rows, or a column of an outer join's
// side with no row, gives one in ordinary use.
func Projected[T any](
	rows *sql.Rows,
	zero func(*meta.Field) bool,
) (func(*T) error, error) {
	return reader[T](rows, zero)
}

func reader[T any](rows *sql.Rows, zero func(*meta.Field) bool) (func(*T) error, error) {
	if meta.IsValue(reflect.TypeFor[T]()) {
		utc := meta.InUTC(reflect.TypeFor[T]())
		read := meta.ReaderFor(reflect.TypeFor[T]())

		switch {
		case utc == nil && read == nil:
			return func(dst *T) error { return rows.Scan(dst) }, nil
		case read != nil:
			return func(dst *T) error { return rows.Scan(read(unsafe.Pointer(dst))) }, nil
		}

		return func(dst *T) error {
			if err := rows.Scan(dst); err != nil {
				return err
			}

			utc(unsafe.Pointer(dst))

			return nil
		}, nil
	}

	columns, err := rows.Columns()
	if err != nil {
		return nil, err
	}

	model := meta.Of[T]()

	// The fields holding a time are found once, so a row moves only those to
	// UTC, and every other column costs what it did before. They share one
	// block with fields, so finding them allocates nothing more.
	block := make([]*meta.Field, 2*len(columns))
	fields, times := block[:len(columns)], block[len(columns):len(columns)]

	for i, c := range columns {
		fields[i] = model.Column(c)

		if fields[i] == nil {
			continue
		}

		for j := range i {
			if fields[j] == fields[i] {
				return nil, fmt.Errorf(
					"ant: result columns %d and %d are both %q, for %s.%s; alias one of them",
					j+1,
					i+1,
					c,
					model.Type.Name(),
					fields[i].Name,
				)
			}
		}

		if fields[i].HoldsTime() {
			times = append(times, fields[i])
		}
	}

	dest := make([]any, len(columns))

	var zeroed []*zeroNull

	if zero != nil {
		for i, f := range fields {
			if f == nil || !zero(f) {
				continue
			}

			if zeroed == nil {
				zeroed = make([]*zeroNull, len(fields))
			}

			zeroed[i] = &zeroNull{field: f}
		}
	}

	return func(dst *T) error {
		for i, f := range fields {
			switch {
			case f == nil:
				dest[i] = new(any)
			case zeroed != nil && zeroed[i] != nil:
				z := zeroed[i]
				z.entity = unsafe.Pointer(dst)
				z.dst = f.Dest(z.entity)
				dest[i] = z
			default:
				dest[i] = f.Dest(unsafe.Pointer(dst))
			}
		}

		// Scanned first and moved after, so a NULL or a value no time comes
		// from is refused by database/sql, as it is for any other field.
		if err := rows.Scan(dest...); err != nil {
			return err
		}

		for _, f := range times {
			f.InUTC(unsafe.Pointer(dst))
		}

		return nil
	}, nil
}

// zeroNull scans a column into a field, leaving the field's zero value for a
// NULL, and passing any other value on as database/sql would: to the field's
// Scanner, or converted into it.
type zeroNull struct {
	field  *meta.Field
	entity unsafe.Pointer
	dst    any
}

// Scan implements sql.Scanner.
func (z *zeroNull) Scan(src any) error {
	if src == nil {
		z.field.Value(z.entity).SetZero()

		return nil
	}

	if s, ok := z.dst.(sql.Scanner); ok {
		return s.Scan(src)
	}

	return sql.ConvertAssign(driver.ScanContext{}, z.dst, src)
}
