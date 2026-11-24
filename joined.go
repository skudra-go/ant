package ant

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"unsafe"

	"github.com/skudra-go/ant/internal/meta"
	"github.com/skudra-go/ant/qb"
)

// joinedInclude loads one relation in the main query.
type joinedInclude struct {
	relation *meta.Relation
	link     meta.Link
	source   source
}

// IncludeJoined loads a many-to-one or one-to-one relation in the same query,
// through a left join, instead of the extra query IncludeOne runs. It suits a
// relation most rows have, read often enough that a round trip matters. Parents
// with the same related row each get their own value unless tracked.
//
// The join applies the relation's Where conditions and Morph type, and a has-one
// with Latest or Oldest joins only the row it picks, so the result is the one
// IncludeOne loads. That pick needs a target with a single-column key.
func (q *Query[T]) IncludeJoined[R any](rel **R) *Query[T] {
	n := q.next()

	r, link, err := linkOf[R](rel)
	if err != nil {
		n.fail(err)

		return n
	}

	if r.Pivot != nil {
		n.fail(ErrNotImplemented)

		return n
	}

	target := sourceOf[R]()

	if r.Pick != nil && target.model.Key() == nil {
		n.fail(fmt.Errorf(
			"%w: IncludeJoined on %s.%s picks one row by its key, and %s has no single-column key; use IncludeOne",
			ErrNotImplemented,
			r.Owner.Type.Name(),
			r.Name,
			target.model.Type.Name(),
		))

		return n
	}

	target.alias = "ant_joined" + strconv.Itoa(len(n.inJoin))
	target.nullable = true

	n.refs = append(n.refs, r.Owner)
	n.inJoin = append(n.inJoin, joinedInclude{r, link, target})

	return n
}

// addJoined puts the joined includes into a statement. A row lock stays on the
// query's own table: the includes are left joins, and PostgreSQL refuses to
// lock the side of one that can be NULL.
func (q *Query[T]) addJoined(stmt *qb.Select) {
	if stmt.Lock != nil && len(q.inJoin) > 0 {
		lock := *stmt.Lock

		switch {
		case stmt.From.Alias != "":
			lock.Of = stmt.From.Alias
		case stmt.From.Table != nil:
			lock.Of = stmt.From.Table.Name
		}

		stmt.Lock = &lock
	}

	for _, j := range q.inJoin {
		on := append(
			[]qb.Cond{
				qb.Compare{
					Left:  j.relation.Owner.Col(j.link.Local),
					Op:    qb.OpEq,
					Right: j.source.col(j.link.Foreign),
				},
			},
			joinedScope(j.relation, j.source, true)...)

		on = append(on, modelFilters(j.source, nil)...)

		if j.relation.Pick != nil {
			on = append(on, picked(j))
		}

		stmt.Joins = append(stmt.Joins, qb.Join{
			Kind:   qb.LeftJoin,
			Source: qb.Source{Table: j.source.model.Table, Alias: j.source.alias},
			On:     on,
		})

		for _, f := range j.source.model.Stored() {
			stmt.Columns = append(stmt.Columns, qb.Selected{Value: j.source.col(f)})
		}
	}
}

// joinedScope returns the relation's scope for a join of target, as Join applies
// it: conditions on the target read target's alias. A morph condition on the
// owner's side reads the owner, even when the owner is the target's table, and
// owner says whether to keep it.
func joinedScope(r *meta.Relation, target source, owner bool) []qb.Cond {
	out := make([]qb.Cond, 0, len(r.Scope))

	for _, c := range r.Scope {
		switch {
		case ownerSide(r, c):
			if owner {
				out = append(out, c)
			}
		default:
			out = append(out, qb.Aliased(c, target.model.Table, target.alias))
		}
	}

	return out
}

