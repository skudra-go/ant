package ant

import (
	"database/sql"
	"errors"
	"slices"
	"time"

	"github.com/skudra-go/ant/dialect"
	"github.com/skudra-go/ant/internal/meta"
	"github.com/skudra-go/ant/qb"
)

// Expr is a typed SQL expression: a column, an aggregate, or a raw fragment.
// V is the Go type it produces, so comparisons and mappings are checked by the
// compiler.
type Expr[V any] struct {
	node qb.Expr
	refs []*meta.Model
	err  error
}

// Col returns the expression for a column.
func Col[V any](field *V) Expr[V] {
	col, model, err := column(field)
	if err != nil {
		return Expr[V]{err: err}
	}

	return Expr[V]{node: col, refs: []*meta.Model{model}}
}

// SQL returns an expression from raw SQL. Each ? takes the next argument. An
// argument that is an Expr renders inline, and any other is a bound value.
// Never build the text from untrusted values.
func SQL[V any](text string, args ...any) Expr[V] {
	e := Expr[V]{}

	nodes := make([]qb.Expr, len(args))

	for i, a := range args {
		if inner, ok := a.(exprNode); ok {
			node, refs, err := inner.parts()
			if err != nil {
				e.err = err

				return e
			}

			nodes[i] = node

			e.refs = append(e.refs, refs...)

			continue
		}

		nodes[i] = qb.Param{Value: a}
	}

	e.node = qb.Fragment{SQL: text, Args: nodes}

	return e
}

// needing marks raw SQL as depending on f, so a database without it gets a clear
// error instead of a syntax error.
func needing(node any, f dialect.Feature) qb.Fragment {
	fragment, _ := node.(qb.Fragment)
	fragment.Needs = string(f)

	return fragment
}

// FailedExpr returns an expression that reports err at the terminal call, for
// helpers that can fail while building one.
func FailedExpr[V any](err error) Expr[V] { return Expr[V]{err: err} }

// exprNode is an Expr of any type.
type exprNode interface {
	parts() (qb.Expr, []*meta.Model, error)
}

func (e Expr[V]) parts() (qb.Expr, []*meta.Model, error) { return e.node, e.refs, e.err }

func (e Expr[V]) compare(op qb.Op, v V) Cond {
	return Cond{
		node: qb.Compare{Left: e.node, Op: op, Right: qb.Param{Value: v}},
		refs: e.refs,
		err:  e.err,
	}
}

// Eq matches rows where the expression equals v. A v stored as NULL, such as a
// nil pointer or an invalid sql.Null[T], matches NULL, as with ant.Eq.
func (e Expr[V]) Eq(v V) Cond {
	if isNull(v) {
		return e.IsNull()
	}

	return e.compare(qb.OpEq, v)
}

// Ne matches rows where the expression does not equal v. A v stored as NULL
// matches non-NULL values, as with ant.Ne.
func (e Expr[V]) Ne(v V) Cond {
	if isNull(v) {
		return e.IsNotNull()
	}

	return e.compare(qb.OpNe, v)
}

// Gt matches rows where the expression is greater than v.
func (e Expr[V]) Gt(v V) Cond { return e.compare(qb.OpGt, v) }

// Gte matches rows where the expression is greater than or equal to v.
func (e Expr[V]) Gte(v V) Cond { return e.compare(qb.OpGte, v) }

// Lt matches rows where the expression is less than v.
func (e Expr[V]) Lt(v V) Cond { return e.compare(qb.OpLt, v) }

// Lte matches rows where the expression is less than or equal to v.
func (e Expr[V]) Lte(v V) Cond { return e.compare(qb.OpLte, v) }

func (e Expr[V]) compareExpr(op qb.Op, other Expr[V]) Cond {
	err := errors.Join(e.err, other.err)

	return Cond{
		node: qb.Compare{Left: e.node, Op: op, Right: other.node},
		refs: append(slices.Clone(e.refs), other.refs...),
		err:  err,
	}
}

// EqExpr matches rows where two expressions are equal.
func (e Expr[V]) EqExpr(other Expr[V]) Cond { return e.compareExpr(qb.OpEq, other) }

// GtExpr matches rows where the expression is greater than another expression.
func (e Expr[V]) GtExpr(other Expr[V]) Cond { return e.compareExpr(qb.OpGt, other) }

// LtExpr matches rows where the expression is less than another expression.
func (e Expr[V]) LtExpr(other Expr[V]) Cond { return e.compareExpr(qb.OpLt, other) }

// IsNull matches rows where the expression is NULL.
func (e Expr[V]) IsNull() Cond {
	return Cond{node: qb.IsNull{Left: e.node}, refs: e.refs, err: e.err}
}

