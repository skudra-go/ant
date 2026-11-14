package ant

import (
	"context"
	"maps"
	"slices"
	"sync"
	"sync/atomic"

	"github.com/skudra-go/ant/dialect"
	"github.com/skudra-go/ant/internal/meta"
	"github.com/skudra-go/ant/qb"
)

// The statements ant runs most, a Find, a First or All with a few plain
// conditions, and an Insert of one entity, have the same SQL every time they
// run with the same shape: only the values bound to them change. So their SQL is
// kept, by shape, and a statement with a known shape binds its values to the
// kept SQL, with no statement built and none rendered.
//
// A shape is learned by rendering the statement once with markers in place of
// its values. The order in which the markers come back is the order in which
// the dialect binds them, which differs between dialects, as SQL Server binds
// OFFSET before the row count. Anything the cache does not take, such as a join,
// an OR, IN, a raw condition, a global filter that reads the context, or Debug,
// goes the usual way.

const (
	shapeConds  = 6
	shapeOrders = 3
	// shapeValues is the most values a select shape binds: two for each
	// condition, and the row count and the offset.
	shapeValues = 2*shapeConds + 2
	// maxPlans bounds the shapes kept for each context. Shapes come from code,
	// so there are few, and past the bound a new shape just goes the usual way.
	maxPlans = 1024
)

// shapeSlot marks a bound value in the statement a plan is learned from. It is
// the index of the value in the shape's values.
type shapeSlot int

// The row count and offset are ints in a statement, not values, so they are
// marked with ints that no query asks for.
const (
	limitMark  = -1 << 40
	offsetMark = limitMark + 1
)

type condKind uint8

const (
	compareCond condKind = iota + 1
	nullCond
	notNullCond
	betweenCond
)

// condShape is one condition on a column of the queried table, with its values
// left out.
type condShape struct {
	column string
	op     qb.Op
	kind   condKind
}

type orderShape struct {
	column string
	desc   bool
	nulls  qb.NullsOrder
}

// selectShape is what the SQL of a plain read depends on. It holds no slice, so
// it is a map key that takes no allocation to look up.
type selectShape struct {
	model   *meta.Model
	conds   [shapeConds]condShape
	orders  [shapeOrders]orderShape
	filters uint8 // which of the model's static filters apply, by index
	limit   bool
	offset  bool
}

// insertShape is what the SQL of a one-row INSERT depends on: the model, and
// which of its writable fields the database fills because they are zero.
type insertShape struct {
	model  *meta.Model
	filled uint64
}

// plan is the SQL of a shape, and for each value it binds, the index of that
// value in the shape's values. A plan with no text is a shape that turned out
// to bind something no marker stands for, which goes the usual way from then on.
type plan struct {
	text     string
	slots    []int
	identity bool // slots[i] == i for every i
}

func (p *plan) usable() bool { return p.text != "" }

// args returns the values to bind to the plan's SQL for d.
func (p *plan) args(d dialect.Dialect, values []any) []any {
	if len(p.slots) == 0 {
		return nil
	}

	args := make([]any, len(p.slots))

	for i, slot := range p.slots {
		args[i] = dialect.Arg(d, values[slot])
	}

	return args
}

// learn renders stmt, whose values are markers, and returns its plan. limit and
// offset are the indexes of the row count and the offset in the shape's values.
func learn(d dialect.Dialect, stmt qb.Statement, limit, offset int) *plan {
	text, args, err := d.Render(qb.Contextual{Context: context.Background(), Statement: stmt})
	if err != nil {
		// The usual way reports the error.
		return &plan{}
	}

	p := &plan{text: text, slots: make([]int, len(args)), identity: true}

	for i, a := range args {
		switch v := a.(type) {
		case shapeSlot:
			p.slots[i] = int(v)
		case int:
			switch {
			case v == limitMark && limit >= 0:
				p.slots[i] = limit
			case v == offsetMark && offset >= 0:
				p.slots[i] = offset
			default:
				return &plan{}
			}
		default:
			// A value from somewhere the shape does not see.
			return &plan{}
		}

		p.identity = p.identity && p.slots[i] == i
	}

	return p
}

// plans holds the plans of one kind of shape. Readers take no lock: they load a
// map that is never changed, and a writer copies it to add to it.
type plans[K comparable] struct {
	mu sync.Mutex
	m  atomic.Pointer[map[K]*plan]
}

func (c *plans[K]) get(k K) *plan {
	if m := c.m.Load(); m != nil {
		return (*m)[k]
	}

	return nil
}

