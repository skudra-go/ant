package ant

import (
	"cmp"
	"database/sql/driver"
	"errors"
	"fmt"
	"reflect"
	"time"

	"github.com/skudra-go/ant/dialect"
	"github.com/skudra-go/ant/internal/meta"
	"github.com/skudra-go/ant/qb"
)

// Ordered is a type with a database ordering. It extends cmp.Ordered with
// time.Time, which cmp.Ordered excludes.
type Ordered interface {
	cmp.Ordered | time.Time
}

// Cond is a query condition. Errors in a condition surface at the terminal
// call of the query that uses it.
type Cond struct {
	node qb.Cond
	refs []*meta.Model
	err  error
}

// on builds a condition on one column.
func on[V any](field *V, node func(qb.Column) qb.Cond) Cond {
	col, model, err := column(field)
	if err != nil {
		return Cond{err: err}
	}

	built := node(col)

	if f := model.Column(col.Name); f != nil && f.Sensitive {
		built = hideParams(built)
	}

	return Cond{node: built, refs: []*meta.Model{model}}
}

// hideParams marks the values a condition compares with as secret.
func hideParams(c qb.Cond) qb.Cond {
	hide := func(e qb.Expr) qb.Expr {
		if p, ok := e.(qb.Param); ok {
			return qb.Param{Value: qb.Secret{V: p.Value}}
		}

		return e
	}

	switch c := c.(type) {
	case qb.Compare:
		c.Right = hide(c.Right)

		return c
	case qb.In:
		values := make([]qb.Expr, len(c.Values))

		for i, v := range c.Values {
			values[i] = hide(v)
		}

		c.Values = values

		return c
	case qb.Between:
		c.Low, c.High = hide(c.Low), hide(c.High)

		return c
	}

	return c
}

func compare[V any](field *V, op qb.Op, v any) Cond {
	return on(field, func(c qb.Column) qb.Cond {
		return qb.Compare{Left: c, Op: op, Right: qb.Param{Value: v}}
	})
}

func nullCheck[V any](field *V, negate bool) Cond {
	return on(field, func(c qb.Column) qb.Cond { return qb.IsNull{Left: c, Negate: negate} })
}

// membership is In or NotIn. A value stored as NULL means what it means to Eq
// and Ne, IS NULL or IS NOT NULL, which SQL's IN cannot say: NULL IN (NULL) is
// unknown.
func membership[V any](field *V, values []V, negate bool) Cond {
	nodes := make([]qb.Expr, 0, len(values))
	null := false

	for _, v := range values {
		if isNull(v) {
			null = true

			continue
		}

		nodes = append(nodes, qb.Param{Value: v})
	}

	return on(field, func(c qb.Column) qb.Cond {
		in := qb.In{Left: c, Values: nodes, Negate: negate}

		switch {
		case !null:
			return in
		case len(nodes) == 0:
			return qb.IsNull{Left: c, Negate: negate}
		case negate:
			return qb.And{Children: []qb.Cond{in, qb.IsNull{Left: c, Negate: true}}}
		}

		return qb.Or{Children: []qb.Cond{in, qb.IsNull{Left: c}}}
	})
}

// isNull reports whether v is stored as NULL: a nil pointer, or a value such
// as an invalid sql.Null[T] that converts to nil.
func isNull(v any) bool {
	rv := reflect.ValueOf(v)
	if !rv.IsValid() || rv.Kind() == reflect.Pointer && rv.IsNil() {
		return true
	}

	if valuer, ok := v.(driver.Valuer); ok {
		value, err := valuer.Value()

		return err == nil && value == nil
	}

	return false
}

// Eq matches rows where field equals v. A v stored as NULL, such as a nil
// pointer, matches NULL. A non-nil pointer compares the pointed-to value.
func Eq[V comparable](field *V, v V) Cond {
	if isNull(v) {
		return nullCheck(field, false)
	}

	return compare(field, qb.OpEq, v)
}

// Ne matches rows where field does not equal v. A v stored as NULL matches
// non-NULL values. As in SQL, a NULL column never matches a non-NULL v.
func Ne[V comparable](field *V, v V) Cond {
	if isNull(v) {
		return nullCheck(field, true)
	}

	return compare(field, qb.OpNe, v)
}

// Gt matches rows where field is greater than v.
func Gt[V Ordered](field *V, v V) Cond { return compare(field, qb.OpGt, v) }

// Gte matches rows where field is greater than or equal to v.
func Gte[V Ordered](field *V, v V) Cond { return compare(field, qb.OpGte, v) }

// Lt matches rows where field is less than v.
func Lt[V Ordered](field *V, v V) Cond { return compare(field, qb.OpLt, v) }