// IsNotNull matches rows where the expression is not NULL.
func (e Expr[V]) IsNotNull() Cond {
	return Cond{node: qb.IsNull{Left: e.node, Negate: true}, refs: e.refs, err: e.err}
}

func (e Expr[V]) binary(op string, v V) Expr[V] {
	return Expr[V]{
		node: qb.Binary{Left: e.node, Op: op, Right: qb.Param{Value: v}},
		refs: e.refs,
		err:  e.err,
	}
}

// Plus adds v.
func (e Expr[V]) Plus(v V) Expr[V] { return e.binary("+", v) }

// Minus subtracts v.
func (e Expr[V]) Minus(v V) Expr[V] { return e.binary("-", v) }

// Times multiplies by v.
func (e Expr[V]) Times(v V) Expr[V] { return e.binary("*", v) }

// Div divides by v.
func (e Expr[V]) Div(v V) Expr[V] { return e.binary("/", v) }

// Over makes an aggregate or ranking function a window function.
func (e Expr[V]) Over(w Window) Expr[V] {
	f, ok := e.node.(qb.Func)
	if !ok {
		return Expr[V]{err: errors.New("ant: Over needs a function")}
	}

	if w.err != nil {
		return Expr[V]{err: w.err}
	}

	f.Over = &qb.Window{PartitionBy: w.partition, OrderBy: w.order}

	return Expr[V]{node: f, refs: append(e.refs, w.refs...), err: e.err}
}

// Filter limits an aggregate to rows that meet conds.
func (e Expr[V]) Filter(conds ...Cond) Expr[V] {
	f, ok := e.node.(qb.Func)
	if !ok {
		return Expr[V]{err: errors.New("ant: Filter needs a function")}
	}

	refs := e.refs

	for _, c := range conds {
		if c.err != nil {
			return Expr[V]{err: c.err}
		}

		refs = append(refs, c.refs...)
		f.Filter = append(f.Filter, c.node)
	}

	return Expr[V]{node: f, refs: refs, err: e.err}
}

// Asc orders by the expression, ascending.
func (e Expr[V]) Asc() Sort {
	return Sort{order: qb.Order{Value: e.node}, refs: e.refs, err: e.err}
}

// Desc orders by the expression, descending.
func (e Expr[V]) Desc() Sort {
	return Sort{order: qb.Order{Value: e.node, Desc: true}, refs: e.refs, err: e.err}
}

// Sort is one term of an ordering, from Asc or Desc.
type Sort struct {
	order qb.Order
	refs  []*meta.Model
	err   error
}

// NullsFirst sorts NULL values before all others.
func (s Sort) NullsFirst() Sort {
	s.order.Nulls = qb.NullsFirst

	return s
}

// NullsLast sorts NULL values after all others.
func (s Sort) NullsLast() Sort {
	s.order.Nulls = qb.NullsLast

	return s
}

// Mapping sets one field of a projection, from Into or IntoExpr.
type Mapping struct {
	dst  *meta.Field
	node qb.Expr
	refs []*meta.Model
	err  error
}

// Into maps a column to a field of the projected type. dst is a field of a
// probe of that type, and src a field of a probe in the query.
func Into[V any](dst *V, src *V) Mapping { return IntoExpr(dst, Col(src)) }

// IntoExpr maps an expression to a field of the projected type.
func IntoExpr[V any](dst *V, src Expr[V]) Mapping {
	if src.err != nil {
		return Mapping{err: src.err}
	}

	f, _, err := fieldOf(dst)
	if err != nil {
		return Mapping{err: err}
	}

	return Mapping{dst: f, node: src.node, refs: src.refs}
}

// Number is a type that SUM and AVG accept.
type Number interface {
	~int | ~int8 | ~int16 | ~int32 | ~int64 |
		~uint | ~uint8 | ~uint16 | ~uint32 | ~uint64 |
		~float32 | ~float64
}

func relatedExpr[O, R, V any](rel *V, conds []Cond, value func(*meta.Field) qb.Expr) Expr[O] {
	r, link, err := linkOf[R](rel)
	if err != nil {
		return Expr[O]{err: err}
	}

	stmt, refs, err := relatedSelect(r, link, conds)
	if err != nil {
		return Expr[O]{err: err}
	}

	stmt.Columns = []qb.Selected{{Value: value(link.Foreign)}}

	return Expr[O]{node: qb.Subquery{Select: stmt}, refs: refs}
}

// CountRelated counts the rows related through a one-to-many relation that
// meet conds. Use it with Query.With or in a Mapping.
func CountRelated[R any](rel *[]R, conds ...Cond) Expr[int64] {
	return relatedExpr[int64, R](rel, conds, func(*meta.Field) qb.Expr {
		return qb.Func{Name: "COUNT", Args: []qb.Expr{qb.Star{}}}
	})
}