// put keeps p for k, unless the cache is full, and returns the plan kept for
// k, which another goroutine may have kept first.
func (c *plans[K]) put(k K, p *plan) *plan {
	c.mu.Lock()
	defer c.mu.Unlock()

	current := c.m.Load()

	if current != nil {
		if kept, ok := (*current)[k]; ok {
			return kept
		}

		if len(*current) >= maxPlans {
			return p
		}
	}

	next := make(map[K]*plan, 1)

	if current != nil {
		next = maps.Clone(*current)
	}

	next[k] = p
	c.m.Store(&next)

	return p
}

// boundValues holds the values of a shape, in order, on the caller's stack.
type boundValues struct {
	v [shapeValues]any
	n int
}

func (b *boundValues) add(v any) bool {
	if b.n == len(b.v) {
		return false
	}

	b.v[b.n] = v
	b.n++

	return true
}

// rootColumn returns the name of a column of table that e names, unaliased.
func rootColumn(e qb.Expr, table *qb.TableRef) (string, bool) {
	c, ok := e.(qb.Column)
	if !ok || c.Alias != "" || c.Table != table {
		return "", false
	}

	return c.Name, true
}

// condShapeOf returns the shape of c, a condition on table, adding its values,
// or false if the cache does not take it.
func condShapeOf(c qb.Cond, table *qb.TableRef, values *boundValues) (condShape, bool) {
	switch c := c.(type) {
	case qb.Compare:
		column, ok := rootColumn(c.Left, table)
		p, isParam := qb.ParamOf(c.Right)

		if !ok || !isParam || !values.add(p.Value) {
			return condShape{}, false
		}

		return condShape{column: column, op: c.Op, kind: compareCond}, true
	case qb.IsNull:
		column, ok := rootColumn(c.Left, table)
		if !ok {
			return condShape{}, false
		}

		if c.Negate {
			return condShape{column: column, kind: notNullCond}, true
		}

		return condShape{column: column, kind: nullCond}, true
	case qb.Between:
		column, ok := rootColumn(c.Left, table)
		low, lowParam := qb.ParamOf(c.Low)
		high, highParam := qb.ParamOf(c.High)

		if !ok || !lowParam || !highParam || !values.add(low.Value) || !values.add(high.Value) {
			return condShape{}, false
		}

		return condShape{column: column, kind: betweenCond}, true
	}

	return condShape{}, false
}

// withSlots returns c, a condition condShapeOf took, with markers in place of
// its values, numbered from *next.
func withSlots(c qb.Cond, next *int) qb.Cond {
	slot := func() qb.Expr {
		p := qb.Param{Value: shapeSlot(*next)}
		*next++

		return p
	}

	switch c := c.(type) {
	case qb.Compare:
		c.Right = slot()

		return c
	case qb.Between:
		c.Low = slot()
		c.High = slot()

		return c
	}

	return c
}

// shapeOf returns q's shape, read with the given row count, and with a key
// condition after its own if key is set, and adds its values to values. It
// returns false if the cache does not take q.
func (q *Query[T]) shapeOf(
	key *meta.Field,
	keyValue any,
	limit *int,
	values *boundValues,
) (selectShape, bool) {
	b := &q.builder

	if q.debug || b.from != nil || len(b.with) > 0 || b.recursive || b.distinct ||
		b.lock != nil || b.comment != "" || b.hint != "" || len(b.joins) > 0 ||
		len(b.group) > 0 || len(b.having) > 0 || len(b.sources) != 1 ||
		b.sources[0].alias != "" || len(q.inJoin) > 0 || len(q.selected) > 0 ||
		len(q.omitted) > 0 || len(q.computed) > 0 {
		return selectShape{}, false
	}

	model := b.sources[0].model
	shape := selectShape{model: model}

	if !b.ignoreAll {
		for i, f := range model.Filters {
			if slices.Contains(b.ignored, f.Name) {
				continue
			}

			if !f.Static || i >= 8 {
				return selectShape{}, false
			}

			shape.filters |= 1 << i
		}
	}

	conds := len(b.where)

	if key != nil {
		conds++
	}

	if conds > shapeConds || len(b.order) > shapeOrders {
		return selectShape{}, false
	}

	for i, c := range b.where {
		s, ok := condShapeOf(c, model.Table, values)
		if !ok {
			return selectShape{}, false
		}

		shape.conds[i] = s
	}

	if key != nil {
		if !values.add(keyValue) {
			return selectShape{}, false
		}

		shape.conds[len(b.where)] = condShape{column: key.Column, op: qb.OpEq, kind: compareCond}
	}

	for i, o := range b.order {
		column, ok := rootColumn(o.Value, model.Table)
		if !ok {
			return selectShape{}, false
		}

		shape.orders[i] = orderShape{column: column, desc: o.Desc, nulls: o.Nulls}
	}

	if limit != nil {
		shape.limit = true
		values.add(*limit)
	}

	if b.offset != nil {
		shape.offset = true
		values.add(*b.offset)
	}

	return shape, true
}

