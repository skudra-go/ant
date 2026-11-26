package ant

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"reflect"
	"slices"
	"unsafe"

	"github.com/skudra-go/ant/internal/meta"
	"github.com/skudra-go/ant/qb"
)

func reflectZero(r *meta.Relation, entity unsafe.Pointer) {
	reflect.NewAt(r.Type, r.Slot(entity)).Elem().SetZero()
}

// Page is one page of a query, with the totals needed to show paging links. In
// JSON its fields are items, total, number and size.
type Page[T any] struct {
	Items  []*T  `json:"items"`
	Total  int64 `json:"total"`
	Number int   `json:"number"` // from 1
	Size   int   `json:"size"`
}

// Pages returns the number of pages.
func (p *Page[T]) Pages() int {
	if p.Size < 1 {
		return 0
	}

	return int((p.Total + int64(p.Size) - 1) / int64(p.Size))
}

// HasNext reports whether another page follows this one.
func (p *Page[T]) HasNext() bool { return p.Number < p.Pages() }

// within returns q cut down to at most size of its rows, starting skip rows
// in. The query's own Limit and Offset say which rows are its rows, so the
// window lies inside them. It reports false when no row is left there.
func (q *Query[T]) within(skip, size int) (*Query[T], bool) {
	if q.limit != nil {
		size = min(size, *q.limit-skip)
	}

	if size <= 0 {
		return nil, false
	}

	if q.offset != nil {
		skip += *q.offset
	}

	return q.Limit(size).Offset(skip), true
}

// Page returns one page, counted from 1, with the total row count. Order the
// query, or pages may overlap. Use After for large tables, where counting and
// skipping rows gets slow. A Limit or Offset on the query says which rows
// there are to page through. A number or size below 1 returns ErrPaging.
func (q *Query[T]) Page(ctx context.Context, number, size int) (*Page[T], error) {
	if number < 1 || size < 1 {
		return nil, fmt.Errorf("%w: page %d of size %d", ErrPaging, number, size)
	}

	total, err := q.ClearOrder().Count(ctx)
	if err != nil {
		return nil, err
	}

	items := []*T{}

	if page, ok := q.within((number-1)*size, size); ok {
		if items, err = page.All(ctx); err != nil {
			return nil, err
		}
	}

	return &Page[T]{
		Items:  items,
		Total:  total,
		Number: number,
		Size:   size,
	}, nil
}

// Chunk reads matching rows in groups of size, calling f with each until f
// returns an error or the rows run out. Paging uses Offset, so order the query
// and do not change matching rows in f. Use ChunkByKey when f changes them. A
// Limit or Offset on the query says which rows there are to read.
//
// On a context that tracks, a group stays tracked while f runs. Once f returns
// with nothing left to save, as after SaveChanges, Chunk stops tracking what
// the group began tracking, including what f loaded or added, so one group is
// held at a time: edit a group's entities, and save them, inside f. While
// anything on the context is left unsaved, by f or before Chunk, the group
// stays tracked for the save that follows.
func (q *Query[T]) Chunk(ctx context.Context, size int, f func([]*T) error) error {
	if size < 1 {
		return fmt.Errorf("%w: chunk size %d", ErrPaging, size)
	}

	for offset := 0; ; offset += size {
		page, ok := q.within(offset, size)
		if !ok {
			return nil
		}

		mark := q.state.tracker.Mark()

		items, err := page.All(ctx)
		if err != nil {
			return err
		}

		if len(items) > 0 {
			if err := f(items); err != nil {
				return err
			}

			q.state.letGo(ctx, mark)
		}

		if len(items) < size {
			return nil
		}
	}
}

