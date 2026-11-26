package ant

import (
	"cmp"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"time"
	"unsafe"

	"github.com/skudra-go/ant/dialect"
	"github.com/skudra-go/ant/internal/exec"
	"github.com/skudra-go/ant/internal/meta"
	"github.com/skudra-go/ant/internal/track"
	"github.com/skudra-go/ant/qb"
)

// FieldRef names a column of any type, from F.
type FieldRef struct {
	field *meta.Field
	model *meta.Model
	err   error
}

// F names a column, for APIs that take a list of columns of different types.
func F[V any](field *V) FieldRef {
	f, model, err := fieldOf(field)

	return FieldRef{f, model, err}
}

// errEmptyFieldRef reports the zero FieldRef, which a map lookup that misses
// returns.
var errEmptyFieldRef = fmt.Errorf(
	"%w: the FieldRef is empty, as a map lookup that misses returns",
	ErrNotProbe,
)

// fault returns why the reference names no column, or nil.
func (r FieldRef) fault() error {
	if r.err == nil && r.field == nil {
		return errEmptyFieldRef
	}

	return r.err
}

// Asc orders by the column, ascending, as Col(field).Asc() does. A map from the
// names a request may sort by to FieldRef holds columns of different types, and
// a name that is not in it is refused:
//
//	sorts := map[string]ant.FieldRef{"name": ant.F(&Users.Name), "age": ant.F(&Users.Age)}
//	if ref, ok := sorts[orderBy]; ok {
//		q = q.Order(ref.Asc())
//	}
//
// The zero FieldRef, from a lookup that misses, makes the query fail with
// ErrNotProbe.
func (r FieldRef) Asc() Sort { return r.sort(false) }

// Desc orders by the column, descending.
func (r FieldRef) Desc() Sort { return r.sort(true) }

func (r FieldRef) sort(desc bool) Sort {
	if err := r.fault(); err != nil {
		return Sort{err: err}
	}

	return Sort{
		order: qb.Order{Value: r.model.ColumnOf(r.field), Desc: desc},
		refs:  r.model.Self(),
	}
}

// Conflict says what to do when an insert hits a unique constraint. Start one
// with OnConflict.
type Conflict struct {
	columns []string
	update  []string
	set     []qb.Assignment
	nothing bool
	err     error
}

// OnConflict names the column of the unique constraint. Add more with And.
func OnConflict[V any](field *V) *Conflict { return (&Conflict{}).add(F(field)) }

func (c *Conflict) add(ref FieldRef) *Conflict {
	n := *c
	n.columns = slices.Clone(c.columns)

	if err := ref.fault(); err != nil {
		n.err = errors.Join(n.err, err)

		return &n
	}

	n.columns = append(n.columns, ref.field.Column)

	return &n
}

// And adds a column for a constraint on several columns.
func (c *Conflict) And(ref FieldRef) *Conflict { return c.add(ref) }

// DoNothing keeps the existing row.
func (c *Conflict) DoNothing() *Conflict {
	n := *c
	n.nothing = true

	return &n
}

// DoUpdate overwrites only the named columns of the existing row. By default,
// every column except the key and the constraint's own is overwritten, with the
// entity's values, including a zero the insert leaves to a column's default.
func (c *Conflict) DoUpdate(fields ...FieldRef) *Conflict {
	n := *c
	n.update = slices.Clone(c.update)

	for _, ref := range fields {
		if err := ref.fault(); err != nil {
			n.err = errors.Join(n.err, err)

			continue
		}

		n.update = append(n.update, ref.field.Column)
	}

	return &n
}

// Set assigns values on conflict, as well as any named in DoUpdate.
func (c *Conflict) Set(assignments ...Assignment) *Conflict {
	n := *c
	n.set = slices.Clone(c.set)

	for _, a := range assignments {
		if a.err != nil {
			n.err = errors.Join(n.err, a.err)

			continue
		}

		n.set = append(n.set, a.node)
	}

	return &n
}

