package ant

import (
	"context"
	"database/sql"
	"strconv"

	"github.com/skudra-go/ant/qb"
)

// scalar reads one value of an expression over the query's rows. NULL, as from
// SUM over no rows, gives the zero value. It reads no row a lock could hold, so
// a query's ForUpdate or ForShare is left out, here and in the other counts and
// aggregates: PostgreSQL refuses a lock on an aggregate.
func scalar[V any](ctx context.Context, b *builder, value Expr[V]) (V, error) {
	var zero V

	if value.err != nil {
		return zero, value.err
	}

	n := b.cloned()
	n.refs = append(n.refs, value.refs...)

	n.order = nil
	n.lock = nil

	if err := n.ready(); err != nil {
		return zero, err
	}

	rows, err := n.query(ctx, n.statement([]qb.Selected{{Value: value.node}}))
	if err != nil {
		return zero, err
	}

	out, err := first[sql.Null[V]](rows)
	if err != nil {
		return zero, err
	}

	return out.V, nil
}

// Count returns the number of matching rows. Limit, Offset, and Distinct are
// honored, by counting over a subquery.
func (q *Query[T]) Count(ctx context.Context) (int64, error) {
	if q.picksRows() {
		if err := q.readyToRead(); err != nil {
			return 0, err
		}
	}

	return countRows(ctx, &q.builder, q.selection, false)
}

// Exists reports whether any row matches. It stops at the first one.
func (q *Query[T]) Exists(ctx context.Context) (bool, error) {
	return existsRows(ctx, &q.builder, q.selection, false)
}

// picksRows reports whether Limit, Offset, Distinct or GroupBy decide which rows
// b returns, so that counting them needs a subquery that keeps the select list.
func (b *builder) picksRows() bool {
	return b.limit != nil || b.offset != nil || b.distinct || len(b.group) > 0
}

// countRows counts the rows b returns, whose select list columns gives: with
// COUNT(*) in its place when nothing picks the rows, and otherwise over a
// subquery that selects them. computed says the select list can decide the rows
// itself, as a projection's expressions can: an aggregate makes one row of
// many, or of none, and a set-returning function many of one.
func countRows(
	ctx context.Context,
	b *builder,
	columns func() []qb.Selected,
	computed bool,
) (int64, error) {
	if !computed && !b.picksRows() {
		return scalar(ctx, b, Count())
	}

	if err := b.ready(); err != nil {
		return 0, err
	}

	inner := b.statement(numberedColumns(columns()))
	inner.Order = nil
	inner.Lock = nil

	const alias = "ant_counted"

	rows, err := b.query(ctx, lift(
		&qb.Select{
			Columns: []qb.Selected{{Value: Count().node}},
			From:    qb.Source{Sub: inner, Alias: alias},
		},
		inner,
	))
	if err != nil {
		return 0, err
	}

	n, err := first[int64](rows)
	if err != nil {
		return 0, err
	}

	return *n, nil
}

// numberedColumns names the columns ant_1, ant_2 and so on: a derived table
// needs names that differ, and the sides of a join share some, such as id.
func numberedColumns(columns []qb.Selected) []qb.Selected {
	out := make([]qb.Selected, len(columns))

	for i, c := range columns {
		out[i] = qb.Selected{Value: c.Value, Alias: "ant_" + strconv.Itoa(i+1)}
	}

	return out
}

// existsRows reports whether b returns any row, stopping at the first, with
// columns its select list. It selects 1 in their place, except where the
// columns decide the rows: when computed says they can, as in countRows, and
// where Distinct or GroupBy decide the rows an Offset skips, since MySQL,
// MariaDB and SQLite drop either from an EXISTS subquery, and with a 1 DISTINCT
// would leave one row. There the rows are read from a derived table of the
// columns.
func existsRows(
	ctx context.Context,
	b *builder,
	columns func() []qb.Selected,
	computed bool,
) (bool, error) {
	if err := b.ready(); err != nil {
		return false, err
	}

	one := []qb.Selected{{Value: qb.Fragment{SQL: "1"}}}

	var inner *qb.Select

	if computed || (b.distinct || len(b.group) > 0) && b.offset != nil {
		picked := b.statement(numberedColumns(columns()))
		picked.Order = nil
		picked.Lock = nil

		inner = lift(
			&qb.Select{Columns: one, From: qb.Source{Sub: picked, Alias: "ant_rows"}},
			picked,
		)
	} else {
		inner = b.statement(one)
		inner.Order = nil
		inner.Lock = nil
	}

	rows, err := b.query(
		ctx,
		lift(&qb.Select{Columns: []qb.Selected{{Value: qb.Exists{Select: inner}}}}, inner),
	)
	if err != nil {
		return false, err
	}

	found, err := first[bool](rows)
	if err != nil {
		return false, err
	}

	return *found, nil
}

