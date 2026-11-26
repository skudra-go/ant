package ant

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"iter"
	"reflect"
	"unsafe"

	"github.com/skudra-go/ant/internal/meta"
	"github.com/skudra-go/ant/internal/scan"
	"github.com/skudra-go/ant/qb"
)

// Selectable is a query that Project can read from.
type Selectable interface {
	base() *builder
}

func (q *Query[T]) base() *builder { return &q.builder }

// Projection is a query that returns values of R. Results are not tracked.
type Projection[R any] struct {
	builder
	columns []qb.Selected
	reader  func(*sql.Rows) (func() (R, error), error)
}

// Project selects only the mapped expressions, and scans them into R. Use it
// instead of Select to avoid fetching whole rows:
//
//	var Summaries = ant.Fields[Summary]()
//
//	ant.Project[Summary](db.Posts.Join(&Posts.User),
//		ant.Into(&Summaries.Author, &Users.Name),
//		ant.Into(&Summaries.Title, &Posts.Title))
func Project[R any](q Selectable, mappings ...Mapping) *Projection[R] {
	p := &Projection[R]{builder: q.base().cloned()}
	model := configure[R]()

	// R scans by its fields, so a struct ant could not read fails the projection
	// once, rather than each of its mappings.
	if err := model.Err(); err != nil {
		p.fail(err)

		mappings = nil
	}

	for _, m := range mappings {
		switch {
		case m.err != nil:
			p.fail(m.err)
		case !model.Has(m.dst):
			p.fail(fmt.Errorf("%w: Into needs a probe of %s", ErrWrongModel, model.Type.Name()))
		default:
			p.refs = append(p.refs, m.refs...)
			p.columns = append(p.columns, qb.Selected{Value: m.node, Alias: m.dst.Column})
		}
	}

	p.reader = func(rows *sql.Rows) (func() (R, error), error) {
		into, err := scan.Projected[R](rows, zeroOnNull)
		if err != nil {
			return nil, err
		}

		return func() (R, error) {
			var v R

			return v, into(&v)
		}, nil
	}

	return p
}

// zeroOnNull says that a projection reads NULL into f as its zero value: f's
// type gives a NOT NULL column, as a number, text, a time, a uuid or a decimal
// does, and an aggregate over no rows or a column of a left join's side gives
// NULL in ordinary use, as MaxOf's zero does (decision 147). A pointer or an
// sql.Null keeps the NULL, and a type only the driver converts is left to it.
func zeroOnNull(f *meta.Field) bool {
	_, nullable := columnValue(f.Type)

	return !nullable && !f.DriverOnly()
}

// projectRows reads every source of b into fresh values and passes them to f.
func projectRows[R any](b builder, f func([]unsafe.Pointer) R) *Projection[R] {
	p := &Projection[R]{builder: b.cloned(), columns: b.allColumns()}
	p.reader = func(rows *sql.Rows) (func() (R, error), error) {
		row := newRowReader(p.sources, rows)

		return func() (R, error) {
			var zero R

			values, err := row.read()
			if err != nil {
				return zero, err
			}

			return f(values), nil
		}, nil
	}

	return p
}

// rowReader scans a joined row into one value per source.
type rowReader struct {
	rows    *sql.Rows
	sources []source
	fields  [][]*meta.Field
	// drivers holds, for a source that may have no row, the columns only the
	// driver converts, by field, nil for the others.
	drivers [][]*driverColumn
	dest    []any
	values  []unsafe.Pointer
	seen    []bool
}

func newRowReader(sources []source, rows *sql.Rows) *rowReader {
	r := &rowReader{
		rows:    rows,
		sources: sources,
		fields:  make([][]*meta.Field, len(sources)),
		drivers: make([][]*driverColumn, len(sources)),
		values:  make([]unsafe.Pointer, len(sources)),
		seen:    make([]bool, len(sources)),
	}

	columns := 0

	for i, s := range sources {
		r.fields[i] = s.model.Stored()
		columns += len(r.fields[i])

		if s.nullable {
			r.drivers[i] = driverColumns(r.fields[i])
		}
	}

	r.dest = make([]any, columns)

	return r
}

// nullableColumn scans into dst unless the column is NULL, and records whether
// any column of its source held a value.
type nullableColumn struct {
	dst  any
	seen *bool
}

// Scan implements sql.Scanner.
func (c *nullableColumn) Scan(src any) error {
	if src == nil {
		return nil
	}

	*c.seen = true

	return sql.ConvertAssign(driver.ScanContext{}, c.dst, src)
}

// driverColumn holds a column of a source that may have no row, of a type only
// the driver converts, as pgx does netip.Addr or []string: database/sql would
// refuse it from the value nullableColumn is given. It is scanned into a pointer
// to the field's type, which NULL leaves nil, and moved into the field after.
type driverColumn struct {
	field *meta.Field
	ptr   reflect.Value // a **T, T the field's type
}

// driverColumns returns a driverColumn for each field only the driver converts,
// by position, and nil if there is none.
func driverColumns(fields []*meta.Field) []*driverColumn {
	var out []*driverColumn

	for i, f := range fields {
		if !f.DriverOnly() {
			continue
		}

		if out == nil {
			out = make([]*driverColumn, len(fields))
		}

		out[i] = &driverColumn{field: f, ptr: reflect.New(reflect.PointerTo(f.Type))}
	}

	return out
}

// dest clears what the last row left and returns what to scan into.
func (c *driverColumn) dest() any {
	c.ptr.Elem().SetZero()

	return c.ptr.Interface()
}