// clause returns the ON CONFLICT clause for a model. An update moves the version
// on, as every write does, so a copy loaded before it cannot save over it. The
// version is ant's to keep, so naming it is an error. An update also sets
// UpdatedAt to now, unless the clause assigns it: the inserted row's value is
// the struct's, which for a loaded copy is the time it was last saved.
//
// The update overwrites a column with the row the insert offers, except where
// the insert leaves the column to the database: a zero field with a default,
// whose offered value is the database's default. There it writes what e holds,
// zero or not, as Update would (#142). E is nil when the clause is only checked.
//
// Offered names the columns of an INSERT ... SELECT, nil for rows of entities.
// Its offered row holds the default of every other column, so the update
// overwrites only the offered ones, and naming another in DoUpdate is an error.
func (c *Conflict) clause(
	model *meta.Model,
	now time.Time,
	e unsafe.Pointer,
	offered []string,
) (*qb.Conflict, error) {
	out := &qb.Conflict{Columns: c.columns, Target: partialTarget(model, c.columns)}

	if c.nothing {
		return out, nil
	}

	if err := managedColumns(model, c.update, c.set); err != nil {
		return nil, err
	}

	for _, name := range c.update {
		if offered == nil || slices.Contains(offered, name) {
			continue
		}

		what := "column " + name

		if f := model.Column(name); f != nil {
			what = model.Type.Name() + "." + f.Name
		}

		return nil, fmt.Errorf("ant: DoUpdate names %s, which the projection does not map", what)
	}

	names := c.update
	if len(names) == 0 && len(c.set) == 0 {
		for _, f := range model.Updatable() {
			if slices.Contains(c.columns, f.Column) || f == model.Version || f == model.Updated {
				continue
			}

			if offered == nil || slices.Contains(offered, f.Column) {
				names = append(names, f.Column)
			}
		}
	}

	for _, name := range names {
		var value qb.Expr = qb.Column{Alias: "excluded", Name: name}

		if f := model.Column(name); e != nil && f != nil && f.Default && f.Value(e).IsZero() {
			value = fieldParam(f, f.Value(e).Interface())
		}

		out.Set = append(out.Set, qb.Assignment{Column: name, Value: value})
	}

	out.Set = append(out.Set, c.set...)

	if err := assignedTwice(model, "assignments", assignedColumns(out.Set)); err != nil {
		return nil, err
	}

	if f := model.Updated; f != nil && !slices.Contains(assignedColumns(out.Set), f.Column) {
		out.Set = append(
			out.Set,
			qb.Assignment{Column: f.Column, Value: qb.Param{Value: timeValue(f, now).Interface()}},
		)
	}

	if v := model.Version; v != nil {
		out.Set = append(out.Set, nextVersion(model))
	}

	return out, nil
}

// partialTarget returns the predicate of the partial unique index on exactly the
// conflict's columns, which PostgreSQL and SQLite match to ON CONFLICT only when
// the statement repeats it: a soft-deleted model keeps its emails unique that
// way, and an upsert by email found no constraint (#150). Columns that a key or
// another unique index covers whole need none, and two partial indexes on them
// leave the database to say which it can't choose.
func partialTarget(model *meta.Model, columns []string) []qb.Cond {
	if len(columns) == 0 {
		return nil
	}

	same := func(fields []*meta.Field) bool {
		return len(fields) == len(columns) &&
			!slices.ContainsFunc(fields, func(f *meta.Field) bool {
				return f == nil || !slices.Contains(columns, f.Column)
			})
	}

	if same(model.Keys) {
		return nil
	}

	if f := model.Column(columns[0]); len(columns) == 1 && f != nil && f.Unique {
		return nil
	}

	var partial []*meta.Index

	for _, idx := range model.Indexes {
		fields := make([]*meta.Field, len(idx.Parts))

		for i, p := range idx.Parts {
			fields[i] = p.Field
		}

		switch {
		case !idx.Unique || !same(fields):
		case idx.Where == "":
			return nil
		default:
			partial = append(partial, idx)
		}
	}

	if len(partial) != 1 {
		return nil
	}

	return []qb.Cond{qb.Fragment{SQL: partial[0].Where}}
}

