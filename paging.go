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

// Page is one page of a query, with the totals needed to show paging links.
type Page[T any] struct {
	Items  []*T
	Total  int64
	Number int // from 1
	Size   int
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

// Page returns one page, counted from 1, with the total row count. Order the
// query, or pages may overlap. Use After for large tables, where counting and
// skipping rows gets slow.
func (q *Query[T]) Page(ctx context.Context, number, size int) (*Page[T], error) {
	if number < 1 || size < 1 {
		return nil, fmt.Errorf("ant: page %d of size %d", number, size)
	}

	total, err := q.ClearOrder().Count(ctx)
	if err != nil {
		return nil, err
	}

	items, err := q.Limit(size).Offset((number - 1) * size).All(ctx)
	if err != nil {
		return nil, err
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
// and do not change matching rows in f. Use ChunkByKey when f changes them.
func (q *Query[T]) Chunk(ctx context.Context, size int, f func([]*T) error) error {
	if size < 1 {
		return fmt.Errorf("ant: chunk size %d", size)
	}

	for offset := 0; ; offset += size {
		items, err := q.Limit(size).Offset(offset).All(ctx)
		if err != nil {
			return err
		}

		if len(items) > 0 {
			if err := f(items); err != nil {
				return err
			}
		}

		if len(items) < size {
			return nil
		}
	}
}

// ChunkByKey reads matching rows in key order, in groups of size. It tracks its
// place by key, so f can change or delete the rows it receives.
func (q *Query[T]) ChunkByKey(ctx context.Context, size int, f func([]*T) error) error {
	if size < 1 {
		return fmt.Errorf("ant: chunk size %d", size)
	}

	key := q.model().Key()
	if key == nil {
		return ErrNoKey
	}

	column := q.sources[0].col(key)
	base := q.next()
	base.order = []qb.Order{{Value: column}}

	var after any

	for {
		page := base.next()

		if after != nil {
			page.where = append(
				page.where,
				qb.Compare{Left: column, Op: qb.OpGt, Right: fieldParam(key, after)},
			)
		}

		items, err := page.Limit(size).All(ctx)
		if err != nil {
			return err
		}

		if len(items) == 0 {
			return nil
		}

		after = key.Value(unsafe.Pointer(items[len(items)-1])).Interface()

		if err := f(items); err != nil {
			return err
		}

		if len(items) < size {
			return nil
		}
	}
}

// CursorPage is one page from After.
type CursorPage[T any] struct {
	Items []*T
	// Next is the cursor for the following page, empty on the last page.
	Next string
}

// sortColumn is one column of a keyset ordering.
type sortColumn struct {
	field *meta.Field
	desc  bool
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

		out = append(out, sortColumn{model.Column(column.Name), o.Desc})
	}

	if len(model.Keys) == 0 {
		return nil, ErrNoKey
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
// shift it. The ordering must use columns of this table that are never NULL. Pass
// Next from the previous page to continue.
func (q *Query[T]) After(ctx context.Context, cursor string, size int) (*CursorPage[T], error) {
	if size < 1 {
		return nil, fmt.Errorf("ant: page size %d", size)
	}

	columns, err := q.keyset()
	if err != nil {
		return nil, err
	}

	n := q.next()

	n.order = make([]qb.Order, len(columns))

	for i, c := range columns {
		n.order[i] = qb.Order{Value: q.sources[0].col(c.field), Desc: c.desc}
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
func seek(s source, columns []sortColumn, values []any) qb.Cond {
	var branches []qb.Cond

	for i, c := range columns {
		var all []qb.Cond

		for j := range i {
			all = append(
				all,
				qb.Compare{
					Left:  s.col(columns[j].field),
					Op:    qb.OpEq,
					Right: fieldParam(columns[j].field, values[j]),
				},
			)
		}

		op := qb.OpGt

		if c.desc {
			op = qb.OpLt
		}

		branches = append(
			branches,
			qb.And{
				Children: append(
					all,
					qb.Compare{Left: s.col(c.field), Op: op, Right: fieldParam(c.field, values[i])},
				),
			},
		)
	}

	return qb.Or{Children: branches}
}

func encodeCursor(columns []sortColumn, entity unsafe.Pointer) (string, error) {
	values := make([]any, len(columns))

	for i, c := range columns {
		value := c.field.Value(entity)
		if value.Kind() == reflect.Pointer && value.IsNil() {
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