// settle moves a value the row held into the field of entity, and reports
// whether there was one.
func (c *driverColumn) settle(entity unsafe.Pointer) bool {
	held := c.ptr.Elem()
	if held.IsNil() {
		return false
	}

	c.field.Value(entity).Set(held.Elem())

	return true
}

// outerDest returns what the column of field j of a source that may have no row
// is scanned into, for the entity at entity.
func outerDest(
	drivers []*driverColumn,
	j int,
	f *meta.Field,
	entity unsafe.Pointer,
	seen *bool,
) any {
	if drivers != nil && drivers[j] != nil {
		return drivers[j].dest()
	}

	return &nullableColumn{dst: f.Dest(entity), seen: seen}
}

// settleOuter moves what the driver columns of a source read into entity, and
// records in seen whether any of them held a value.
func settleOuter(drivers []*driverColumn, entity unsafe.Pointer, seen *bool) {
	for _, c := range drivers {
		if c != nil && c.settle(entity) {
			*seen = true
		}
	}
}

// read returns one pointer per source, nil for an unmatched outer join side.
func (r *rowReader) read() ([]unsafe.Pointer, error) {
	column := 0

	for i, s := range r.sources {
		r.values[i] = s.alloc()

		r.seen[i] = false

		for j, f := range r.fields[i] {
			if s.nullable {
				r.dest[column] = outerDest(r.drivers[i], j, f, r.values[i], &r.seen[i])
			} else {
				r.dest[column] = f.Dest(r.values[i])
			}

			column++
		}
	}

	if err := r.rows.Scan(r.dest...); err != nil {
		return nil, err
	}

	for i, s := range r.sources {
		if s.nullable {
			settleOuter(r.drivers[i], r.values[i], &r.seen[i])
		}

		if s.nullable && !r.seen[i] {
			r.values[i] = nil

			continue
		}

		for _, f := range r.fields[i] {
			f.InUTC(r.values[i])
		}
	}

	return r.values, nil
}

// runnable returns every error that stops the projection from running.
func (p *Projection[R]) runnable() error {
	if err := p.ready(); err != nil {
		return err
	}

	if len(p.columns) == 0 {
		return ErrNoColumns
	}

	return nil
}

func (p *Projection[R]) selection() []qb.Selected { return p.columns }

// Debug logs the SQL and values of each SELECT the projection runs. See
// Query.Debug.
func (p *Projection[R]) Debug() *Projection[R] {
	n := *p
	n.debug = true

	return &n
}

// ToSQL returns the SELECT the projection runs. Global filters that need a
// context see an empty one.
func (p *Projection[R]) ToSQL() (string, []any, error) {
	return p.ToSQLContext(context.Background())
}

// ToSQLContext is ToSQL with global filters resolved against ctx.
func (p *Projection[R]) ToSQLContext(ctx context.Context) (string, []any, error) {
	if err := p.runnable(); err != nil {
		return "", nil, err
	}

	return p.selectSQL(ctx, p.columns)
}

// Count returns the number of rows the projection returns, counted over a
// subquery of its SELECT, so that Limit, Offset, Distinct, GroupBy and the
// columns themselves, such as an aggregate with no GroupBy, decide them.
func (p *Projection[R]) Count(ctx context.Context) (int64, error) {
	if err := p.runnable(); err != nil {
		return 0, err
	}

	return countRows(ctx, &p.builder, p.selection, true)
}

// Exists reports whether the projection returns any row. It stops at the
// first one.
func (p *Projection[R]) Exists(ctx context.Context) (bool, error) {
	if err := p.runnable(); err != nil {
		return false, err
	}

	return existsRows(ctx, &p.builder, p.selection, true)
}

func (p *Projection[R]) open(ctx context.Context) (*sql.Rows, func() (R, error), error) {
	if err := p.runnable(); err != nil {
		return nil, nil, err
	}

	if p.toMany > 1 {
		p.state.warnCartesian(ctx, p.toMany)
	}

	rows, err := p.query(ctx, p.statement(p.columns))
	if err != nil {
		return nil, nil, err
	}

	read, err := p.reader(rows)
	if err != nil {
		return nil, nil, errors.Join(err, rows.Close())
	}

	return rows, read, nil
}

// All returns every projected row, as an empty slice if there are none.
func (p *Projection[R]) All(ctx context.Context) (out []R, err error) {
	rows, read, err := p.open(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { err = errors.Join(err, rows.Close()) }()

	out = []R{}

	for rows.Next() {
		v, err := read()
		if err != nil {
			return nil, err
		}

		out = append(out, v)
	}

	return out, rows.Err()
}

// First returns the first projected row, or ErrNotFound.
func (p *Projection[R]) First(ctx context.Context) (*R, error) {
	n := *p
	n.limit = atMost(p.limit, 1)

	found, err := n.All(ctx)

	switch {
	case err != nil:
		return nil, err
	case len(found) == 0:
		return nil, ErrNotFound
	}

	return &found[0], nil
}

// Each streams projected rows, stopping after the first error. In a
// transaction, a statement inside the loop returns ErrQueryInEach, as with
// Query.Each.
func (p *Projection[R]) Each(ctx context.Context) iter.Seq2[R, error] {
	return func(yield func(R, error) bool) {
		var zero R

		rows, read, err := p.open(ctx)
		if err != nil {
			yield(zero, err)

			return
		}
		defer rows.Close()

		// As in Query.Each, the connection streams until the loop ends.
		on := p.state.streamsOf(ctx)

		on.start()
		defer on.stop()

		for rows.Next() {
			v, err := read()
			if err != nil {
				yield(zero, err)

				return
			}

			if !yield(v, nil) {
				return
			}
		}

		if err := rows.Err(); err != nil {
			yield(zero, err)
		}
	}
}