// managedColumns refuses an update that names a column ant keeps itself: the
// version, which ant moves on, and fields that updates never write, which are
// immutable, readonly, or computed. Without this, an explicit assignment would
// change what the tag says stays put.
func managedColumns(model *meta.Model, columns []string, set []qb.Assignment) error {
	named := slices.Clone(columns)

	for _, a := range set {
		named = append(named, a.Column)
	}

	for _, column := range named {
		f := model.Column(column)
		if f == nil {
			continue
		}

		var why string

		switch {
		case f == model.Version:
			why = "a version, which ant moves on by itself"
		case f.Immutable:
			why = "immutable, written on insert only"
		case f.ReadOnly:
			why = "readonly, filled by the database"
		case f.Computed:
			why = "computed, never written"
		default:
			continue
		}

		return fmt.Errorf("ant: %s.%s is %s; leave it out", model.Type.Name(), f.Name, why)
	}

	return nil
}

// one is a literal 1, which takes no parameter: InsertMany fills a statement
// with as many rows as the parameter limit allows.
var one = qb.Fragment{SQL: "1"}

// nextVersion is the assignment that moves the stored version on by one.
func nextVersion(model *meta.Model) qb.Assignment {
	v := model.Version

	return qb.Assignment{
		Column: v.Column,
		Value: qb.Binary{
			Left:  qb.Column{Table: model.Table, Name: v.Column},
			Op:    "+",
			Right: one,
		},
	}
}

// insertStatement returns a one-row INSERT for e. Generated zero keys are left
// out for the database to fill in.
func insertStatement(model *meta.Model, e unsafe.Pointer) *qb.Insert {
	stmt := &qb.Insert{Table: model.Table}

	var row []qb.Expr

	for _, f := range model.Writable() {
		if f.FromDatabase() && f.Value(e).IsZero() {
			continue
		}

		stmt.Columns = append(stmt.Columns, f.Column)
		row = append(row, fieldParam(f, f.Value(e).Interface()))
	}

	if len(row) > 0 {
		stmt.Rows = [][]qb.Expr{row}
	}

	return stmt
}

func storedColumns(model *meta.Model) []qb.Selected {
	fields := model.Stored()

	out := make([]qb.Selected, len(fields))

	for i, f := range fields {
		out[i] = qb.Selected{Value: qb.Column{Name: f.Column}}
	}

	return out
}