// aggregateRelated applies an aggregate to a field of the related rows.
func aggregateRelated[O, R, V any](
	name string,
	rel *[]R,
	field *V,
	conds []Cond,
	wrap func(qb.Expr) qb.Expr,
) Expr[O] {
	f, _, err := fieldOf(field)
	if err != nil {
		return Expr[O]{err: err}
	}

	return relatedExpr[O, R](rel, conds, func(*meta.Field) qb.Expr {
		column := qb.Column{Alias: relatedAlias, Name: f.Column}

		return wrap(qb.Func{Name: name, Args: []qb.Expr{column}})
	})
}

func unchanged(e qb.Expr) qb.Expr { return e }

// SumRelated adds up a field of the related rows, giving zero if there are
// none.
func SumRelated[R any, V Number](rel *[]R, field *V, conds ...Cond) Expr[V] {
	return aggregateRelated[V](`SUM`, rel, field, conds, func(sum qb.Expr) qb.Expr {
		return qb.Func{Name: "COALESCE", Args: []qb.Expr{sum, qb.Fragment{SQL: "0"}}}
	})
}

// AvgRelated averages a field of the related rows. It is NULL if there are
// none.
func AvgRelated[R any, V Number](rel *[]R, field *V, conds ...Cond) Expr[sql.Null[float64]] {
	return aggregateRelated[sql.Null[float64]]("AVG", rel, field, conds, unchanged)
}

// MinRelated returns the smallest value of a field of the related rows. It is
// NULL if there are none.
func MinRelated[R any, V Ordered](rel *[]R, field *V, conds ...Cond) Expr[sql.Null[V]] {
	return aggregateRelated[sql.Null[V]]("MIN", rel, field, conds, unchanged)
}

// MaxRelated returns the largest value of a field of the related rows. It is
// NULL if there are none.
func MaxRelated[R any, V Ordered](rel *[]R, field *V, conds ...Cond) Expr[sql.Null[V]] {
	return aggregateRelated[sql.Null[V]]("MAX", rel, field, conds, unchanged)
}

func function[O any](name string, distinct bool, args ...exprNode) Expr[O] {
	f := qb.Func{Name: name, Distinct: distinct}

	var refs []*meta.Model

	for _, a := range args {
		node, r, err := a.parts()
		if err != nil {
			return Expr[O]{err: err}
		}

		f.Args = append(f.Args, node)
		refs = append(refs, r...)
	}

	return Expr[O]{node: f, refs: refs}
}

// Count counts rows.
func Count() Expr[int64] {
	return Expr[int64]{node: qb.Func{Name: "COUNT", Args: []qb.Expr{qb.Star{}}}}
}

// CountOf counts rows where field is not NULL.
func CountOf[V any](field *V) Expr[int64] { return function[int64]("COUNT", false, Col(field)) }

// CountDistinct counts the different non-NULL values of field.
func CountDistinct[V any](
	field *V,
) Expr[int64] {
	return function[int64]("COUNT", true, Col(field))
}

// Sum adds up a column.
func Sum[V Number](field *V) Expr[V] { return function[V]("SUM", false, Col(field)) }

// Avg averages a column.
func Avg[V Number](field *V) Expr[float64] { return function[float64]("AVG", false, Col(field)) }

// Min returns a column's smallest value.
func Min[V Ordered](field *V) Expr[V] { return function[V]("MIN", false, Col(field)) }

// Max returns a column's largest value.
func Max[V Ordered](field *V) Expr[V] { return function[V]("MAX", false, Col(field)) }

// Func calls a database function by name. Arguments that are an Expr render
// inline, and any other is a bound value. The name is not quoted, so it must
// never come from untrusted input.
func Func[R any](name string, args ...any) Expr[R] {
	var nodes []exprNode

	for _, a := range args {
		if e, ok := a.(exprNode); ok {
			nodes = append(nodes, e)
		} else {
			nodes = append(nodes, Expr[any]{node: qb.Param{Value: a}})
		}
	}

	return function[R](name, false, nodes...)
}

// Coalesce returns the first non-NULL of field and fallback.
func Coalesce[V any](
	field *V,
	fallback V,
) Expr[V] {
	return Func[V]("COALESCE", Col(field), fallback)
}

// Lower lowercases a text column.
func Lower[V ~string](field *V) Expr[V] { return function[V]("LOWER", false, Col(field)) }

// Upper uppercases a text column.
func Upper[V ~string](field *V) Expr[V] { return function[V]("UPPER", false, Col(field)) }

// Length returns the length of a text column, in characters.
func Length[V ~string](field *V) Expr[int] { return function[int]("LENGTH", false, Col(field)) }

