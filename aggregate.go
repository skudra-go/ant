package ant

import (
	"context"
	"database/sql"

	"github.com/skudra-go/ant/qb"
)

// scalar reads one value of an expression over the query's rows. NULL, as from
// SUM over no rows, gives the zero value.
func scalar[V any](ctx context.Context, b *builder, value Expr[V]) (V, error) {
	var zero V

	if value.err != nil {
		return zero, value.err
	}

	n := b.cloned()
	n.refs = append(n.refs, value.refs...)

	n.order = nil

	if err := n.ready(); err != nil {
		return zero, err
	}

	rows, err := n.state.query(ctx, n.statement([]qb.Selected{{Value: value.node}}))
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
	if q.limit == nil && q.offset == nil && !q.distinct {
		return scalar(ctx, &q.builder, Count())
	}

	if err := q.readyToRead(); err != nil {
		return 0, err
	}

	inner := q.statement(q.selection())
	inner.Order = nil

	const alias = "ant_counted"

	rows, err := q.state.query(ctx, lift(
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

// Exists reports whether any row matches. It stops at the first one.
func (q *Query[T]) Exists(ctx context.Context) (bool, error) {
	if err := q.ready(); err != nil {
		return false, err
	}

	inner := q.statement([]qb.Selected{{Value: qb.Fragment{SQL: "1"}}})
	inner.Order = nil

	rows, err := q.state.query(
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

	if n.limit == nil && n.offset == nil {
		inner.Order = nil
	}

	f.Args = []qb.Expr{qb.Column{Alias: alias, Name: column}}

	rows, err := n.state.query(ctx, lift(
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

// SumOf adds up a column over matching rows, giving zero if there are none.
// Limit, Offset, and Distinct are honored, by summing over a subquery.
func SumOf[T any, V Number](ctx context.Context, q *Query[T], field *V) (V, error) {
	return aggregateOf(ctx, q, Sum(field))
}

// AvgOf averages a column over matching rows, giving zero if there are none.
// Limit, Offset, and Distinct are honored, as with SumOf.
func AvgOf[T any, V Number](ctx context.Context, q *Query[T], field *V) (float64, error) {
	return aggregateOf(ctx, q, Avg(field))
}

// MinOf returns a column's smallest value, or zero if there are no rows.
// Limit, Offset, and Distinct are honored, as with SumOf.
func MinOf[T any, V Ordered](ctx context.Context, q *Query[T], field *V) (V, error) {
	return aggregateOf(ctx, q, Min(field))
}

// MaxOf returns a column's largest value, or zero if there are no rows.
// Limit, Offset, and Distinct are honored, as with SumOf.
func MaxOf[T any, V Ordered](ctx context.Context, q *Query[T], field *V) (V, error) {
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

	rows, err := n.state.query(ctx, n.statement([]qb.Selected{{Value: value.node}}))
	if err != nil {
		return nil, err
	}

	return collect[V](rows)
}
