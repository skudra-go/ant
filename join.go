package ant

import (
	"unsafe"

	"github.com/skudra-go/ant/qb"
)

// joinChained adds J through a relation of a model already in the query.
func joinChained[J, V any](b *builder, kind qb.JoinKind, rel *V) {
	r, link, err := linkOf[J](rel)
	if err != nil {
		b.fail(err)
		b.addSource(sourceOf[J](), kind)

		return
	}

	joinRelation(b, kind, sourceOf[J](), r, link)
}

// Join2 is a query across two joined models.
type Join2[A, B any] struct {
	builder
}

func (j *Join2[A, B]) next() *Join2[A, B] { return &Join2[A, B]{builder: j.builder.cloned()} }

// Where adds conditions on any joined model.
func (j *Join2[A, B]) Where(c ...Cond) *Join2[A, B] {
	n := j.next()
	n.addWhere(c)

	return n
}

// OrderBy orders results by a field of any joined model, ascending, replacing
// any earlier ordering.
func (j *Join2[A, B]) OrderBy[V Ordered](field *V) *Join2[A, B] { return j.Order(Col(field).Asc()) }

// OrderByDesc orders results by a field of any joined model, descending,
// replacing any earlier ordering.
func (j *Join2[A, B]) OrderByDesc[V Ordered](field *V) *Join2[A, B] {
	return j.Order(Col(field).Desc())
}

// Order orders results by terms from Asc and Desc, replacing any earlier
// ordering.
func (j *Join2[A, B]) Order(terms ...Sort) *Join2[A, B] {
	n := j.next()
	n.order = nil
	n.addOrder(terms)

	return n
}

// Limit caps the number of rows returned.
func (j *Join2[A, B]) Limit(limit int) *Join2[A, B] {
	n := j.next()
	n.limit = n.paging("Limit", limit)

	return n
}

// Offset skips the first rows.
func (j *Join2[A, B]) Offset(offset int) *Join2[A, B] {
	n := j.next()
	n.offset = n.paging("Offset", offset)

	return n
}

// Distinct removes duplicate rows.
func (j *Join2[A, B]) Distinct() *Join2[A, B] {
	n := j.next()
	n.distinct = true

	return n
}

// Select projects each joined row through f. The join, filters, and ordering
// run in the database, and f runs in Go once per row. Every stored column of
// each model is fetched, because Go cannot inspect what f reads. Use Project to
// fetch only some columns. A side a left, right, or full join did not match is
// nil. Results are not tracked.
func (j *Join2[A, B]) Select[R any](f func(*A, *B) R) *Projection[R] {
	return projectRows(j.builder, func(row []unsafe.Pointer) R {
		return f((*A)(row[0]), (*B)(row[1]))
	})
}

func (j *Join2[A, B]) base() *builder { return &j.builder }

// GroupBy groups the joined rows by a column. See Query.GroupBy.
func (j *Join2[A, B]) GroupBy[V any](field *V) *Grouped {
	return groupedBy(j.builder, Col(field))
}

// GroupByExpr groups the joined rows by an expression.
func (j *Join2[A, B]) GroupByExpr[V any](e Expr[V]) *Grouped { return groupedBy(j.builder, e) }

// Join adds a many-to-one or one-to-one relation of any model in the query.
func (j *Join2[A, B]) Join[C any](rel **C) *Join3[A, B, C] {
	n := j.next()
	joinChained[C](&n.builder, qb.InnerJoin, rel)

	return &Join3[A, B, C]{builder: n.builder}
}

// JoinMany adds a one-to-many relation of any model in the query.
func (j *Join2[A, B]) JoinMany[C any](rel *[]C) *Join3[A, B, C] {
	n := j.next()
	joinChained[C](&n.builder, qb.InnerJoin, rel)

	return &Join3[A, B, C]{builder: n.builder}
}

// LeftJoin adds a many-to-one or one-to-one relation, keeping unmatched rows.
func (j *Join2[A, B]) LeftJoin[C any](rel **C) *Join3[A, B, C] {
	n := j.next()
	joinChained[C](&n.builder, qb.LeftJoin, rel)

	return &Join3[A, B, C]{builder: n.builder}
}

// LeftJoinMany adds a one-to-many relation, keeping rows with no related row.
func (j *Join2[A, B]) LeftJoinMany[C any](rel *[]C) *Join3[A, B, C] {
	n := j.next()
	joinChained[C](&n.builder, qb.LeftJoin, rel)

	return &Join3[A, B, C]{builder: n.builder}
}

