package ant

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"unsafe"

	"github.com/skudra-go/ant/internal/meta"
	"github.com/skudra-go/ant/qb"
)

// source is one table in a query. alloc makes a zero value of its model.
type source struct {
	model    *meta.Model
	alias    string
	nullable bool
	alloc    func() unsafe.Pointer
}

func sourceOf[T any]() source {
	return source{
		model: configure[T](),
		alloc: func() unsafe.Pointer { return unsafe.Pointer(new(T)) },
	}
}

func (s source) col(f *meta.Field) qb.Column {
	column := s.model.Col(f)
	column.Alias = s.alias

	return column
}

// builder holds the state common to every query shape. Builder methods record
// errors here, and terminal calls return them.
type builder struct {
	state        *state
	sources      []source
	joins        []qb.Join
	joined       []source      // the source of each join, for its global filters
	refs         []*meta.Model // models that conditions and orderings refer to
	where        []qb.Cond
	order        []qb.Order
	limit        *int
	offset       *int
	distinct     bool
	group        []qb.Expr
	having       []qb.Cond
	lock         *qb.Lock
	comment      string
	hint         string
	toMany       int // joins through a one-to-many relation
	with         []qb.CTE
	recursive    bool
	from         *qb.Source // replaces the root table, keeping its name
	ignored      []string   // global filters switched off
	ignoreAll    bool
	filtered     bool // has a condition that can exclude rows
	unfiltered   bool // Unfiltered was called
	err          error
	fromKey      bool // selects the model's stored key, which finds the rows to write
	fromFiltered bool // has a condition that can exclude rows
}

func (b *builder) fail(err error) { b.err = errors.Join(b.err, err) }

// paging returns n for a Limit or an Offset, and fails the query when it is
// negative. The databases disagree on a negative limit: most refuse it, and
// SQLite reads it as no limit at all.
func (b *builder) paging(name string, n int) *int {
	if n < 0 {
		b.fail(fmt.Errorf("ant: %s cannot be negative, got %d", name, n))
	}

	return &n
}

func (b *builder) narrowed() bool { return b.filtered || b.fromFiltered }

// cloned returns a copy of b that the copy's builder method can add to. A list
// that is only ever appended to is shared with b, clipped so that the first
// append copies it, which spares a query a copy of every list at every step of
// its chain. The others have elements that are changed in place, so they are
// copied. Whoever changes an element of a shared list copies the list first, as
// addSource does.
func (b builder) cloned() builder {
	b.sources = slices.Clip(b.sources)
	b.joins = slices.Clone(b.joins)
	b.joined = slices.Clone(b.joined)
	b.refs = slices.Clip(b.refs)
	b.where = slices.Clip(b.where)
	b.order = slices.Clone(b.order)
	b.group = slices.Clip(b.group)
	b.having = slices.Clip(b.having)
	b.with = slices.Clip(b.with)
	b.ignored = slices.Clip(b.ignored)

	return b
}

func (b *builder) addWhere(conds []Cond) {
	for _, c := range conds {
		if c.err != nil {
			b.fail(c.err)

			continue
		}

		b.refs = append(b.refs, c.refs...)
		b.where = append(b.where, c.node)
		b.filtered = b.filtered || !qb.MatchesAll(c.node)
	}
}

func (b *builder) addOrder(terms []Sort) {
	for _, s := range terms {
		if s.err != nil {
			b.fail(s.err)

			continue
		}

		b.refs = append(b.refs, s.refs...)
		b.order = append(b.order, s.order)
	}
}

func (b *builder) sourceFor(model *meta.Model) (source, bool) {
	i := slices.IndexFunc(b.sources, func(s source) bool { return s.model == model })
	if i < 0 {
		return source{}, false
	}

	return b.sources[i], true
}

// addSource adds a joined table, aliased if its table is already in the query.
func (b *builder) addSource(s source, kind qb.JoinKind) source {
	if _, used := b.sourceFor(s.model); used {
		s.alias = "t" + string(rune('0'+len(b.sources)))
	}

	if kind == qb.LeftJoin || kind == qb.FullJoin {
		s.nullable = true
	}

	if kind == qb.RightJoin || kind == qb.FullJoin {
		// The sources may be shared with the query this one was made from.
		b.sources = slices.Clone(b.sources)

		for i := range b.sources {
			b.sources[i].nullable = true
		}
	}

	b.sources = append(b.sources, s)

	return s
}

