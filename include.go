package ant

import (
	"context"
	"slices"
	"unsafe"

	"github.com/skudra-go/ant/dialect"
	"github.com/skudra-go/ant/internal/meta"
	"github.com/skudra-go/ant/internal/track"
	"github.com/skudra-go/ant/qb"
)

// loadBatch caps the keys in one statement, well under PostgreSQL's limit of
// 65535 parameters.
const loadBatch = 10000

// loadSize returns the keys that fit in one statement on d, leaving room for the
// query's own conditions.
func loadSize(d dialect.Dialect) int {
	return min(loadBatch, dialect.SyntaxOf(d).ParameterLimit()*9/10)
}

// partitionColumn numbers rows per parent for a limited include.
const partitionColumn = "ant_row"

// include loads one relation for a set of parent entities.
// The tracked argument is false when the parents are not tracked, so that their
// related rows are not either, whatever order the builder calls came in.
type include struct {
	load     func(ctx context.Context, parents []unsafe.Pointer, tracked bool) error
	relation *meta.Relation
}

// Include loads a one-to-many relation in one extra query per relation, however
// many parents there are. Parents with no related rows get an empty slice, so a
// nil slice always means the relation was not loaded.
//
// The optional function shapes the related query. It can filter, order, and
// limit, per parent, and call Include again for nested relations:
//
//	db.Users.Include(&U.Posts, func(q *ant.Query[Post]) *ant.Query[Post] {
//		return q.Where(ant.Eq(&P.Published, true)).OrderByDesc(&P.ID).Limit(3)
//	})
func (q *Query[T]) Include[R any](rel *[]R, shape ...func(*Query[R]) *Query[R]) *Query[T] {
	n := q.next()

	inc, err := manyInclude(n.state, rel, shape, n.noTracking)
	if err != nil {
		n.fail(err)

		return n
	}

	n.refs = append(n.refs, inc.owner)
	n.includes = append(n.includes, inc.include)

	return n
}

// IncludeOne loads a many-to-one or one-to-one relation in one extra query. An
// unmatched relation stays nil. Parents with the same related row share one
// value. See Include for the optional function.
func (q *Query[T]) IncludeOne[R any](rel **R, shape ...func(*Query[R]) *Query[R]) *Query[T] {
	n := q.next()

	inc, err := oneInclude(n.state, rel, shape, n.noTracking)
	if err != nil {
		n.fail(err)

		return n
	}

	n.refs = append(n.refs, inc.owner)
	n.includes = append(n.includes, inc.include)

	return n
}

type ownedInclude struct {
	include
	owner *meta.Model
}

func shaped[R any](s *state, shape []func(*Query[R]) *Query[R], noTracking bool) *Query[R] {
	sub := newQuery[R](s)
	sub.noTracking = noTracking

	for _, f := range shape {
		sub = f(sub)
	}

	return sub
}

func manyInclude[R any](
	s *state,
	rel *[]R,
	shape []func(*Query[R]) *Query[R],
	noTracking bool,
) (ownedInclude, error) {
	r, link, err := linkOf[R](rel)
	if err != nil {
		return ownedInclude{}, err
	}

	sub := scoped(shaped(s, shape, noTracking), r)
	load := func(ctx context.Context, parents []unsafe.Pointer, tracked bool) error {
		return loadMany(ctx, r, link, untrackedUnless(sub, tracked), parents)
	}

	return ownedInclude{include{load, r}, r.Owner}, nil
}

func oneInclude[R any](
	s *state,
	rel **R,
	shape []func(*Query[R]) *Query[R],
	noTracking bool,
) (ownedInclude, error) {
	r, link, err := linkOf[R](rel)
	if err != nil {
		return ownedInclude{}, err
	}

	sub := scoped(shaped(s, shape, noTracking), r)

	if r.Pick != nil {
		sub = sub.next()
		sub.order = []qb.Order{*r.Pick}
		sub = sub.Limit(1)
	}

	load := func(ctx context.Context, parents []unsafe.Pointer, tracked bool) error {
		return loadOne(ctx, r, link, untrackedUnless(sub, tracked), parents)
	}

	return ownedInclude{include{load, r}, r.Owner}, nil
}