// Lte matches rows where field is less than or equal to v.
func Lte[V Ordered](field *V, v V) Cond { return compare(field, qb.OpLte, v) }

// In matches rows where field equals one of values, as Eq would. With no
// values, it matches nothing. A nil value matches NULL, as with Eq.
func In[V comparable](field *V, values ...V) Cond { return membership(field, values, false) }

// NotIn matches rows where field equals none of values, as Ne would. With no
// values, it matches everything, including NULL. A nil value leaves NULL out,
// as with Ne, and otherwise, as in SQL, a NULL column matches no other value.
func NotIn[V comparable](field *V, values ...V) Cond { return membership(field, values, true) }

// Between matches rows where field is within low and high, inclusive.
func Between[V Ordered](field *V, low, high V) Cond {
	return on(field, func(c qb.Column) qb.Cond {
		return qb.Between{Left: c, Low: qb.Param{Value: low}, High: qb.Param{Value: high}}
	})
}

// Like matches rows where field matches a SQL LIKE pattern, in which % matches
// any text and _ matches one character.
func Like[V ~string](field *V, pattern V) Cond { return compare(field, qb.OpLike, string(pattern)) }

// ILike is Like ignoring case.
func ILike[V ~string](
	field *V,
	pattern V,
) Cond {
	return compare(field, qb.OpILike, string(pattern))
}

// AnyOf matches rows where at least one of fields equals v.
func AnyOf[V comparable](v V, fields ...*V) Cond { return Or(eachField(v, fields)...) }

// AllOf matches rows where every one of fields equals v.
func AllOf[V comparable](v V, fields ...*V) Cond { return And(eachField(v, fields)...) }

// NoneOf matches rows where none of fields equals v.
func NoneOf[V comparable](v V, fields ...*V) Cond { return Not(Or(eachField(v, fields)...)) }

func eachField[V comparable](v V, fields []*V) []Cond {
	conds := make([]Cond, len(fields))

	for i, f := range fields {
		conds[i] = Eq(f, v)
	}

	return conds
}

// Past matches rows where a time column is before the database's current time.
func Past(field *time.Time) Cond { return Col(field).LtExpr(SQL[time.Time]("CURRENT_TIMESTAMP")) }

// Future matches rows where a time column is after the database's current time.
func Future(field *time.Time) Cond { return Col(field).GtExpr(SQL[time.Time]("CURRENT_TIMESTAMP")) }

// Today matches rows where a time column falls on the database's current date.
func Today(field *time.Time) Cond {
	c := SQLCond("CAST(? AS date) = CURRENT_DATE", Col(field))
	c.node = needing(c.node, dialect.DateCast)

	return c
}

// SQLCond returns a condition from raw SQL. Each ? takes the next argument. An
// argument that is an Expr renders inline, and any other is a bound value. Never
// build the text from untrusted values.
func SQLCond(text string, args ...any) Cond {
	e := SQL[bool](text, args...)
	if e.err != nil {
		return Cond{err: e.err}
	}

	return Cond{node: e.node.(qb.Fragment), refs: e.refs}
}

// FailedCond returns a condition that reports err at the terminal call, for use
// by condition helpers that can fail.
func FailedCond(err error) Cond { return Cond{err: err} }

// IsNull matches rows where field is NULL.
func IsNull[V any](field *V) Cond { return nullCheck(field, false) }

// IsNotNull matches rows where field is not NULL.
func IsNotNull[V any](field *V) Cond { return nullCheck(field, true) }

// EqCol matches rows where two columns are equal.
func EqCol[V comparable](a, b *V) Cond {
	left, leftModel, err := column(a)
	if err != nil {
		return Cond{err: err}
	}

	right, rightModel, err := column(b)
	if err != nil {
		return Cond{err: err}
	}

	return Cond{
		node: qb.Compare{Left: left, Op: qb.OpEq, Right: right},
		refs: []*meta.Model{leftModel, rightModel},
	}
}

// And matches rows where every condition matches. With no conditions, it
// matches everything.
func And(conds ...Cond) Cond {
	nodes, c := merge(conds)
	c.node = qb.And{Children: nodes}

	return c
}

// Or matches rows where any condition matches. With no conditions, it matches
// nothing.
func Or(conds ...Cond) Cond {
	nodes, c := merge(conds)
	c.node = qb.Or{Children: nodes}

	return c
}

// Not matches rows where c does not match.
func Not(c Cond) Cond {
	return Cond{node: qb.Not{Child: c.node}, refs: c.refs, err: c.err}
}