// ChunkByKey reads matching rows in key order, in groups of size, for a single
// or a composite key. It tracks its
// place by key, so f can change or delete the rows it receives. The key is read
// even if Column or Omit left it out. A Limit or Offset on the query counts
// rows in key order: the offset is skipped once, and the limit ends the read.
// Like Chunk, it stops tracking each group once f leaves nothing to save.
func (q *Query[T]) ChunkByKey(ctx context.Context, size int, f func([]*T) error) error {
	if size < 1 {
		return fmt.Errorf("%w: chunk size %d", ErrPaging, size)
	}

	model := q.model()
	if len(model.Keys) == 0 {
		return noKey(model)
	}

	// A composite key is one place too: the rows after (a, b) are those with a
	// greater a, or the same a and a greater b.
	keys := make([]sortColumn, len(model.Keys))
	base := q.next()
	base.order = make([]qb.Order, len(model.Keys))

	for i, key := range model.Keys {
		keys[i] = sortColumn{field: key}
		base.order[i] = qb.Order{Value: q.sources[0].col(key)}

		base.need(key)
	}

	// The place is kept by key from the second group on, so the offset applies
	// to the first only, and the limit is counted down.
	base.limit, base.offset = nil, nil
	left, skip := q.limit, q.offset

	var after []any

	for {
		page := base.next()

		if after != nil {
			page.where = append(page.where, seek(q.sources[0], keys, after))
		} else {
			page.offset = skip
		}

		take := size

		if left != nil {
			take = min(size, *left)
		}

		if take <= 0 {
			return nil
		}

		mark := q.state.tracker.Mark()

		items, err := page.Limit(take).All(ctx)
		if err != nil {
			return err
		}

		if len(items) == 0 {
			return nil
		}

		last := unsafe.Pointer(items[len(items)-1])
		after = make([]any, len(keys))

		for i, k := range keys {
			after[i] = k.field.Value(last).Interface()
		}

		if left != nil {
			rest := *left - len(items)
			left = &rest
		}

		if err := f(items); err != nil {
			return err
		}

		q.state.letGo(ctx, mark)

		if len(items) < size {
			return nil
		}
	}
}

// letGo stops tracking what a group of Chunk or ChunkByKey began tracking since
// mark, its rows, their includes, and what its callback loaded or added, once a
// save would write nothing, so that a walk over a large table holds one group
// at a time (#151). A group whose callback left changes to save stays tracked,
// for the save that follows, and so does one that an entity tracked before the
// group holds in a loaded relation.
func (s *state) letGo(ctx context.Context, mark uint64) {
	end := s.tracker.Mark()
	if end == mark {
		return
	}

	release, err := s.hold(ctx)
	if err != nil {
		return
	}

	defer release()

	u := newUnit(s)
	u.dry = true

	if err := u.collect(); err != nil || u.writesAnything() {
		return
	}

	s.tracker.ForgetAlone(mark, end)
}

// CursorPage is one page from After. In JSON its fields are items and next.
type CursorPage[T any] struct {
	Items []*T `json:"items"`
	// Next is the cursor for the following page, empty on the last page.
	Next string `json:"next"`
}

// sortColumn is one column of a keyset ordering.
type sortColumn struct {
	field *meta.Field
	desc  bool
	// nulls says where the NULLs of a column that can hold one go, first or
	// last, and is NullsDefault for a column that cannot.
	nulls qb.NullsOrder
}

// keyset returns the query's ordering as columns, ending in the primary key so
// that every row has one place.
func (q *Query[T]) keyset() ([]sortColumn, error) {
	model := q.model()

	var out []sortColumn

	for _, o := range q.order {
		column, ok := o.Value.(qb.Column)
		if !ok || column.Table != model.Table || model.Column(column.Name) == nil {
			return nil, fmt.Errorf("%w: order by columns of the queried table", ErrCursor)
		}

		// No comparison matches NULL, so seek tests for it, and needs to know
		// where the NULL rows are: by default PostgreSQL puts them after every
		// value, and the other databases before.
		f := model.Column(column.Name)
		nulls := qb.NullsDefault

		if canBeNull(f) {
			if o.Nulls == qb.NullsDefault {
				return nil, fmt.Errorf(
					"%w: %s can be NULL, and each database puts NULLs in its own place: "+
						"order it with NullsFirst or NullsLast",
					ErrCursor,
					f.Short,
				)
			}

			nulls = o.Nulls
		}

		out = append(out, sortColumn{f, o.Desc, nulls})
	}

	if len(model.Keys) == 0 {
		return nil, noKey(model)
	}

	for _, key := range model.Keys {
		if !slices.ContainsFunc(out, func(c sortColumn) bool { return c.field == key }) {
			out = append(out, sortColumn{field: key})
		}
	}

	return out, nil
}