// picked returns the condition that keeps the one row a has-one's pick chooses
// of the joined relation's rows, as IncludeOne reads it: the joined row's key is
// the first key of the owner's rows in the pick's order, and the key breaks a tie.
func picked(j joinedInclude) qb.Cond {
	key := j.source.model.Key()
	inner := source{model: j.source.model, alias: j.source.alias + "_pick"}
	one := 1

	pick := *j.relation.Pick
	pick.Value = qb.Aliased(qb.Compare{Left: pick.Value}, inner.model.Table, inner.alias).(qb.Compare).Left

	order := []qb.Order{pick}

	// SQL Server refuses a column twice in an ORDER BY.
	if pick.Value != qb.Expr(inner.col(key)) {
		order = append(order, qb.Order{Value: inner.col(key)})
	}

	where := append(
		[]qb.Cond{
			qb.Compare{
				Left:  inner.col(j.link.Foreign),
				Op:    qb.OpEq,
				Right: j.relation.Owner.Col(j.link.Local),
			},
		},
		joinedScope(j.relation, inner, false)...)

	return qb.Compare{
		Left: j.source.col(key),
		Op:   qb.OpEq,
		Right: qb.Subquery{
			Select: &qb.Select{
				Columns: []qb.Selected{{Value: inner.col(key)}},
				From:    qb.Source{Table: inner.model.Table, Alias: inner.alias},
				Where:   append(where, modelFilters(inner, nil)...),
				Order:   order,
				Limit:   &one,
			},
		},
	}
}

// collectJoined reads rows that hold the root's columns, then each joined model's.
func (q *Query[T]) collectJoined(ctx context.Context, rows *sql.Rows) (out []*T, err error) {
	defer func() { err = errors.Join(err, rows.Close()) }()

	model := q.model()

	root := slices.Clone(q.columns())

	for _, c := range q.computed {
		root = append(root, model.Column(c.Alias))
	}

	total := len(root)

	for _, j := range q.inJoin {
		total += len(j.source.model.Stored())
	}

	dest := make([]any, total)
	seen := make([]bool, len(q.inJoin))
	drivers := make([][]*driverColumn, len(q.inJoin))
	tracked := q.tracked()
	out = []*T{}

	for i, j := range q.inJoin {
		drivers[i] = driverColumns(j.source.model.Stored())
	}

	for rows.Next() {
		entity := new(T)
		column := 0

		for _, f := range root {
			dest[column] = f.Dest(unsafe.Pointer(entity))
			column++
		}

		values := make([]unsafe.Pointer, len(q.inJoin))

		for i, j := range q.inJoin {
			values[i] = j.source.alloc()
			seen[i] = false

			for k, f := range j.source.model.Stored() {
				dest[column] = outerDest(drivers[i], k, f, values[i], &seen[i])
				column++
			}
		}

		if err := rows.Scan(dest...); err != nil {
			return nil, err
		}

		for _, f := range root {
			f.InUTC(unsafe.Pointer(entity))
		}

		for i := range q.inJoin {
			settleOuter(drivers[i], values[i], &seen[i])
		}

		for i, j := range q.inJoin {
			if !seen[i] {
				continue
			}

			value := values[i]

			for _, f := range j.source.model.Stored() {
				f.InUTC(value)
			}

			if tracked {
				value = q.state.tracker.Load(j.source.model, value)
			}

			*(*unsafe.Pointer)(j.relation.Slot(unsafe.Pointer(entity))) = value
		}

		out = append(out, entity)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	if tracked {
		if err := q.rememberJoined(ctx, out); err != nil {
			return nil, err
		}
	}

	return out, nil
}

// rememberJoined records loaded parents on tracked entities, as IncludeOne does.
// The root instance can be an earlier one, so that one gets the relation too,
// and the row, if the query locked it.
func (q *Query[T]) rememberJoined(ctx context.Context, items []*T) error {
	model := q.model()

	for i, item := range items {
		loaded := unsafe.Pointer(item)
		instance := q.state.tracker.Load(model, loaded)
		items[i] = (*T)(instance)
		q.keepComputed((*T)(instance), item)

		if err := q.refreshLocked(ctx, (*T)(instance), item); err != nil {
			return err
		}

		entry := q.state.tracker.Get(instance)
		if entry == nil {
			continue
		}

		for _, j := range q.inJoin {
			slot := (*unsafe.Pointer)(j.relation.Slot(loaded))
			if *slot == nil {
				continue
			}

			*(*unsafe.Pointer)(j.relation.Slot(instance)) = *slot

			key, _ := j.link.Target.KeyOf(*slot)
			entry.SetParent(j.relation, key)
		}
	}

	return nil
}

// resolveIdentity makes one instance of each row, keeping the first.
func resolveIdentity[T any](model *meta.Model, items []*T) []*T {
	seen := make(map[any]*T, len(items))

	for i, item := range items {
		key, ok := model.KeyOf(unsafe.Pointer(item))
		if !ok {
			continue
		}

		if first := seen[key]; first != nil {
			items[i] = first
		} else {
			seen[key] = item
		}
	}

	return items
}
