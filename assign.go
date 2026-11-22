package ant

import (
	"reflect"
	"unsafe"

	"fmt"

	"github.com/skudra-go/ant/internal/meta"
	"github.com/skudra-go/ant/qb"
)

// Assignment sets a column in a bulk update.
type Assignment struct {
	node  qb.Assignment
	model *meta.Model
	refs  []*meta.Model
	err   error
}

// Set assigns v to field. Zero values are written like any other value.
func Set[V any](field *V, v V) Assignment {
	f, model, err := fieldOf(field)
	if err != nil {
		return Assignment{err: err}
	}

	return Assignment{node: qb.Assignment{Column: f.Column, Value: fieldParam(f, v)}, model: model}
}

// fieldParam binds a value of a column, hidden from logs if it is sensitive.
func fieldParam(f *meta.Field, v any) qb.Param {
	if f.Sensitive {
		return qb.Param{Value: qb.Secret{V: v}}
	}

	return qb.Param{Value: v}
}

// rowBlock makes the VALUES rows of one INSERT from two blocks, one of cells and
// one of the values bound in them, in place of a slice per row and an
// allocation per value. A cell holds a pointer to its qb.Param, which, unlike
// the qb.Param itself, goes into a qb.Expr with no allocation.
type rowBlock struct {
	cells  []qb.Expr
	params []qb.Param
	width  int
}

func newRowBlock(rows, width int) *rowBlock {
	return &rowBlock{
		cells:  make([]qb.Expr, rows*width),
		params: make([]qb.Param, rows*width),
		width:  width,
	}
}

// row fills in and returns row i: the entity's value of each column, or DEFAULT
// for a zero value of a column that has a default.
func (b *rowBlock) row(i int, columns []*meta.Field, entity unsafe.Pointer) []qb.Expr {
	start := i * b.width
	row := b.cells[start : start+b.width : start+b.width]

	for j, f := range columns {
		value := f.Value(entity)
		if f.Default && value.IsZero() {
			row[j] = qb.Default{}

			continue
		}

		p := &b.params[start+j]
		*p = fieldParam(f, value.Interface())
		row[j] = p
	}

	return row
}

// SetExpr assigns an expression to field, such as another column.
func SetExpr[V any](field *V, value Expr[V]) Assignment {
	f, model, err := fieldOf(field)

	switch {
	case err != nil:
		return Assignment{err: err}
	case value.err != nil:
		return Assignment{err: value.err}
	}

	return Assignment{
		node:  qb.Assignment{Column: f.Column, Value: castCaseIntoEnum(value.node, f.Storage.Enum)},
		model: model,
		refs:  value.refs,
	}
}

// castCaseIntoEnum casts the parameters of a CASE to the enum type the column
// declares. PostgreSQL resolves an unknown parameter inside a CASE to text, and
// text does not assign to an enum column, so the caller would otherwise have to
// write the cast on every value.
func castCaseIntoEnum(node qb.Expr, enum *meta.Enum) qb.Expr {
	if enum == nil || enum.Name == "" {
		return node
	}

	c, ok := node.(qb.Case)
	if !ok {
		return node
	}

	cast := func(e qb.Expr) qb.Expr {
		if _, isParam := e.(qb.Param); !isParam {
			return e
		}

		return qb.Cast{Value: e, Type: enum.Name}
	}

	out := qb.Case{Else: cast(c.Else)}

	if len(c.Whens) > 0 {
		out.Whens = make([]qb.When, len(c.Whens))

		for i, w := range c.Whens {
			out.Whens[i] = qb.When{If: w.If, Then: cast(w.Then)}
		}
	}

	return out
}

// Inc adds n to a column, in the database. The field is of any type the
// database adds to, such as a number, a decimal, or a pointer or sql.Null of
// one, and n is a value of the type it holds: Inc(&Posts.Votes, 1) for a Votes
// *int. A column that can hold NULL counts from zero, so Inc on a NULL sets n.
// An integer field refuses a step with a fraction, which the database would
// round.
func Inc[V, N any](field *V, n N) Assignment { return step(field, "+", n) }

// Dec subtracts n from a column, in the database, as Inc adds it: a NULL
// counts from zero.
func Dec[V, N any](field *V, n N) Assignment { return step(field, "-", n) }

// step is Inc and Dec: column op n, with COALESCE(column, 0) for a column that
// can hold NULL, which would otherwise stay NULL.
func step[V, N any](field *V, op string, n N) Assignment {
	col, f, model, err := columnExpr(field)
	if err != nil {
		return Assignment{err: err}
	}

	// An integer column would round a fraction away, where Inc[V Number] did
	// not compile.
	if held := heldType(reflect.TypeFor[V]()); isInteger(held) && !isInteger(reflect.TypeFor[N]()) {
		return Assignment{
			err: fmt.Errorf(
				"ant: %s holds %s, and a step of %T would be rounded: pass an integer",
				f.Name,
				held,
				n,
			),
		}
	}

	left := col

	if canBeNull(f) {
		left = qb.Func{Name: "COALESCE", Args: []qb.Expr{col, qb.Fragment{SQL: "0"}}}
	}

	refs := model.Self()

	return SetExpr(field, Expr[V]{
		node: qb.Binary{Left: left, Op: op, Right: hidden(n, readsSensitive(col, refs))},
		refs: refs,
	})
}

// heldType is the value a field of type t holds: a pointer's element, or the
// value of an sql.Null and the other nullable structs with a Valid flag.
func heldType(t reflect.Type) reflect.Type {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}

	if t.Kind() == reflect.Struct && t.NumField() == 2 {
		if valid, ok := t.FieldByName("Valid"); ok && valid.Type.Kind() == reflect.Bool &&
			valid.Index[0] == 1 {
			return t.Field(0).Type
		}
	}

	return t
}

// isInteger reports whether t is an integer type.
func isInteger(t reflect.Type) bool {
	switch t.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return true
	}

	return false
}

// assignedTwice refuses a write that gives one column two values. PostgreSQL and
// SQL Server refuse it, and MySQL keeps the last, so ant refuses it everywhere,
// with the column's name. What names the column, such as an assignment or a
// mapping, says what the write does with it.
func assignedTwice(model *meta.Model, what string, columns []string) error {
	seen := make(map[string]bool, len(columns))

	for _, column := range columns {
		if !seen[column] {
			seen[column] = true

			continue
		}

		name := column

		if f := model.Column(column); f != nil {
			name = f.Name
		}

		return fmt.Errorf("ant: %s.%s has two %s; give it one", model.Type.Name(), name, what)
	}

	return nil
}

// assignedColumns returns the columns set assigns, in order.
func assignedColumns(set []qb.Assignment) []string {
	out := make([]string, len(set))

	for i, a := range set {
		out[i] = a.Column
	}

	return out
}