// Upsert inserts e, or follows conflict if a unique constraint already holds the
// row. With an update, e receives the stored row, including its key. With
// DoNothing, it stays as it was and Upsert reports false. It offers e as a new
// row, so it checks the Validate rules and runs Validate, BeforeInsert, and
// AfterInsert whichever way the database goes, as GORM runs its create hooks.
func (t *Table[T]) Upsert(ctx context.Context, e *T, conflict *Conflict) (bool, error) {
	model, err := t.model()

	switch {
	case err != nil:
		return false, err
	case e == nil:
		return false, ErrNilEntity
	case conflict == nil:
		return false, errors.New("ant: Upsert needs a Conflict")
	case conflict.err != nil:
		return false, conflict.err
	}

	if model.ReadOnly {
		return false, fmt.Errorf("%w: %s", ErrReadOnly, model.Type.Name())
	}

	t = t.in(ctx)
	now := t.state.settings.timestamp()

	if _, err := conflict.clause(model, now, nil, nil); err != nil {
		return false, err
	}

	ctx, release, err := t.state.claim(ctx, t.state.touchesTracker())
	if err != nil {
		return false, err
	}

	defer release()

	// The hooks run in the write's transaction, the context's or the one ctx is
	// bound to, and what they run with their ctx does too (#144). BeforeInsert
	// runs in it as well, so what it writes is undone with the upsert. Outside a
	// transaction, only an AfterInsert opens one: with Before hooks alone, one
	// row opens none, as Insert does, and what BeforeInsert writes stands.
	hooked := ctx.Value(skipHooks{}) == nil && hooksOf(model).writes()

	n := &node{model: model, entity: unsafe.Pointer(e)}
	u := newUnit(t.state)

	// missed is a conflict that wrote nothing, which readInto reports as
	// ErrNotFound, told apart from a hook's own ErrNotFound.
	var checked, missed bool

	// An AfterInsert error has to undo the write, which a transaction covers.
	err = t.state.transactHooked(
		ctx,
		hooksOf(model).afterInsert,
		hooked,
		func(ctx context.Context, to exec.Executor) error {
			if err := n.beforeInsert(ctx); err != nil {
				return err
			}

			// The keys the database fills in, taken back if the write is.
			for _, f := range model.Keys {
				if f.FromDatabase() && f.Value(n.entity).IsZero() {
					n.generated = append(n.generated, f)
				}
			}

			if err := zeroKey(model, n.entity); err != nil {
				return err
			}

			u.stamp(n)

			// The clause is made from e as the hooks and stamps left it, as the
			// insert is, so that both leave the same columns to the database.
			clause, err := conflict.clause(model, now, unsafe.Pointer(e), nil)
			if err != nil {
				return err
			}

			// A copy that carries a version updates only the row it was loaded
			// from. A new entity, whose version only the stamp gave it, has none
			// to check.
			checked = !slices.Contains(n.stamped, model.Version) &&
				t.versionCheck(model, unsafe.Pointer(e), clause)

			stmt := insertStatement(model, unsafe.Pointer(e))
			stmt.Conflict = clause

			rows, err := t.upsertRows(ctx, to, model, stmt, unsafe.Pointer(e), checked)
			if err == nil {
				err = readInto(rows, e)
			}

			if err != nil {
				missed = errors.Is(err, ErrNotFound)

				return err
			}

			return n.afterInsert(ctx)
		},
	)
	if err != nil {
		for _, f := range slices.Concat(n.generated, n.stamped) {
			f.Value(n.entity).SetZero()
		}

		switch {
		case missed && checked:
			key, _ := model.KeyOf(unsafe.Pointer(e))

			return false, &ConcurrencyError{Model: model.Type.Name(), Key: key}
		case missed:
			return false, nil
		}

		return false, err
	}

	if t.state.settings.tracking && model.Key() != nil {
		// As a unit's commit does: a rollback takes back the key and the stamps
		// and, for an entity tracked here, stops tracking it.
		tracker := t.state.tracker

		if entry := tracker.Get(unsafe.Pointer(e)); entry != nil {
			t.state.record(ctx, tracker.Saved(entry, track.Unchanged, u.undoing(n)))
			entry.Stored(n.written)
			tracker.Rekey(entry)
		} else {
			t.state.record(ctx, tracker.Added(model, unsafe.Pointer(e), u.undoing(n)))

			if added := tracker.Get(unsafe.Pointer(e)); added != nil {
				added.Stored(n.written)
			}
		}
	}

	return true, nil
}

// versionCheck limits the conflict's update to the version e carries, if any, and
// reports whether it did. A zero version is a new entity, which has none to
// check. ON DUPLICATE KEY takes no condition, so there each assignment keeps the
// stored value unless the version matches. MySQL and MariaDB run them in order,
// each seeing what those before it set, and the version's comes last, so every
// one reads the version the row had. A copy that is out of date then changes
// nothing, which the row count tells.
func (t *Table[T]) versionCheck(model *meta.Model, e unsafe.Pointer, c *qb.Conflict) bool {
	v := model.Version

	switch {
	case v == nil, len(c.Set) == 0, v.Value(e).IsZero():
		return false
	}

	same := qb.Compare{
		Left:  qb.Column{Table: model.Table, Name: v.Column},
		Op:    qb.OpEq,
		Right: qb.Param{Value: v.Value(e).Interface()},
	}

	if dialect.Supports(t.state.dialect, dialect.ConflictWhere) {
		c.Where = append(c.Where, same)

		return true
	}

	for i, a := range c.Set {
		c.Set[i].Value = qb.Case{
			Whens: []qb.When{{If: same, Then: a.Value}},
			Else:  qb.Column{Table: model.Table, Name: a.Column},
		}
	}

	return true
}