// JoinOn adds a model on conditions.
func (j *Join2[A, B]) JoinOn[C any](on ...Cond) *Join3[A, B, C] {
	n := j.next()
	joinOn(&n.builder, qb.InnerJoin, sourceOf[C](), on)

	return &Join3[A, B, C]{builder: n.builder}
}

// LeftJoinOn adds a model on conditions, keeping unmatched rows.
func (j *Join2[A, B]) LeftJoinOn[C any](on ...Cond) *Join3[A, B, C] {
	n := j.next()
	joinOn(&n.builder, qb.LeftJoin, sourceOf[C](), on)

	return &Join3[A, B, C]{builder: n.builder}
}

// Join3 is a query across three joined models.
type Join3[A, B, C any] struct {
	builder
}

func (j *Join3[A, B, C]) next() *Join3[A, B, C] {
	return &Join3[A, B, C]{builder: j.builder.cloned()}
}

// Where adds conditions on any joined model.
func (j *Join3[A, B, C]) Where(c ...Cond) *Join3[A, B, C] {
	n := j.next()
	n.addWhere(c)

	return n
}

// OrderBy orders results by a field of any joined model, ascending, replacing
// any earlier ordering.
func (j *Join3[A, B, C]) OrderBy[V Ordered](field *V) *Join3[A, B, C] {
	return j.Order(Col(field).Asc())
}

// OrderByDesc orders results by a field of any joined model, descending,
// replacing any earlier ordering.
func (j *Join3[A, B, C]) OrderByDesc[V Ordered](field *V) *Join3[A, B, C] {
	return j.Order(Col(field).Desc())
}

// Order orders results by terms from Asc and Desc, replacing any earlier
// ordering.
func (j *Join3[A, B, C]) Order(terms ...Sort) *Join3[A, B, C] {
	n := j.next()
	n.order = nil
	n.addOrder(terms)

	return n
}

// Limit caps the number of rows returned.
func (j *Join3[A, B, C]) Limit(limit int) *Join3[A, B, C] {
	n := j.next()
	n.limit = n.paging("Limit", limit)

	return n
}

// Offset skips the first rows.
func (j *Join3[A, B, C]) Offset(offset int) *Join3[A, B, C] {
	n := j.next()
	n.offset = n.paging("Offset", offset)

	return n
}

// Distinct removes duplicate rows.
func (j *Join3[A, B, C]) Distinct() *Join3[A, B, C] {
	n := j.next()
	n.distinct = true

	return n
}

// Select projects each joined row through f. The join, filters, and ordering
// run in the database, and f runs in Go once per row. Every stored column of
// each model is fetched, because Go cannot inspect what f reads. Use Project to
// fetch only some columns. A side a left, right, or full join did not match is
// nil. Results are not tracked.
func (j *Join3[A, B, C]) Select[R any](f func(*A, *B, *C) R) *Projection[R] {
	return projectRows(j.builder, func(row []unsafe.Pointer) R {
		return f((*A)(row[0]), (*B)(row[1]), (*C)(row[2]))
	})
}

func (j *Join3[A, B, C]) base() *builder { return &j.builder }

// GroupBy groups the joined rows by a column. See Query.GroupBy.
func (j *Join3[A, B, C]) GroupBy[V any](field *V) *Grouped {
	return groupedBy(j.builder, Col(field))
}

// GroupByExpr groups the joined rows by an expression.
func (j *Join3[A, B, C]) GroupByExpr[V any](e Expr[V]) *Grouped { return groupedBy(j.builder, e) }

// Join adds a many-to-one or one-to-one relation of any model in the query.
func (j *Join3[A, B, C]) Join[D any](rel **D) *Join4[A, B, C, D] {
	n := j.next()
	joinChained[D](&n.builder, qb.InnerJoin, rel)

	return &Join4[A, B, C, D]{builder: n.builder}
}

// JoinMany adds a one-to-many relation of any model in the query.
func (j *Join3[A, B, C]) JoinMany[D any](rel *[]D) *Join4[A, B, C, D] {
	n := j.next()
	joinChained[D](&n.builder, qb.InnerJoin, rel)

	return &Join4[A, B, C, D]{builder: n.builder}
}

