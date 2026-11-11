package ant

import (
	"errors"
	"fmt"
	"slices"
	"unsafe"

	"github.com/skudra-go/ant/internal/meta"
	"github.com/skudra-go/ant/qb"
	"github.com/skudra-go/ant/schema"
)

// Sub is a query for one column of type V, for use in a condition or as a value.
type Sub[V any] struct {
	stmt *qb.Select
	refs []*meta.Model
	err  error
}

// SubOf makes q a subquery selecting field. A subquery can refer to models of
// the outer query in its conditions.
func SubOf[V any](q Selectable, field *V) Sub[V] { return SubExpr(q, Col(field)) }

// SubExpr makes q a subquery selecting an expression.
func SubExpr[V any](q Selectable, value Expr[V]) Sub[V] {
	b := q.base().cloned()

	if value.err != nil {
		return Sub[V]{err: value.err}
	}

	if b.err != nil {
		return Sub[V]{err: b.err}
	}

	b.refs = append(b.refs, value.refs...)

	return Sub[V]{stmt: b.statement([]qb.Selected{{Value: value.node}}), refs: b.outerRefs()}
}

// Expr uses the subquery as a single value. It must return at most one row.
func (s Sub[V]) Expr() Expr[V] {
	return Expr[V]{node: qb.Subquery{Select: s.stmt}, refs: s.refs, err: s.err}
}

func inSub[V any](field *V, sub Sub[V], negate bool) Cond {
	if sub.err != nil {
		return Cond{err: sub.err}
	}

	c := on(field, func(col qb.Column) qb.Cond {
		return qb.In{Left: col, Sub: sub.stmt, Negate: negate}
	})
	c.refs = append(c.refs, sub.refs...)

	return c
}

// InSub matches rows where field is one of the subquery's values.
func InSub[V any](field *V, sub Sub[V]) Cond { return inSub(field, sub, false) }

// NotInSub matches rows where field is none of the subquery's values. As in SQL,
// a NULL among the values means nothing matches.
func NotInSub[V any](field *V, sub Sub[V]) Cond { return inSub(field, sub, true) }

func exists(q Selectable, negate bool) Cond {
	b := q.base().cloned()
	if b.err != nil {
		return Cond{err: b.err}
	}

	stmt := b.statement([]qb.Selected{{Value: qb.Fragment{SQL: "1"}}})

	return Cond{node: qb.Exists{Select: stmt, Negate: negate}, refs: b.outerRefs()}
}

// Exists matches rows for which q returns anything. Conditions in q can refer to
// the outer query's models.
func Exists(q Selectable) Cond { return exists(q, false) }

// NotExists matches rows for which q returns nothing.
func NotExists(q Selectable) Cond { return exists(q, true) }

// Grouped is a query with GROUP BY. Read it with Project.
type Grouped struct {
	builder
}

func (g *Grouped) base() *builder { return &g.builder }

func (g *Grouped) next() *Grouped { return &Grouped{builder: g.builder.cloned()} }

func groupedBy(b builder, e exprNode) *Grouped {
	g := &Grouped{builder: b.cloned()}

	return g.add(e)
}

func (g *Grouped) add(e exprNode) *Grouped {
	n := g.next()

	node, refs, err := e.parts()
	if err != nil {
		n.fail(err)

		return n
	}

	n.refs = append(n.refs, refs...)
	n.group = append(n.group, node)

	return n
}

// GroupBy groups rows by a column. Add more with Then, and read the results with
// Project:
//
//	ant.Project[Stat](db.Posts.GroupBy(&Posts.UserID),
//		ant.Into(&Stats.UserID, &Posts.UserID),
//		ant.IntoExpr(&Stats.Posts, ant.Count()))
func (q *Query[T]) GroupBy[V any](field *V) *Grouped { return groupedBy(q.builder, Col(field)) }

// GroupByExpr groups rows by an expression.
func (q *Query[T]) GroupByExpr[V any](e Expr[V]) *Grouped { return groupedBy(q.builder, e) }