// scoped applies a relation's scope to a query of its target. A morph condition
// on the owner's side does not belong there, and is applied to the parents instead.
func scoped[R any](sub *Query[R], r *meta.Relation) *Query[R] {
	if len(r.Scope) == 0 {
		return sub
	}

	n := sub.next()

	for _, c := range r.Scope {
		if !ownerSide(r, c) {
			n.where = append(n.where, c)
		}
	}

	return n
}

// ownerSide reports whether a scope condition is on the relation's owner.
func ownerSide(r *meta.Relation, c qb.Cond) bool {
	compare, ok := c.(qb.Compare)
	if !ok {
		return false
	}

	column, ok := compare.Left.(qb.Column)

	return ok && r.MorphField != nil && r.Owner.Has(r.MorphField) &&
		column.Table == r.Owner.Table &&
		column.Name == r.MorphField.Column
}

// morphParents keeps the parents whose polymorphic type is this relation's.
func morphParents(r *meta.Relation, parents []unsafe.Pointer) []unsafe.Pointer {
	f := r.MorphField
	if f == nil || !r.Owner.Has(f) {
		return parents
	}

	return slices.DeleteFunc(slices.Clone(parents), func(p unsafe.Pointer) bool {
		return f.Value(p).Interface() != r.MorphValue
	})
}

func untrackedUnless[R any](sub *Query[R], tracked bool) *Query[R] {
	if tracked {
		return sub
	}

	return sub.NoTracking()
}

// groupByKey returns the parents for each key, and the keys in first-seen
// order. Parents with a NULL key are left out.
func groupByKey(field *meta.Field, parents []unsafe.Pointer) (map[any][]unsafe.Pointer, []any) {
	groups := make(map[any][]unsafe.Pointer, len(parents))

	var keys []any

	for _, p := range parents {
		key, ok := field.Key(p)
		if !ok {
			continue
		}

		if _, seen := groups[key]; !seen {
			keys = append(keys, key)
		}

		groups[key] = append(groups[key], p)
	}

	return groups, keys
}

// fetchRelated returns the rows of sub whose column field is one of keys.
func fetchRelated[R any](
	ctx context.Context,
	sub *Query[R],
	field *meta.Field,
	keys []any,
) ([]*R, error) {
	if err := sub.readyToRead(); err != nil {
		return nil, err
	}

	var out []*R

	for batch := range slices.Chunk(keys, loadSize(sub.state.dialect)) {
		values := make([]qb.Expr, len(batch))

		for i, k := range batch {
			values[i] = qb.Param{Value: k}
		}

		q := sub.next()

		q.where = append(q.where, qb.In{Left: q.sources[0].col(field), Values: values})

		if len(q.selected) > 0 && !slices.Contains(q.selected, field) {
			q.selected = append(q.selected, field)
		}

		q.omitted = slices.DeleteFunc(q.omitted, func(f *meta.Field) bool { return f == field })

		found, err := q.fetch(ctx, q.relatedStatement(field))
		if err != nil {
			return nil, err
		}

		out = append(out, found...)
	}

	return out, nil
}

// relatedStatement applies Limit and Offset per parent, using row numbers.
func (q *Query[T]) relatedStatement(field *meta.Field) *qb.Select {
	if q.limit == nil && q.offset == nil {
		return q.statement(q.selection())
	}

	rowNumber := qb.Func{
		Name: "ROW_NUMBER",
		Over: &qb.Window{
			PartitionBy: []qb.Expr{q.sources[0].col(field)},
			OrderBy:     q.order,
		},
	}
	inner := q.statement(
		append(q.selection(), qb.Selected{Value: rowNumber, Alias: partitionColumn}),
	)
	inner.Order, inner.Limit, inner.Offset = nil, nil, nil

	const alias = "ant_partition"

	number := qb.Column{Alias: alias, Name: partitionColumn}
	skipped := 0

	if q.offset != nil {
		skipped = *q.offset
	}

	outer := &qb.Select{
		Columns: []qb.Selected{{Value: qb.Star{}}},
		From:    qb.Source{Sub: inner, Alias: alias},
		Where:   []qb.Cond{qb.Compare{Left: number, Op: qb.OpGt, Right: qb.Param{Value: skipped}}},
		Order:   []qb.Order{{Value: number}},
	}

	if q.limit != nil {
		outer.Where = append(
			outer.Where,
			qb.Compare{Left: number, Op: qb.OpLte, Right: qb.Param{Value: skipped + *q.limit}},
		)
	}

	return outer
}