// Collate applies a collation to a text column.
func Collate[V ~string](field *V, collation string) Expr[V] {
	e := Col(field)

	return Expr[V]{node: qb.Collate{Value: e.node, Collation: collation}, refs: e.refs, err: e.err}
}

// DatePart extracts part of a date column, such as "year" or "month".
func DatePart(field *time.Time, part string) Expr[float64] {
	e := SQL[float64]("DATE_PART(?, ?)", part, Col(field))
	e.node = needing(e.node, dialect.DatePart)

	return e
}

// RowNumber numbers rows within a window, from 1.
func RowNumber() Expr[int64] { return Expr[int64]{node: qb.Func{Name: "ROW_NUMBER"}} }

// Rank ranks rows within a window, leaving gaps after ties.
func Rank() Expr[int64] { return Expr[int64]{node: qb.Func{Name: "RANK"}} }

// DenseRank ranks rows within a window, with no gaps after ties.
func DenseRank() Expr[int64] { return Expr[int64]{node: qb.Func{Name: "DENSE_RANK"}} }

// Lag returns field from the previous row of a window.
func Lag[V any](
	field *V,
) Expr[sql.Null[V]] {
	return function[sql.Null[V]]("LAG", false, Col(field))
}

// Lead returns field from the next row of a window.
func Lead[V any](
	field *V,
) Expr[sql.Null[V]] {
	return function[sql.Null[V]]("LEAD", false, Col(field))
}

// Window is an OVER clause, from PartitionBy or OrderedBy.
type Window struct {
	partition []qb.Expr
	order     []qb.Order
	refs      []*meta.Model
	err       error
}

// PartitionBy starts a window divided by a column.
func PartitionBy[V any](field *V) Window { return Window{}.PartitionBy(Col(field)) }

// OrderedBy starts a window over all rows in the given order.
func OrderedBy(terms ...Sort) Window { return Window{}.OrderBy(terms...) }

// PartitionBy divides the window by more expressions.
func (w Window) PartitionBy(exprs ...exprNode) Window {
	w.partition = slices.Clone(w.partition)

	for _, e := range exprs {
		node, refs, err := e.parts()
		if err != nil {
			w.err = err

			continue
		}

		w.partition = append(w.partition, node)
		w.refs = append(slices.Clone(w.refs), refs...)
	}

	return w
}

// OrderBy orders rows within the window.
func (w Window) OrderBy(terms ...Sort) Window {
	w.order = slices.Clone(w.order)

	for _, s := range terms {
		if s.err != nil {
			w.err = s.err

			continue
		}

		w.order = append(w.order, s.order)
		w.refs = append(slices.Clone(w.refs), s.refs...)
	}

	return w
}

// CaseExpr builds a CASE expression. See Case.
type CaseExpr[V any] struct {
	node qb.Case
	refs []*meta.Model
	err  error
}

// Case starts a CASE expression:
//
//	ant.Case[string]().When(ant.Lt(&Users.Age, 18), "minor").Else("adult")
func Case[V any]() CaseExpr[V] { return CaseExpr[V]{} }

// When adds a branch.
func (c CaseExpr[V]) When(cond Cond, then V) CaseExpr[V] {
	if cond.err != nil {
		c.err = cond.err

		return c
	}

	c.node.Whens = append(
		slices.Clone(c.node.Whens),
		qb.When{If: cond.node, Then: qb.Param{Value: then}},
	)

	c.refs = append(slices.Clone(c.refs), cond.refs...)

	return c
}

// WhenExpr adds a branch whose result is an expression, such as another column.
func (c CaseExpr[V]) WhenExpr(cond Cond, then Expr[V]) CaseExpr[V] {
	if cond.err != nil || then.err != nil {
		c.err = errors.Join(cond.err, then.err)

		return c
	}

	c.node.Whens = append(slices.Clone(c.node.Whens), qb.When{If: cond.node, Then: then.node})
	c.refs = append(append(slices.Clone(c.refs), cond.refs...), then.refs...)

	return c
}

// ElseExpr finishes the expression, with an expression for rows no branch matched.
func (c CaseExpr[V]) ElseExpr(value Expr[V]) Expr[V] {
	c.node.Else = value.node

	return Expr[V]{
		node: c.node,
		refs: append(c.refs, value.refs...),
		err:  errors.Join(c.err, value.err),
	}
}

// Else finishes the expression with a value for when no branch matches.
func (c CaseExpr[V]) Else(value V) Expr[V] {
	c.node.Else = qb.Param{Value: value}

	return Expr[V]{node: c.node, refs: c.refs, err: c.err}
}

// End finishes the expression, which is NULL when no branch matches.
func (c CaseExpr[V]) End() Expr[sql.Null[V]] {
	return Expr[sql.Null[V]]{node: c.node, refs: c.refs, err: c.err}
}