// upsertRows runs an upsert and returns the stored row, or ErrNotFound if it did
// nothing. ON DUPLICATE KEY returns the old row even when it changed nothing, so
// there, as with no RETURNING at all, the row count decides, and the row is read
// back by the conflict's columns. Checked says the update checks a version,
// which moves on whenever it goes ahead, so no change means it was turned down.
func (t *Table[T]) upsertRows(
	ctx context.Context,
	to exec.Executor,
	model *meta.Model,
	stmt *qb.Insert,
	e unsafe.Pointer,
	checked bool,
) (*sql.Rows, error) {
	if t.state.dialect.SupportsReturning() && !dialect.SyntaxOf(t.state.dialect).DuplicateKey {
		stmt.Returning = storedColumns(model)

		return t.state.queryOn(ctx, to, stmt)
	}

	if len(stmt.Conflict.Columns) == 0 {
		return nil, fmt.Errorf(
			"%w: an upsert with no conflict columns on %s",
			ErrUnsupported,
			t.state.dialect.Name(),
		)
	}

	changed, err := t.state.runOn(ctx, to, stmt)

	switch {
	case err != nil:
		return nil, err
	// No row changed: the conflict did nothing, or its condition, such as a
	// copy's version, turned the update down.
	case changed == 0 && (len(stmt.Conflict.Set) == 0 || checked):
		return nil, ErrNotFound
	}

	read := &qb.Select{Columns: storedColumns(model), From: qb.Source{Table: model.Table}}

	for _, name := range stmt.Conflict.Columns {
		f := model.Column(name)
		read.Where = append(
			read.Where,
			qb.Compare{
				Left:  qb.Column{Name: name},
				Op:    qb.OpEq,
				Right: fieldParam(f, f.Value(e).Interface()),
			},
		)
	}

	return t.state.queryOn(ctx, to, read)
}

// insertOptions configures InsertMany.
type insertOptions struct {
	batch    int
	conflict *Conflict
}

// InsertOption configures InsertMany and InsertFrom.
type InsertOption func(*insertOptions)

// BatchSize sets the most rows in one statement. The default is 1000.
func BatchSize(n int) InsertOption { return func(o *insertOptions) { o.batch = n } }

// WithConflict applies a Conflict to every row of InsertMany or InsertFrom.
func WithConflict(c *Conflict) InsertOption { return func(o *insertOptions) { o.conflict = c } }

// InsertMany writes many rows quickly, in multi-row statements inside one
// transaction, and returns how many were inserted. Unlike Insert, it ignores
// relations and does not track the rows. Generated keys are copied back, except
// with WithConflict, where rows may be skipped.
func (t *Table[T]) InsertMany(
	ctx context.Context,
	entities []*T,
	options ...InsertOption,
) (int64, error) {
	model, err := t.model()
	if err != nil {
		return 0, err
	}

	o := insertOptions{batch: 1000}

	for _, option := range options {
		option(&o)
	}

	if o.batch < 1 {
		return 0, fmt.Errorf("ant: batch size %d", o.batch)
	}

	if o.conflict != nil && o.conflict.err != nil {
		return 0, o.conflict.err
	}

	if slices.Contains(entities, nil) {
		return 0, ErrNilEntity
	}

	if len(entities) == 0 {
		return 0, nil
	}

	if model.ReadOnly {
		return 0, fmt.Errorf("%w: %s", ErrReadOnly, model.Type.Name())
	}

	if o.conflict != nil {
		if _, err := o.conflict.clause(model, time.Time{}, nil, nil); err != nil {
			return 0, err
		}
	}

	for _, e := range entities {
		if err := zeroKey(model, unsafe.Pointer(e)); err != nil {
			return 0, err
		}
	}

	// One node serves every row: nothing here reads what stamp notes on it.
	stamper, stamped := newUnit(t.state), node{model: model}

	for _, e := range entities {
		stamped.entity, stamped.stamped = unsafe.Pointer(e), stamped.stamped[:0]
		stamper.stamp(&stamped)
	}

	var (
		total    int64
		assigned []*T
	)

	key := model.Key()

	groups := keyGroups(model, entities)

	if o.conflict != nil && !o.conflict.nothing {
		groups = defaultGroups(model, groups)
	}

	err = t.state.transact(ctx, true, func(to exec.Executor) error {
		for _, group := range groups {
			generated := key != nil && key.Generated && key.Value(unsafe.Pointer(group[0])).IsZero()
			columns := insertColumns(model, generated)

			size := min(
				o.batch,
				dialect.SyntaxOf(t.state.dialect).ParameterLimit()/max(len(columns), 1),
			)

			if generated && o.conflict == nil && !t.state.dialect.SupportsReturning() {
				size = 1 // LastInsertId reports one key
			}

			for batch := range slices.Chunk(group, size) {
				n, err := t.insertBatch(ctx, to, model, batch, columns, o.conflict, generated)
				if err != nil {
					return err
				}

				total += n

				if generated && o.conflict == nil {
					assigned = append(assigned, batch...)
				}
			}
		}

		return nil
	})
	if err != nil {
		for _, e := range assigned {
			key.Value(unsafe.Pointer(e)).SetZero()
		}

		return 0, err
	}

	return total, nil
}