// InTuples matches rows whose columns, taken together, equal one of the tuples:
//
//	ant.InTuples([]ant.FieldRef{ant.F(&O.Region), ant.F(&O.Number)},
//		[]any{"eu", 1}, []any{"us", 7})
//
// As with In, an empty list matches nothing.
func InTuples(fields []FieldRef, tuples ...[]any) Cond {
	if len(fields) == 0 {
		return Cond{err: ErrNoColumns}
	}

	out := Cond{}
	left := qb.Row{}

	for _, ref := range fields {
		if ref.err != nil {
			return Cond{err: ref.err}
		}

		out.refs = append(out.refs, ref.model)
		left.Values = append(left.Values, ref.model.Col(ref.field))
	}

	values := make([]qb.Expr, len(tuples))

	for i, tuple := range tuples {
		if len(tuple) != len(fields) {
			return Cond{
				err: fmt.Errorf(
					"ant: tuple %d has %d values for %d columns",
					i,
					len(tuple),
					len(fields),
				),
			}
		}

		row := qb.Row{Values: make([]qb.Expr, len(tuple))}

		for j, v := range tuple {
			row.Values[j] = qb.Param{Value: v}
		}

		values[i] = row
	}

	out.node = qb.In{Left: left, Values: values}

	return out
}

func merge(conds []Cond) ([]qb.Cond, Cond) {
	var out Cond

	nodes := make([]qb.Cond, 0, len(conds))

	for _, c := range conds {
		nodes = append(nodes, c.node)
		out.refs = append(out.refs, c.refs...)
		out.err = errors.Join(out.err, c.err)
	}

	return nodes, out
}

// Has matches rows with at least one related row in a one-to-many relation
// that meets conds. Build it at run time, not in a package variable, because
// it needs the related model's configuration.
func Has[R any](rel *[]R, conds ...Cond) Cond { return related[R](rel, conds, false) }

// HasNone matches rows with no related row that meets conds.
func HasNone[R any](rel *[]R, conds ...Cond) Cond { return related[R](rel, conds, true) }

// HasOne matches rows with a related row in a many-to-one or one-to-one
// relation that meets conds. See Has.
func HasOne[R any](rel **R, conds ...Cond) Cond { return related[R](rel, conds, false) }

func related[R, V any](rel *V, conds []Cond, negate bool) Cond {
	r, link, err := linkOf[R](rel)
	if err != nil {
		return Cond{err: err}
	}

	stmt, refs, err := relatedSelect(r, link, conds)
	if err != nil {
		return Cond{err: err}
	}

	return Cond{node: qb.Exists{Select: stmt, Negate: negate}, refs: refs}
}

// relatedAlias names the inner table of a relation subquery, which is needed
// when a relation refers to its own table.
const relatedAlias = "ant_related"

// relatedSelect returns SELECT 1 for the rows related to the outer row, and
// the outer models the subquery refers to.
func relatedSelect(
	r *meta.Relation,
	link meta.Link,
	conds []Cond,
) (*qb.Select, []*meta.Model, error) {
	inner := link.Target.Col(link.Foreign)
	inner.Alias = relatedAlias

	stmt := &qb.Select{
		Columns: []qb.Selected{{Value: qb.Fragment{SQL: "1"}}},
		From:    qb.Source{Table: link.Target.Table, Alias: relatedAlias},
		Where:   []qb.Cond{qb.Compare{Left: inner, Op: qb.OpEq, Right: r.Owner.Col(link.Local)}},
	}

	if p := r.Pivot; p != nil {
		const pivot = "ant_related_pivot"

		stmt.Joins = []qb.Join{
			{
				Source: qb.Source{Table: p.Table, Alias: pivot},
				On: []qb.Cond{
					qb.Compare{
						Left:  qb.Column{Alias: pivot, Name: p.Foreign},
						Op:    qb.OpEq,
						Right: inner,
					},
				},
			},
		}

		stmt.Where = []qb.Cond{
			qb.Compare{
				Left:  qb.Column{Alias: pivot, Name: p.Local},
				Op:    qb.OpEq,
				Right: r.Owner.Col(link.Local),
			},
		}
	}

	stmt.Where = append(
		stmt.Where,
		modelFilters(source{model: link.Target, alias: relatedAlias}, nil)...)

	for _, c := range r.Scope {
		stmt.Where = append(stmt.Where, qb.Aliased(c, link.Target.Table, relatedAlias))
	}

	refs := []*meta.Model{r.Owner}

	merged := And(conds...)
	if merged.err != nil {
		return nil, nil, merged.err
	}

	for _, ref := range merged.refs {
		if ref != link.Target {
			refs = append(refs, ref)
		}
	}

	if len(conds) > 0 {
		stmt.Where = append(stmt.Where, qb.Aliased(merged.node, link.Target.Table, relatedAlias))
	}

	return stmt, refs, nil
}