// Then groups by another column as well.
func Then[V any](g *Grouped, field *V) *Grouped { return g.add(Col(field)) }

// Having keeps groups that meet conds, which can use aggregates such as
// ant.Count().Gt(1).
func (g *Grouped) Having(conds ...Cond) *Grouped {
	n := g.next()

	for _, c := range conds {
		if c.err != nil {
			n.fail(c.err)

			continue
		}

		n.refs = append(n.refs, c.refs...)
		n.having = append(n.having, c.node)
	}

	return n
}

// Order orders the groups by terms from Asc and Desc.
func (g *Grouped) Order(terms ...Sort) *Grouped {
	n := g.next()
	n.order = nil
	n.addOrder(terms)

	return n
}

// Limit caps the number of groups returned.
func (g *Grouped) Limit(limit int) *Grouped {
	n := g.next()
	n.limit = &limit

	return n
}

// Offset skips the first groups.
func (g *Grouped) Offset(offset int) *Grouped {
	n := g.next()
	n.offset = &offset

	return n
}

// FromRaw returns a query over raw SQL, which conditions, ordering, and paging
// then build on. The SQL must return T's columns. Results are not tracked.
func FromRaw[T any](h Handle, text string, args ...any) *Query[T] {
	var s *state

	if h != nil {
		s = h.context().state
	}

	q := newQuery[T](s)

	e := SQL[T](text, args...)
	if e.err != nil {
		q.fail(e.err)

		return q
	}

	fragment, ok := e.node.(qb.Fragment)
	if !ok {
		q.fail(fmt.Errorf("ant: FromRaw needs a SQL fragment"))

		return q
	}

	q.from = &qb.Source{Raw: &fragment, Alias: q.model().Table.Name}

	return q
}

// FromFunc reads from a table-valued function, written with ? for its arguments,
// as in generate_series(?, ?). Name its result columns if they differ from T's:
// unnest(?) AS "users"(name).
func FromFunc[T any](h Handle, call string, args ...any) *Query[T] {
	return FromRaw[T](h, "SELECT * FROM "+call, args...)
}

// Values reads rows held in memory as if they were a table, for joining against a
// list or using it in a subquery. It works on every dialect: PostgreSQL casts
// each value to its column's type, and the others write the rows as they are.
// Results are not tracked.
//
//	wanted := ant.Values(db, Wanted{ID: 1, Limit: 5}, Wanted{ID: 2, Limit: 9})
//	ant.JoinSub(db.Users.Query(), wanted, ant.EqCol(&Users.ID, &Wanteds.ID))
func Values[T any](h Handle, rows ...T) *Query[T] {
	var s *state

	if h != nil {
		s = h.context().state
	}

	q := newQuery[T](s)

	if s == nil {
		q.fail(ErrNotOpen)

		return q
	}

	if len(rows) == 0 {
		q.fail(ErrNoRows)

		return q
	}
	// Casts tell PostgreSQL what a bound value is. A dialect with no column types
	// takes that from the values.
	d, err := schema.Of(s.dialect)
	if err != nil && !errors.Is(err, schema.ErrUnsupported) {
		q.fail(err)

		return q
	}

	model := q.model()
	fields := model.Stored()
	types := make([]string, len(fields))
	values := &qb.Values{}

	for i, f := range fields {
		values.Columns = append(values.Columns, f.Column)

		if err == nil {
			valueType, _ := columnValue(f.Type)
			types[i] = columnTypeFor(d, valueType, f.Storage)
		}
	}

	for r := range rows {
		row := make([]qb.Expr, len(fields))

		for i, f := range fields {
			var cell qb.Expr = qb.Param{Value: f.Value(unsafe.Pointer(&rows[r])).Interface()}

			if types[i] != "" {
				cell = qb.Cast{Value: cell, Type: types[i]}
			}

			row[i] = cell
		}

		values.Rows = append(values.Rows, row)
	}

	q.from = &qb.Source{Values: values, Alias: model.Table.Name}
	q.fromKey = slices.Contains(fields, model.Key())

	return q
}