// keyGroups splits entities into those that need a generated key and the rest,
// which need different column lists.
func keyGroups[T any](model *meta.Model, entities []*T) [][]*T {
	key := model.Key()
	if key == nil || !key.Generated {
		return [][]*T{entities}
	}

	var generated, supplied []*T

	for _, e := range entities {
		if key.Value(unsafe.Pointer(e)).IsZero() {
			generated = append(generated, e)
		} else {
			supplied = append(supplied, e)
		}
	}

	return slices.DeleteFunc([][]*T{generated, supplied}, func(g []*T) bool { return len(g) == 0 })
}

// defaultGroups splits each group by which columns with a default its entities
// leave zero. The insert leaves those to the database, and a conflict's update
// writes what the rows hold there instead, so each statement holds rows that
// leave the same columns zero, and its update writes their zero.
func defaultGroups[T any](model *meta.Model, groups [][]*T) [][]*T {
	var defaulted []*meta.Field

	for _, f := range model.Writable() {
		if f.Default {
			defaulted = append(defaulted, f)
		}
	}

	if len(defaulted) == 0 {
		return groups
	}

	var out [][]*T

	pattern := make([]byte, len(defaulted))

	for _, group := range groups {
		index := map[string]int{}
		first := len(out)

		for _, e := range group {
			for i, f := range defaulted {
				pattern[i] = '0'

				if f.Value(unsafe.Pointer(e)).IsZero() {
					pattern[i] = '1'
				}
			}

			i, ok := index[string(pattern)]
			if !ok {
				i = len(out) - first
				index[string(pattern)] = i

				out = append(out, nil)
			}

			out[first+i] = append(out[first+i], e)
		}
	}

	return out
}

func insertColumns(model *meta.Model, generated bool) []*meta.Field {
	if !generated {
		return model.Writable()
	}

	return slices.DeleteFunc(
		slices.Clone(model.Writable()),
		func(f *meta.Field) bool { return f.Generated },
	)
}

func (t *Table[T]) insertBatch(
	ctx context.Context,
	to exec.Executor,
	model *meta.Model,
	batch []*T,
	columns []*meta.Field,
	conflict *Conflict,
	generated bool,
) (int64, error) {
	stmt := &qb.Insert{
		Table:   model.Table,
		Columns: make([]string, len(columns)),
		Rows:    make([][]qb.Expr, len(batch)),
	}

	for i, f := range columns {
		stmt.Columns[i] = f.Column
	}

	values := newRowBlock(len(batch), len(columns))

	for i, e := range batch {
		stmt.Rows[i] = values.row(i, columns, unsafe.Pointer(e))
	}

	if conflict != nil {
		// The rows of a batch leave the same columns to their defaults, so the
		// first one's values there are every row's.
		clause, err := conflict.clause(
			model,
			t.state.settings.timestamp(),
			unsafe.Pointer(batch[0]),
			nil,
		)
		if err != nil {
			return 0, err
		}

		stmt.Conflict = clause

		return t.state.runOn(ctx, to, stmt)
	}

	if !generated {
		return t.state.runOn(ctx, to, stmt)
	}

	key := model.Key()

	if !t.state.dialect.SupportsReturning() {
		text, args, err := t.state.dialect.Render(stmt)
		if err != nil {
			return 0, err
		}

		result, err := to.ExecContext(ctx, text, args...)
		if err != nil {
			return 0, err
		}

		id, err := result.LastInsertId()
		if err != nil {
			return 0, err
		}

		return 1, assignKey(key, unsafe.Pointer(batch[0]), id)
	}

	stmt.Returning = []qb.Selected{{Value: qb.Column{Name: key.Column}}}

	text, args, err := t.state.dialect.Render(stmt)
	if err != nil {
		return 0, err
	}

	rows, err := to.QueryContext(ctx, text, args...)
	if err != nil {
		return 0, err
	}

	keys, err := collect[int64](rows)
	if err != nil {
		return 0, err
	}

	if len(keys) != len(batch) {
		return 0, fmt.Errorf("ant: insert returned %d keys for %d rows", len(keys), len(batch))
	}
	// One statement draws its sequence values in row order, while RETURNING
	// promises no order, so ascending keys match rows in order.
	slices.SortFunc(keys, func(a, b int64) int { return cmp.Compare(a, b) })

	for i, e := range batch {
		if err := assignKey(key, unsafe.Pointer(e), keys[i]); err != nil {
			return 0, err
		}
	}

	return int64(len(batch)), nil
}