// joinRelation joins target through the relation r.
func joinRelation(b *builder, kind qb.JoinKind, target source, r *meta.Relation, link meta.Link) {
	owner, ok := b.sourceFor(r.Owner)
	if !ok {
		b.fail(ErrWrongModel)

		return
	}

	if r.Kind == meta.Many {
		b.toMany++
	}

	s := b.addSource(target, kind)
	joined := qb.Source{Table: s.model.Table, Alias: s.alias}

	scope := make([]qb.Cond, 0, len(r.Scope))

	for _, c := range r.Scope {
		if s.alias != "" {
			c = qb.Aliased(c, s.model.Table, s.alias)
		}

		scope = append(scope, c)
	}

	if p := r.Pivot; p != nil {
		alias := "ant_pivot" + strconv.Itoa(len(b.joins))
		b.joined = append(b.joined, source{})

		b.joins = append(b.joins, qb.Join{
			Kind:   kind,
			Source: qb.Source{Table: p.Table, Alias: alias},
			On: []qb.Cond{
				qb.Compare{
					Left:  owner.col(link.Local),
					Op:    qb.OpEq,
					Right: qb.Column{Alias: alias, Name: p.Local},
				},
			},
		})

		b.joined = append(b.joined, s)

		b.joins = append(b.joins, qb.Join{
			Kind:   kind,
			Source: joined,
			On: append(
				[]qb.Cond{
					qb.Compare{
						Left:  qb.Column{Alias: alias, Name: p.Foreign},
						Op:    qb.OpEq,
						Right: s.col(link.Foreign),
					},
				},
				scope...),
		})

		return
	}

	b.joined = append(b.joined, s)

	b.joins = append(b.joins, qb.Join{
		Kind:   kind,
		Source: joined,
		On: append(
			[]qb.Cond{
				qb.Compare{Left: owner.col(link.Local), Op: qb.OpEq, Right: s.col(link.Foreign)},
			},
			scope...),
	})
}

// joinOn joins target on conditions, or unconditionally for a cross join.
func joinOn(b *builder, kind qb.JoinKind, target source, conds []Cond) {
	s := b.addSource(target, kind)

	on := make([]qb.Cond, 0, len(conds))

	for _, c := range conds {
		if c.err != nil {
			b.fail(c.err)

			continue
		}

		b.refs = append(b.refs, c.refs...)
		on = append(on, c.node)
	}

	b.joined = append(b.joined, s)

	b.joins = append(b.joins, qb.Join{
		Kind:   kind,
		Source: qb.Source{Table: s.model.Table, Alias: s.alias},
		On:     on,
	})
}

// ready returns every error that stops the query from running.
func (b *builder) ready() error {
	err := b.err

	if b.state == nil {
		err = errors.Join(err, ErrNotOpen)
	}

	for _, s := range b.sources {
		err = errors.Join(err, s.model.Err())
	}

	for _, ref := range b.refs {
		if _, ok := b.sourceFor(ref); !ok {
			return errors.Join(err, ErrWrongModel)
		}
	}

	return err
}

// filters returns a source's global filters, resolved at render time.
func (b *builder) filters(s source) []qb.Cond {
	if b.ignoreAll || s.model == nil {
		return nil
	}

	return modelFilters(s, b.ignored)
}

func modelFilters(s source, ignored []string) []qb.Cond {
	var out []qb.Cond

	for _, f := range s.model.Filters {
		if slices.Contains(ignored, f.Name) {
			continue
		}

		out = append(out, qb.Lazy{
			Build: func(ctx context.Context) (qb.Cond, error) {
				c, err := f.Build(ctx)
				if err != nil || c == nil || s.alias == "" {
					return c, err
				}

				return qb.Aliased(c, s.model.Table, s.alias), nil
			},
		})
	}

	return out
}

// conditions returns the WHERE conditions with the root's global filters. Rows
// from a derived source had filters applied where they came from.
func (b *builder) conditions() []qb.Cond {
	if b.from != nil {
		return b.where
	}

	return append(slices.Clip(b.where), b.filters(b.sources[0])...)
}

// statement returns the SELECT for the given list.
func (b *builder) statement(columns []qb.Selected) *qb.Select {
	root := b.sources[0]

	from := qb.Source{Table: root.model.Table, Alias: root.alias}
	if b.from != nil {
		from = *b.from
	}

	joins := slices.Clone(b.joins)
	for i := range joins {
		// In the ON clause, so a filter does not turn a left join into an inner one.
		joins[i].On = append(slices.Clip(joins[i].On), b.filters(b.joined[i])...)
	}

	return &qb.Select{
		With:      b.with,
		Recursive: b.recursive,
		Comment:   b.comment,
		Hint:      b.hint,
		Distinct:  b.distinct,
		Columns:   columns,
		From:      from,
		Joins:     joins,
		Where:     b.conditions(),
		GroupBy:   b.group,
		Having:    b.having,
		Order:     b.order,
		Limit:     b.limit,
		Offset:    b.offset,
		Lock:      b.lock,
	}
}

// allColumns selects every stored column of every source.
func (b *builder) allColumns() []qb.Selected {
	var out []qb.Selected

	for _, s := range b.sources {
		if s.alias == "" {
			out = append(out, s.model.Selection()...)

			continue
		}

		for _, f := range s.model.Stored() {
			out = append(out, qb.Selected{Value: s.col(f)})
		}
	}

	return out
}

// outerRefs returns the models a subquery refers to outside itself.
func (b *builder) outerRefs() []*meta.Model {
	var out []*meta.Model

	for _, ref := range b.refs {
		if _, ok := b.sourceFor(ref); !ok {
			out = append(out, ref)
		}
	}

	return out
}
