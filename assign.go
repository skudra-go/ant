package ant

import (
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
		node:  qb.Assignment{Column: f.Column, Value: value.node},
		model: model,
		refs:  value.refs,
	}
}

// Inc adds n to a numeric column, in the database.
func Inc[V Number](field *V, n V) Assignment { return SetExpr(field, Col(field).Plus(n)) }

// Dec subtracts n from a numeric column, in the database.
func Dec[V Number](field *V, n V) Assignment { return SetExpr(field, Col(field).Minus(n)) }

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