// InsertFrom inserts the rows of a projection, and returns how many. The
// projection's mappings name the columns:
//
//	db.Archive.InsertFrom(ctx, ant.Project[Archive](db.Posts.Where(old),
//		ant.Into(&Archives.Title, &Posts.Title)))
//
// WithConflict skips or updates the rows a unique constraint already holds, as
// in one INSERT ... SELECT ... ON CONFLICT, which no concurrent insert can race
// into a unique violation. Its update overwrites only the columns the mappings
// name, sets UpdatedAt and moves the version on. SQL Server has no such clause,
// so there WithConflict returns ErrUnsupported. BatchSize has no effect, since
// InsertFrom runs one statement.
func (t *Table[T]) InsertFrom(
	ctx context.Context,
	rows *Projection[T],
	options ...InsertOption,
) (int64, error) {
	model, err := t.model()
	if err != nil {
		return 0, err
	}

	var o insertOptions

	for _, option := range options {
		option(&o)
	}

	if o.conflict != nil && o.conflict.err != nil {
		return 0, o.conflict.err
	}

	if err := rows.ready(); err != nil {
		return 0, err
	}

	if len(rows.columns) == 0 {
		return 0, ErrNoColumns
	}

	stmt := &qb.Insert{Table: model.Table, From: rows.statement(rows.columns)}

	for _, c := range rows.columns {
		stmt.Columns = append(stmt.Columns, c.Alias)
	}

	if err := assignedTwice(model, "mappings", stmt.Columns); err != nil {
		return 0, err
	}

	if o.conflict != nil {
		clause, err := o.conflict.clause(model, t.state.settings.timestamp(), nil, stmt.Columns)
		if err != nil {
			return 0, err
		}

		stmt.Conflict = clause
	}

	return t.state.run(ctx, stmt)
}

// FirstOrCreate returns the first row that meets conds, or inserts e and returns
// it. If another writer inserts a matching row first and a unique constraint
// stops this insert, it returns that row. In a transaction, on a database with
// row locks, it reads that row with FOR UPDATE, since the transaction's
// snapshot may not show it, and the row stays locked until the transaction
// ends, ready to change. The bool reports whether e was created.
func (t *Table[T]) FirstOrCreate(ctx context.Context, e *T, conds ...Cond) (*T, bool, error) {
	if e == nil {
		return nil, false, ErrNilEntity
	}

	t = t.in(ctx)

	q := t.query().Where(conds...)

	found, err := q.First(ctx)
	if err == nil {
		return found, false, nil
	}

	if !errors.Is(err, ErrNotFound) {
		return nil, false, err
	}

	created := error(nil)

	if t.state.tx != nil {
		created = t.state.savepoint(ctx, func() error { return t.Insert(ctx, e) })
	} else {
		created = t.Insert(ctx, e)
	}

	if created == nil {
		return e, true, nil
	}

	if !IsUniqueViolation(created) {
		return nil, false, created
	}

	// In a transaction, the row that stopped the insert may have committed after
	// the transaction's snapshot, which a plain read at MySQL's REPEATABLE READ
	// does not see. A locking read sees the newest row. It locks for update, not
	// share: the caller usually changes the row next, and two transactions that
	// each hold a share lock on it deadlock when both try to upgrade it.
	if t.state.tx != nil && dialect.Supports(t.state.dialect, dialect.RowLocks) {
		q = q.ForUpdate()
	}

	// A row that is still not there, as when the conflict was on another
	// unique column, leaves the insert's error. Any other failure of the read
	// is its own, such as a deadlock that a retry can get past.
	found, err = q.First(ctx)
	if errors.Is(err, ErrNotFound) {
		return nil, false, created
	}

	if err != nil {
		return nil, false, err
	}

	return found, false, nil
}