// After returns the page following cursor, which is empty for the first page.
// Each page costs the same however deep it is, and rows added meanwhile do not
// shift it. The ordering must use columns of this table. A column that can hold
// NULL, as a pointer or sql.Null field can unless its column is configured
// NotNull, needs NullsFirst or NullsLast, since databases put NULLs in
// different places, and returns ErrCursor without:
//
//	db.Posts.Order(ant.Col(&Posts.PublishedAt).Desc().NullsLast()).After(ctx, cursor, 20)
//
// Pass Next from the previous page to continue. The cursor is made of the
// ordering and the key, so those columns are read even if Column or Omit left
// them out. A cursor holds a place and no count of the rows before it, so a
// query with a Limit or Offset is refused.
func (q *Query[T]) After(ctx context.Context, cursor string, size int) (*CursorPage[T], error) {
	if size < 1 {
		return nil, fmt.Errorf("%w: page size %d", ErrPaging, size)
	}

	if q.limit != nil || q.offset != nil {
		return nil, fmt.Errorf(
			"%w: After pages the whole query, without Limit or Offset",
			ErrCursor,
		)
	}

	columns, err := q.keyset()
	if err != nil {
		return nil, err
	}

	n := q.next()

	n.order = make([]qb.Order, len(columns))

	for i, c := range columns {
		n.order[i] = qb.Order{Value: q.sources[0].col(c.field), Desc: c.desc, Nulls: c.nulls}

		n.need(c.field)
	}

	if cursor != "" {
		values, err := decodeCursor(cursor, columns)
		if err != nil {
			return nil, err
		}

		n.where = append(n.where, seek(q.sources[0], columns, values))
	}

	items, err := n.Limit(size + 1).All(ctx)
	if err != nil {
		return nil, err
	}

	page := &CursorPage[T]{Items: items}

	if len(items) > size {
		page.Items = items[:size]

		page.Next, err = encodeCursor(columns, unsafe.Pointer(page.Items[size-1]))
		if err != nil {
			return nil, err
		}
	}

	return page, nil
}

// seek returns the condition for rows after values: (a > x) OR (a = x AND b > y).
// A NULL in a column that can hold one is matched by IS NULL, and placed where
// the column's NullsFirst or NullsLast says.
func seek(s source, columns []sortColumn, values []any) qb.Cond {
	var (
		branches []qb.Cond
		equal    []qb.Cond // the columns so far hold the cursor's values
	)

	for i, c := range columns {
		column := s.col(c.field)
		null := c.nulls != qb.NullsDefault && isNull(values[i])

		if past := c.past(column, values[i], null); past != nil {
			branches = append(branches, qb.And{Children: append(slices.Clip(equal), past)})
		}

		if null {
			equal = append(equal, qb.IsNull{Left: column})
		} else {
			equal = append(
				equal,
				qb.Compare{Left: column, Op: qb.OpEq, Right: fieldParam(c.field, values[i])},
			)
		}
	}

	return qb.Or{Children: branches}
}

// past returns the condition for a value of the column that comes after v in
// the ordering, or nil if none does, as after a NULL that goes last.
func (c sortColumn) past(column qb.Expr, v any, null bool) qb.Cond {
	switch {
	case null && c.nulls == qb.NullsFirst:
		return qb.IsNull{Left: column, Negate: true}
	case null:
		return nil
	}

	op := qb.OpGt

	if c.desc {
		op = qb.OpLt
	}

	beyond := qb.Compare{Left: column, Op: op, Right: fieldParam(c.field, v)}

	if c.nulls == qb.NullsLast {
		return qb.Or{Children: []qb.Cond{beyond, qb.IsNull{Left: column}}}
	}

	return beyond
}

func encodeCursor(columns []sortColumn, entity unsafe.Pointer) (string, error) {
	values := make([]any, len(columns))

	for i, c := range columns {
		value := c.field.Value(entity)
		if c.nulls == qb.NullsDefault && value.Kind() == reflect.Pointer && value.IsNil() {
			return "", fmt.Errorf("%w: %s is NULL", ErrCursor, c.field.Short)
		}

		values[i] = value.Interface()
	}

	text, err := json.Marshal(values)
	if err != nil {
		return "", err
	}

	return base64.RawURLEncoding.EncodeToString(text), nil
}

func decodeCursor(cursor string, columns []sortColumn) ([]any, error) {
	text, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrCursor, err)
	}

	var raw []json.RawMessage
	if err := json.Unmarshal(text, &raw); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrCursor, err)
	}

	if len(raw) != len(columns) {
		return nil, fmt.Errorf("%w: wrong number of values", ErrCursor)
	}

	values := make([]any, len(raw))

	for i, c := range columns {
		value := reflect.New(c.field.Type)
		if err := json.Unmarshal(raw[i], value.Interface()); err != nil {
			return nil, fmt.Errorf("%w: %w", ErrCursor, err)
		}

		values[i] = value.Elem().Interface()
	}

	return values, nil
}