// lift moves the WITH of inner, a subquery of outer, to the start of outer, and
// returns outer. SQL Server takes WITH only there, and outer reads nothing but
// inner, so no name it reads can be one of inner's CTEs.
func lift(outer, inner *qb.Select) *qb.Select {
	outer.With, outer.Recursive = inner.With, inner.Recursive
	inner.With, inner.Recursive = nil, false

	return outer
}

// DoesNotExist reports whether no row matches.
func (q *Query[T]) DoesNotExist(ctx context.Context) (bool, error) {
	found, err := q.Exists(ctx)

	return !found, err
}

// aggregateOf reads value, an aggregate of one column, over the query's rows.
// Limit, Offset, and Distinct pick those rows, so with any of them it aggregates
// over a subquery, as Count does, keeping the order that decides which rows a
// limit or offset takes.
func aggregateOf[T, V any](ctx context.Context, q *Query[T], value Expr[V]) (V, error) {
	var zero V

	if q.limit == nil && q.offset == nil && !q.distinct {
		return scalar(ctx, &q.builder, value)
	}

	if value.err != nil {
		return zero, value.err
	}

	n := q.next()
	n.refs = append(n.refs, value.refs...)

	if err := n.ready(); err != nil {
		return zero, err
	}

	const (
		alias  = "ant_counted"
		column = "ant_value"
	)

	f := value.node.(qb.Func)

	inner := n.statement(append(n.selection(), qb.Selected{Value: f.Args[0], Alias: column}))
	inner.Lock = nil

	if n.limit == nil && n.offset == nil {
		inner.Order = nil
	}

	f.Args = []qb.Expr{qb.Column{Alias: alias, Name: column}}

	rows, err := n.query(ctx, lift(
		&qb.Select{
			Columns: []qb.Selected{{Value: f}},
			From:    qb.Source{Sub: inner, Alias: alias},
		},
		inner,
	))
	if err != nil {
		return zero, err
	}

	out, err := first[sql.Null[V]](rows)
	if err != nil {
		return zero, err
	}

	return out.V, nil
}

// SumOf adds up a column over matching rows, giving the zero value of V if
// there are none or all are NULL: 0 for a number, nil for a pointer. The column
// is of any type the database adds up, such as a number, a decimal, or a
// pointer or sql.Null of one. Limit, Offset, and Distinct are honored, by
// summing over a subquery.
func SumOf[T, V any](ctx context.Context, q *Query[T], field *V) (V, error) {
	return aggregateOf(ctx, q, Sum(field))
}

// AvgOf averages a column over matching rows, giving zero if there are none.
// Limit, Offset, and Distinct are honored, as with SumOf. The average of a
// time.Duration is in nanoseconds, from an interval column as from an integer
// one.
func AvgOf[T, V any](ctx context.Context, q *Query[T], field *V) (float64, error) {
	return aggregateOf(ctx, q, Avg(field))
}

// MinOf returns a column's smallest value, or the zero value of V if there are
// no rows. The column is of any type the database orders, as with OrderBy.
// Limit, Offset, and Distinct are honored, as with SumOf.
func MinOf[T, V any](ctx context.Context, q *Query[T], field *V) (V, error) {
	return aggregateOf(ctx, q, Min(field))
}

// MaxOf returns a column's largest value, or zero if there are no rows.
// Limit, Offset, and Distinct are honored, as with SumOf.
func MaxOf[T, V any](ctx context.Context, q *Query[T], field *V) (V, error) {
	return aggregateOf(ctx, q, Max(field))
}

// Pluck returns one column of every matching row.
func Pluck[T, V any](ctx context.Context, q *Query[T], field *V) ([]V, error) {
	value := Col(field)
	if value.err != nil {
		return nil, value.err
	}

	n := q.next()

	n.refs = append(n.refs, value.refs...)

	if err := n.ready(); err != nil {
		return nil, err
	}

	rows, err := n.query(ctx, n.statement([]qb.Selected{{Value: value.node}}))
	if err != nil {
		return nil, err
	}

	return collect[V](rows)
}