// LeftJoin adds a many-to-one or one-to-one relation, keeping unmatched rows.
func (j *Join3[A, B, C]) LeftJoin[D any](rel **D) *Join4[A, B, C, D] {
	n := j.next()
	joinChained[D](&n.builder, qb.LeftJoin, rel)

	return &Join4[A, B, C, D]{builder: n.builder}
}

// LeftJoinMany adds a one-to-many relation, keeping rows with no related row.
func (j *Join3[A, B, C]) LeftJoinMany[D any](rel *[]D) *Join4[A, B, C, D] {
	n := j.next()
	joinChained[D](&n.builder, qb.LeftJoin, rel)

	return &Join4[A, B, C, D]{builder: n.builder}
}

// JoinOn adds a model on conditions.
func (j *Join3[A, B, C]) JoinOn[D any](on ...Cond) *Join4[A, B, C, D] {
	n := j.next()
	joinOn(&n.builder, qb.InnerJoin, sourceOf[D](), on)

	return &Join4[A, B, C, D]{builder: n.builder}
}

// LeftJoinOn adds a model on conditions, keeping unmatched rows.
func (j *Join3[A, B, C]) LeftJoinOn[D any](on ...Cond) *Join4[A, B, C, D] {
	n := j.next()
	joinOn(&n.builder, qb.LeftJoin, sourceOf[D](), on)

	return &Join4[A, B, C, D]{builder: n.builder}
}

// Join4 is a query across four joined models.
type Join4[A, B, C, D any] struct {
	builder
}

func (j *Join4[A, B, C, D]) next() *Join4[A, B, C, D] {
	return &Join4[A, B, C, D]{builder: j.builder.cloned()}
}

// Where adds conditions on any joined model.
func (j *Join4[A, B, C, D]) Where(c ...Cond) *Join4[A, B, C, D] {
	n := j.next()
	n.addWhere(c)

	return n
}

// OrderBy orders results by a field of any joined model, ascending, replacing
// any earlier ordering.
func (j *Join4[A, B, C, D]) OrderBy[V Ordered](field *V) *Join4[A, B, C, D] {
	return j.Order(Col(field).Asc())
}

// OrderByDesc orders results by a field of any joined model, descending,
// replacing any earlier ordering.
func (j *Join4[A, B, C, D]) OrderByDesc[V Ordered](field *V) *Join4[A, B, C, D] {
	return j.Order(Col(field).Desc())
}

// Order orders results by terms from Asc and Desc, replacing any earlier
// ordering.
func (j *Join4[A, B, C, D]) Order(terms ...Sort) *Join4[A, B, C, D] {
	n := j.next()
	n.order = nil
	n.addOrder(terms)

	return n
}

// Limit caps the number of rows returned.
func (j *Join4[A, B, C, D]) Limit(limit int) *Join4[A, B, C, D] {
	n := j.next()
	n.limit = n.paging("Limit", limit)

	return n
}

// Offset skips the first rows.
func (j *Join4[A, B, C, D]) Offset(offset int) *Join4[A, B, C, D] {
	n := j.next()
	n.offset = n.paging("Offset", offset)

	return n
}

// Distinct removes duplicate rows.
func (j *Join4[A, B, C, D]) Distinct() *Join4[A, B, C, D] {
	n := j.next()
	n.distinct = true

	return n
}

// Select projects each joined row through f. The join, filters, and ordering
// run in the database, and f runs in Go once per row. Every stored column of
// each model is fetched, because Go cannot inspect what f reads. Use Project to
// fetch only some columns. A side a left, right, or full join did not match is
// nil. Results are not tracked.
func (j *Join4[A, B, C, D]) Select[R any](f func(*A, *B, *C, *D) R) *Projection[R] {
	return projectRows(j.builder, func(row []unsafe.Pointer) R {
		return f((*A)(row[0]), (*B)(row[1]), (*C)(row[2]), (*D)(row[3]))
	})
}

func (j *Join4[A, B, C, D]) base() *builder { return &j.builder }

// GroupBy groups the joined rows by a column. See Query.GroupBy.
func (j *Join4[A, B, C, D]) GroupBy[V any](field *V) *Grouped {
	return groupedBy(j.builder, Col(field))
}

// GroupByExpr groups the joined rows by an expression.
func (j *Join4[A, B, C, D]) GroupByExpr[V any](
	e Expr[V],
) *Grouped {
	return groupedBy(j.builder, e)
}