// FirstOrNew returns the first row that meets conds, or e, not saved. The bool
// reports whether a row was found.
func (t *Table[T]) FirstOrNew(ctx context.Context, e *T, conds ...Cond) (*T, bool, error) {
	found, err := t.query().Where(conds...).First(ctx)

	switch {
	case err == nil:
		return found, true, nil
	case errors.Is(err, ErrNotFound):
		return e, false, nil
	}

	return nil, false, err
}

// UpdateMany writes the named columns of each entity to its own row, in one
// transaction, and returns how many rows changed, which on MySQL and MariaDB
// leaves out a row that already held the values, as Update does. Use it for
// values that differ per row. Tracked entities keep their snapshots, so prefer SaveChanges for them.
// As a bulk Update does, it moves the version on, sets UpdatedAt unless a field
// names it, and skips rows the model's filters leave out, such as soft-deleted
// ones. The entities themselves are not changed.
func (t *Table[T]) UpdateMany(
	ctx context.Context,
	entities []*T,
	fields ...FieldRef,
) (int64, error) {
	model, field, err := t.keyed()
	if err != nil {
		return 0, err
	}

	if len(fields) == 0 {
		return 0, fmt.Errorf(
			"%w: name the fields to write, as UpdateMany(ctx, rows, &...Name)",
			ErrNoAssignment,
		)
	}

	columns := make([]string, 0, len(fields))

	for _, ref := range fields {
		if err := ref.fault(); err != nil {
			return 0, err
		}

		if ref.model != model {
			return 0, ErrWrongModel
		}

		columns = append(columns, ref.field.Column)
	}

	if err := managedColumns(model, columns, nil); err != nil {
		return 0, err
	}

	if err := assignedTwice(model, "assignments", columns); err != nil {
		return 0, err
	}

	if slices.Contains(entities, nil) {
		return 0, ErrNilEntity
	}

	var kept []qb.Assignment

	if f := model.Updated; f != nil && !slices.Contains(columns, f.Column) {
		kept = append(kept, qb.Assignment{
			Column: f.Column,
			Value:  qb.Param{Value: timeValue(f, t.state.settings.timestamp()).Interface()},
		})
	}

	if model.Version != nil {
		kept = append(kept, nextVersion(model))
	}

	filters := modelFilters(source{model: model}, nil)

	var total int64

	err = t.state.transact(ctx, len(entities) > 1, func(to exec.Executor) error {
		for _, e := range entities {
			entity := unsafe.Pointer(e)
			stmt := &qb.Update{
				Table: model.Table,
				Where: append(
					keyEquals(model, field, field.Value(entity).Interface()),
					filters...,
				),
			}

			for _, ref := range fields {
				stmt.Set = append(stmt.Set, qb.Assignment{
					Column: ref.field.Column,
					Value:  fieldParam(ref.field, ref.field.Value(entity).Interface()),
				})
			}

			stmt.Set = append(stmt.Set, kept...)

			n, err := t.state.runOn(ctx, to, stmt)
			if err != nil {
				return err
			}

			total += n
		}

		return nil
	})
	if err != nil {
		return 0, err
	}

	return total, nil
}

// Truncate removes every row, faster than Delete, and cannot be used on a table
// that others refer to unless they are empty too. It stops tracking nothing, so
// call ClearTracked if the context holds these rows.
func (t *Table[T]) Truncate(ctx context.Context) error {
	model, err := t.model()
	if err != nil {
		return err
	}

	name := t.state.dialect.Quote(model.Table.Name)

	if model.Table.Schema != "" {
		name = t.state.dialect.Quote(model.Table.Schema) + "." + name
	}

	_, err = t.state.exec.ExecContext(ctx, "TRUNCATE TABLE "+name)

	return err
}

// Clone returns a copy of entity with its key and relations cleared, ready to be
// inserted as a new row.
func Clone[T any](entity *T) *T {
	model := configure[T]()
	out := new(T)
	*out = *entity

	if key := model.Key(); key != nil && key.Generated {
		key.Value(unsafe.Pointer(out)).SetZero()
	}

	for _, r := range model.Relations {
		reflectZero(r, unsafe.Pointer(out))
	}

	return out
}