func slot[V any](parent unsafe.Pointer, r *meta.Relation) *V {
	return (*V)(unsafe.Add(parent, r.Offset))
}

// loadPivot maps each target key to its parents, through the join table.
func loadPivot[R any](
	ctx context.Context,
	sub *Query[R],
	r *meta.Relation,
	groups map[any][]unsafe.Pointer,
	keys []any,
) (map[any][]unsafe.Pointer, []any, error) {
	if sub.state == nil {
		return nil, nil, ErrNotOpen
	}

	p := r.Pivot
	targets := map[any][]unsafe.Pointer{}

	var foreign []any

	for batch := range slices.Chunk(keys, loadSize(sub.state.dialect)) {
		values := make([]qb.Expr, len(batch))

		for i, k := range batch {
			values[i] = qb.Param{Value: k}
		}

		rows, err := sub.state.query(ctx, &qb.Select{
			Columns: []qb.Selected{
				{Value: qb.Column{Name: p.Local}},
				{Value: qb.Column{Name: p.Foreign}},
			},
			From:  qb.Source{Table: p.Table},
			Where: []qb.Cond{qb.In{Left: qb.Column{Name: p.Local}, Values: values}},
		})
		if err != nil {
			return nil, nil, err
		}

		pairs, err := collectPairs(rows)
		if err != nil {
			return nil, nil, err
		}

		for _, pair := range pairs {
			local, _ := meta.KeyValue(pair[0])

			key, _ := meta.KeyValue(pair[1])
			if _, seen := targets[key]; !seen {
				foreign = append(foreign, key)
			}

			targets[key] = append(targets[key], groups[local]...)
		}
	}

	return targets, foreign, nil
}

// distinct drops repeated parents, as from Load given one entity twice, so that
// each gets its related rows once.
func distinct(parents []unsafe.Pointer) []unsafe.Pointer {
	seen := make(map[unsafe.Pointer]bool, len(parents))

	return slices.DeleteFunc(slices.Clone(parents), func(p unsafe.Pointer) bool {
		if seen[p] {
			return true
		}

		seen[p] = true

		return false
	})
}

// loadMany fills a one-to-many relation. Each parent's slice is built apart and
// then set in one write, so a parent never holds half of its rows, or another
// load's rows as well.
func loadMany[R any](
	ctx context.Context,
	r *meta.Relation,
	link meta.Link,
	sub *Query[R],
	parents []unsafe.Pointer,
) error {
	parents = distinct(parents)
	groups, keys := groupByKey(link.Local, parents)

	if r.Pivot != nil {
		var err error
		if groups, keys, err = loadPivot(ctx, sub, r, groups, keys); err != nil {
			return err
		}
	}

	q := sub.next()
	q.inSlice = true

	children, err := fetchRelated(ctx, q, link.Foreign, keys)
	if err != nil {
		return err
	}

	loaded := make(map[unsafe.Pointer][]R, len(parents))

	for _, child := range children {
		key, ok := link.Foreign.Key(unsafe.Pointer(child))
		if !ok {
			continue
		}

		for _, p := range groups[key] {
			loaded[p] = append(loaded[p], *child)
		}
	}

	for _, p := range parents {
		elements := loaded[p]
		if elements == nil {
			elements = []R{}
		}

		if r.Inverse != nil {
			for i := range elements {
				*(*unsafe.Pointer)(r.Inverse.Slot(unsafe.Pointer(&elements[i]))) = p
			}
		}

		*slot[[]R](p, r) = elements
	}

	if sub.state != nil && sub.tracked() {
		trackMany[R](sub.state, r, link, parents, sub.includes)
	}

	return nil
}