// learnSelect returns the plan of q's shape, from its statement with markers in
// place of its values.
func (q *Query[T]) learnSelect(key *meta.Field, limit *int) *plan {
	n := q.next()

	if key != nil {
		n.where = append(n.where, keyEquals(n.model(), key, nil)...)
	}

	next := 0
	where := make([]qb.Cond, len(n.where))

	for i, c := range n.where {
		where[i] = withSlots(c, &next)
	}

	n.where = where

	limitSlot, offsetSlot := -1, -1

	var marked *int

	if limit != nil {
		limitSlot, next = next, next+1
		marked = new(int(limitMark))
	}

	if n.offset != nil {
		offsetSlot = next
		n.offset = new(int(offsetMark))
	}

	stmt := n.statement(n.selection())
	stmt.Limit = marked

	return learn(n.state.dialect, stmt, limitSlot, offsetSlot)
}

// cachedSQL returns the SQL and values of q's read with the given row count,
// and a key condition if key is set, from the plan of its shape, or false if
// the cache does not take q.
func (q *Query[T]) cachedSQL(key *meta.Field, keyValue any, limit *int) (string, []any, bool) {
	var values boundValues

	shape, ok := q.shapeOf(key, keyValue, limit, &values)
	if !ok {
		return "", nil, false
	}

	cache := &q.state.shared.selects

	p := cache.get(shape)
	if p == nil {
		p = cache.put(shape, q.learnSelect(key, limit))
	}

	if !p.usable() {
		return "", nil, false
	}

	return p.text, p.args(q.state.dialect, values.v[:values.n]), true
}

// filledFields says which writable fields of a model the database fills in an
// INSERT, by their index in Model.Writable. The first 64 are a mask, which is
// what an insert shape holds, and any past them a list, which the cache does
// not take.
type filledFields struct {
	mask uint64
	rest []bool
}

func (f *filledFields) set(i, of int) {
	if i < 64 {
		f.mask |= 1 << i

		return
	}

	if f.rest == nil {
		f.rest = make([]bool, of)
	}

	f.rest[i] = true
}

func (f *filledFields) has(i int) bool {
	if i < 64 {
		return f.mask&(1<<i) != 0
	}

	return f.rest != nil && f.rest[i]
}

// insertSQL returns the SQL and values of the one-row INSERT of n, whose
// columns are the writable fields the database does not fill, from the plan of
// its shape. returning are the fields the INSERT reads back.
func (u *unit) insertSQL(
	n *node,
	writable []*meta.Field,
	filled *filledFields,
	returning []*meta.Field,
) (string, []any, error) {
	values := make([]any, 0, len(writable))

	for i, f := range writable {
		if !filled.has(i) {
			values = append(values, fieldParam(f, f.Value(n.entity).Interface()).Value)
		}
	}

	if filled.rest != nil {
		return u.s.dialect.Render(oneRowInsert(n.model, writable, filled, returning, values))
	}

	shape := insertShape{model: n.model, filled: filled.mask}
	cache := &u.s.shared.inserts

	p := cache.get(shape)
	if p == nil {
		marked := make([]any, len(values))
		for i := range marked {
			marked[i] = shapeSlot(i)
		}

		p = cache.put(shape, learn(
			u.s.dialect,
			oneRowInsert(n.model, writable, filled, returning, marked),
			-1,
			-1,
		))
	}

	if !p.usable() {
		return u.s.dialect.Render(oneRowInsert(n.model, writable, filled, returning, values))
	}

	if p.identity {
		for i, v := range values {
			values[i] = dialect.Arg(u.s.dialect, v)
		}

		return p.text, values, nil
	}

	return p.text, p.args(u.s.dialect, values), nil
}

// oneRowInsert returns the one-row INSERT of a model: the writable fields
// the database does not fill, bound to values in order, returning the given
// fields.
func oneRowInsert(
	model *meta.Model,
	writable []*meta.Field,
	filled *filledFields,
	returning []*meta.Field,
	values []any,
) *qb.Insert {
	stmt := &qb.Insert{Table: model.Table, Columns: make([]string, 0, len(values))}
	row := make([]qb.Expr, 0, len(values))
	params := make([]qb.Param, len(values))

	for i, f := range writable {
		if filled.has(i) {
			continue
		}

		p := &params[len(row)]
		p.Value = values[len(row)]

		stmt.Columns = append(stmt.Columns, f.Column)
		row = append(row, p)
	}

	if len(row) > 0 {
		stmt.Rows = [][]qb.Expr{row}
	}

	for _, f := range returning {
		stmt.Returning = append(stmt.Returning, qb.Selected{Value: qb.Column{Name: f.Column}})
	}

	return stmt
}
