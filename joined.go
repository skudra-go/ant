package ant

import (
	"database/sql"
	"errors"
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
	target.alias = "ant_joined" + strconv.Itoa(len(n.inJoin))
	target.nullable = true

	n.refs = append(n.refs, r.Owner)
	n.inJoin = append(n.inJoin, joinedInclude{r, link, target})

	return n
}

// addJoined puts the joined includes into a statement.
func (q *Query[T]) addJoined(stmt *qb.Select) {
	for _, j := range q.inJoin {
		on := append(
			[]qb.Cond{
				qb.Compare{
					Left:  j.relation.Owner.Col(j.link.Local),
					Op:    qb.OpEq,
					Right: j.source.col(j.link.Foreign),
				},
			},
			modelFilters(j.source, nil)...)

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

// collectJoined reads rows that hold the root's columns, then each joined model's.
func (q *Query[T]) collectJoined(rows *sql.Rows) (out []*T, err error) {
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
	tracked := q.tracked()
	out = []*T{}

	for rows.Next() {
		entity := new(T)
		column := 0

		for _, f := range root {
			dest[column] = f.Addr(unsafe.Pointer(entity))
			column++
		}

		values := make([]unsafe.Pointer, len(q.inJoin))

		for i, j := range q.inJoin {
			values[i] = j.source.alloc()
			seen[i] = false

			for _, f := range j.source.model.Stored() {
				dest[column] = &nullableColumn{dst: f.Addr(values[i]), seen: &seen[i]}
				column++
			}
		}

		if err := rows.Scan(dest...); err != nil {
			return nil, err
		}

		for i, j := range q.inJoin {
			if !seen[i] {
				continue
			}

			value := values[i]

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
		q.rememberJoined(out)
	}

	return out, nil
}

// rememberJoined records loaded parents on tracked entities, as IncludeOne does.
// The root instance can be an earlier one, so that one gets the relation too.
func (q *Query[T]) rememberJoined(items []*T) {
	model := q.model()

	for i, item := range items {
		loaded := unsafe.Pointer(item)
		instance := q.state.tracker.Load(model, loaded)
		items[i] = (*T)(instance)

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