// trackMany tracks each loaded element where it finally lives, and records the
// entries each parent loaded, to notice changes to the slice. The elements were
// loaded into values of their own, which are not tracked, so what their nested
// includes loaded is recorded on each element's entry here. A parent that held
// other elements before lets go of their entries, as its slice no longer holds
// them, so that an edit to one is not saved behind what it shows, unless one
// was marked by hand.
func trackMany[R any](
	s *state,
	r *meta.Relation,
	link meta.Link,
	parents []unsafe.Pointer,
	nested []include,
) {
	for _, p := range parents {
		elements := *slot[[]R](p, r)

		members := make(map[any]*track.Entry, len(elements))

		for i := range elements {
			element := unsafe.Pointer(&elements[i])

			entry := s.tracker.Copy(link.Target, element)
			if entry == nil {
				continue
			}

			if key, ok := link.Target.KeyOf(element); ok {
				members[key] = entry
			}

			for _, inc := range nested {
				recordLoaded(s, entry, inc.relation)
			}
		}

		if r.Pivot != nil && r.Pivot.ReadOnly {
			continue // a through relation is read, never written
		}

		e := s.tracker.Get(p)
		if e == nil {
			continue
		}

		kept := make(map[*track.Entry]bool, len(members))

		for _, entry := range members {
			kept[entry] = true
		}

		// An element marked by hand, as by Remove, keeps its mark.
		for _, old := range e.Members[r] {
			if old != nil && !kept[old] && old.State == track.Unchanged {
				s.tracker.Forget(old)
			}
		}

		e.SetMembers(r, members)
	}
}

// recordLoaded records on entry what its relation r holds, as a load of r into a
// tracked entity records it.
func recordLoaded(s *state, entry *track.Entry, r *meta.Relation) {
	link, err := linkFor(r)
	if err != nil || r.Pivot != nil && r.Pivot.ReadOnly {
		return
	}

	if r.Kind == meta.One {
		for _, related := range r.Related(entry.Entity) {
			if key, ok := link.Target.KeyOf(related); ok {
				entry.SetParent(r, key)
			}
		}

		return
	}

	members := map[any]*track.Entry{}

	for _, related := range r.Related(entry.Entity) {
		if key, ok := link.Target.KeyOf(related); ok {
			members[key] = s.tracker.Get(related)
		}
	}

	entry.SetMembers(r, members)
}

func loadOne[R any](
	ctx context.Context,
	r *meta.Relation,
	link meta.Link,
	sub *Query[R],
	parents []unsafe.Pointer,
) error {
	groups, keys := groupByKey(link.Local, morphParents(r, parents))

	if r.Pivot != nil {
		var err error
		if groups, keys, err = loadPivot(ctx, sub, r, groups, keys); err != nil {
			return err
		}
	}

	children, err := fetchRelated(ctx, sub, link.Foreign, keys)
	if err != nil {
		return err
	}

	loaded := make(map[unsafe.Pointer]*R, len(parents))

	for _, child := range children {
		key, ok := link.Foreign.Key(unsafe.Pointer(child))
		if !ok {
			continue
		}

		for _, p := range groups[key] {
			if _, seen := loaded[p]; !seen {
				loaded[p] = child
			}
		}
	}

	for _, p := range parents {
		*slot[*R](p, r) = loaded[p]
	}

	if sub.state != nil && sub.tracked() {
		for _, p := range parents {
			e := sub.state.tracker.Get(p)
			if e == nil || *slot[*R](p, r) == nil {
				continue
			}

			key, _ := link.Target.KeyOf(unsafe.Pointer(*slot[*R](p, r)))
			e.SetParent(r, key)
		}
	}

	return nil
}

// Pointers returns pointers to the elements of values, for passing a slice of
// values to Load.
func Pointers[T any](values []T) []*T {
	out := make([]*T, len(values))

	for i := range values {
		out[i] = &values[i]
	}

	return out
}

func parentPointers[T any](entities []*T) ([]unsafe.Pointer, error) {
	out := make([]unsafe.Pointer, 0, len(entities))

	for _, e := range entities {
		if e == nil {
			return nil, ErrNilEntity
		}

		out = append(out, unsafe.Pointer(e))
	}

	return out, nil
}

func (t *Table[T]) run(ctx context.Context, inc include, entities []*T) error {
	if _, err := t.model(); err != nil {
		return err
	}

	parents, err := parentPointers(entities)
	if err != nil || len(parents) == 0 {
		return err
	}

	ctx, release, err := t.state.claim(ctx, t.state.touchesTracker())
	if err != nil {
		return err
	}

	defer release()

	return inc.load(ctx, parents, t.state.settings.tracking)
}

// Load fills a one-to-many relation on entities already in memory, in one extra
// query. See Include for the optional shape, passed through LoadShaped.
func (t *Table[T]) Load[R any](ctx context.Context, rel *[]R, entities ...*T) error {
	inc, err := manyInclude[R](t.state, rel, nil, t.state == nil || !t.state.settings.tracking)
	if err != nil {
		return err
	}

	return t.run(ctx, inc.include, entities)
}

// LoadOne fills a many-to-one or one-to-one relation on entities already in
// memory, in one extra query.
func (t *Table[T]) LoadOne[R any](ctx context.Context, rel **R, entities ...*T) error {
	inc, err := oneInclude[R](t.state, rel, nil, t.state == nil || !t.state.settings.tracking)
	if err != nil {
		return err
	}

	return t.run(ctx, inc.include, entities)
}

// LoadMissing is Load for only the entities whose relation is still nil.
func (t *Table[T]) LoadMissing[R any](ctx context.Context, rel *[]R, entities ...*T) error {
	r, err := relation(rel)
	if err != nil {
		return err
	}

	if _, err := t.model(); err != nil {
		return err
	}

	// The relations are read under the claim, so another operation cannot be
	// loading them meanwhile.
	ctx, release, err := t.state.claim(ctx, t.state.touchesTracker())
	if err != nil {
		return err
	}

	defer release()

	var missing []*T

	for _, e := range entities {
		if e == nil {
			return ErrNilEntity
		}

		if *slot[[]R](unsafe.Pointer(e), r) == nil {
			missing = append(missing, e)
		}
	}

	return t.Load(ctx, rel, missing...)
}

// Related returns a query for the rows related to entity through a one-to-many
// relation.
func Related[T, R any](h Handle, entity *T, rel *[]R) *Query[R] {
	return relatedTo[T, R](h, entity, rel)
}

// RelatedOne returns a query for the row related to entity through a
// many-to-one or one-to-one relation.
func RelatedOne[T, R any](h Handle, entity *T, rel **R) *Query[R] {
	return relatedTo[T, R](h, entity, rel)
}

func relatedTo[T, R, V any](h Handle, entity *T, rel *V) *Query[R] {
	var s *state

	if h != nil {
		s = h.context().state
	}

	q := newQuery[R](s)

	r, link, err := linkOf[R](rel)

	switch {
	case err != nil:
		q.fail(err)

		return q
	case entity == nil:
		q.fail(ErrNilEntity)

		return q
	case r.Owner != configure[T]():
		q.fail(ErrWrongModel)

		return q
	}

	key, ok := link.Local.Key(unsafe.Pointer(entity))
	if !ok {
		q.where = append(q.where, qb.Or{})

		return q
	}

	q.where = append(
		q.where,
		qb.Compare{Left: q.sources[0].col(link.Foreign), Op: qb.OpEq, Right: qb.Param{Value: key}},
	)

	q.filtered = true

	return scoped(q, r)
}
