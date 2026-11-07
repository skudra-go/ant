package ant

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"time"
	"unsafe"

	"github.com/skudra-go/ant/dialect"
	"github.com/skudra-go/ant/internal/exec"
	"github.com/skudra-go/ant/internal/meta"
	"github.com/skudra-go/ant/internal/track"
	"github.com/skudra-go/ant/qb"
)

// node is one entity in a unit of work. Its entity pointer may be a private copy
// of the snapshot, for a row whose memory no longer belongs to it.
type node struct {
	model   *meta.Model
	entity  unsafe.Pointer
	entry   *track.Entry // nil if the entity is not tracked
	insert  bool
	remove  bool
	forced  bool          // write every field, from MarkModified
	links   []fkLink      // foreign keys to fill before writing
	changed []*meta.Field // fields written by UPDATE

	generated []*meta.Field // filled by the database in this unit
	stamped   []*meta.Field // filled in Go in this unit
	restore   []prior       // values the unit replaced, put back if it fails
}

// prior is a value a unit replaced in an entity, such as the version it moved on.
type prior struct {
	field *meta.Field
	value reflect.Value
}

// copyOf returns a copy of f's value in entity.
func copyOf(f *meta.Field, entity unsafe.Pointer) reflect.Value {
	v := reflect.New(f.Type).Elem()
	v.Set(f.Value(entity))

	return v
}

// undoing returns how a rollback takes back what the unit wrote into n's entity
// once its row is gone: the values the database filled, such as a generated key,
// and those the unit filled, such as timestamps and the version. It is given
// the address the entity has by then, and a value changed since stays. A key
// given back to the database lets the next save insert the entity again. With
// no transaction whose rollback ant would see, there is nothing to take back.
func (u *unit) undoing(n *node) func(entity unsafe.Pointer) {
	if u.s.tx == nil || !u.s.tx.sees() {
		return nil
	}

	filled := slices.Clone(n.restore)

	for _, f := range slices.Concat(n.generated, n.stamped) {
		filled = append(filled, prior{f, reflect.Zero(f.Type)})
	}

	if len(filled) == 0 {
		return nil
	}

	left := make([]any, len(filled))

	for i, p := range filled {
		left[i] = copyOf(p.field, n.entity).Interface()
	}

	return func(entity unsafe.Pointer) {
		for i, p := range filled {
			if v := p.field.Value(entity); reflect.DeepEqual(v.Interface(), left[i]) {
				v.Set(p.value)
			}
		}
	}
}

// fkLink fills a foreign key from a parent just before a node is written.
type fkLink struct {
	parent *node
	field  *meta.Field // on this node
	source *meta.Field // on the parent
}

// unit is one atomic write. It finds what must change, orders it so parents
// exist before their children, and runs it in one transaction.
type unit struct {
	s      *state
	nodes  map[unsafe.Pointer]*node
	order  []*node // in discovery order
	pivots []pivotChange
	// dry plans the unit without changing any entity, for HasChanges.
	dry bool
}

// pivotChange adds or removes one join table row.
type pivotChange struct {
	r      *meta.Relation
	link   meta.Link
	parent *node
	target *node // when adding
	key    any   // the target's key, when removing
	remove bool
}

func (c pivotChange) run(ctx context.Context, u *unit, to exec.Executor) error {
	local, ok := c.link.Local.Key(c.parent.entity)
	if !ok {
		return ErrNoKey
	}

	p := c.r.Pivot

	if c.remove {
		_, err := u.s.runOn(ctx, to, pivotDelete(p, local, []any{c.key}))

		return err
	}

	foreign, ok := c.link.Foreign.Key(c.target.entity)
	if !ok {
		return ErrNoKey
	}

	_, err := u.s.runOn(ctx, to, pivotInsert(p, local, []any{foreign}))

	return err
}

func pivotInsert(p *meta.Pivot, local any, foreign []any) *qb.Insert {
	stmt := &qb.Insert{
		Table:    p.Table,
		Columns:  []string{p.Local, p.Foreign},
		Conflict: &qb.Conflict{},
	}

	for _, key := range foreign {
		stmt.Rows = append(stmt.Rows, []qb.Expr{qb.Param{Value: local}, qb.Param{Value: key}})
	}

	return stmt
}

func pivotDelete(p *meta.Pivot, local any, foreign []any) *qb.Delete {
	values := make([]qb.Expr, len(foreign))

	for i, key := range foreign {
		values[i] = qb.Param{Value: key}
	}

	return &qb.Delete{
		Table: p.Table,
		Where: []qb.Cond{
			qb.Compare{Left: qb.Column{Name: p.Local}, Op: qb.OpEq, Right: qb.Param{Value: local}},
			qb.In{Left: qb.Column{Name: p.Foreign}, Values: values},
		},
	}
}

func newUnit(s *state) *unit {
	return &unit{s: s, nodes: map[unsafe.Pointer]*node{}}
}

func (u *unit) add(model *meta.Model, entity unsafe.Pointer) *node {
	if n, ok := u.nodes[entity]; ok {
		return n
	}

	n := &node{model: model, entity: entity}

	if entry := u.s.tracker.Get(entity); entry != nil && entry.Holds(entity) {
		n.entry = entry
	}

	u.nodes[entity] = n
	u.order = append(u.order, n)

	return n
}

// insert schedules an entity, and with related set, walks its relations.
func (u *unit) insert(model *meta.Model, entity unsafe.Pointer, related bool) error {
	n := u.add(model, entity)
	n.insert = true

	if related {
		return u.walk(n)
	}

	return nil
}

// collect gathers every tracked entity and its pending changes. Relations are
// reconciled first, because slice elements may have moved in memory, and until
// they are matched by key, an entry's memory may hold another row.
func (u *unit) collect() error {
	entries := u.s.tracker.Entries()
	for _, e := range entries {
		n := u.add(e.Model, e.Entity)

		n.entry = e

		switch e.State {
		case track.Added:
			n.insert = true
		case track.Deleted:
			n.remove = true
		case track.Modified:
			n.forced = true
		}
	}

	claimed, err := u.claims()
	if err != nil {
		return err
	}

	for _, n := range slices.Clone(u.order) {
		if u.current(n) && n.entry != nil {
			if err := u.reconcile(n, claimed); err != nil {
				return err
			}
		}
	}

	for _, n := range slices.Clone(u.order) {
		if u.current(n) && !n.remove {
			if err := u.walk(n); err != nil {
				return err
			}
		}
	}

	for _, n := range u.order {
		if n.entry != nil && n.entity == n.entry.Entity && !n.remove && n.entry.KeyChanged() {
			return fmt.Errorf("%w: %s", ErrKeyChanged, n.model.Type.Name())
		}
	}

	return nil
}

// current reports whether n has not been replaced during reconciliation.
func (u *unit) current(n *node) bool { return u.nodes[n.entity] == n }

// claims finds, by key, the entity that each loaded relation holds, so a row
// moved to another parent is not mistaken for one removed.
func (u *unit) claims() (map[rowID]unsafe.Pointer, error) {
	claimed := map[rowID]unsafe.Pointer{}

	for _, n := range u.order {
		if n.entry == nil {
			continue
		}

		for r := range n.entry.Members {
			if r.Pivot != nil {
				continue
			}

			link, err := linkFor(r)
			if err != nil {
				return nil, err
			}

			for _, entity := range r.Related(n.entity) {
				if key, ok := link.Target.KeyOf(entity); ok {
					claimed[rowID{link.Target, key}] = entity
				}
			}
		}
	}

	return claimed, nil
}

// reconcile matches a parent's loaded relations against what they hold now.
func (u *unit) reconcile(n *node, claimed map[rowID]unsafe.Pointer) error {
	for r, members := range n.entry.Members {
		link, err := linkFor(r)
		if err != nil {
			return err
		}

		for _, entity := range r.Related(n.entity) {
			if key, ok := link.Target.KeyOf(entity); ok {
				if entry := members[key]; entry != nil && entry.Entity != entity {
					u.relocate(entry, entity)
				}
			}
		}

		current := map[any]struct{}{}

		for _, entity := range r.Related(n.entity) {
			if key, ok := link.Target.KeyOf(entity); ok {
				current[key] = struct{}{}
			}
		}

		for key, entry := range members {
			if _, kept := current[key]; kept {
				continue
			}

			if r.Pivot != nil {
				u.pivots = append(u.pivots, pivotChange{
					r:      r,
					link:   link,
					parent: n,
					key:    key,
					remove: true,
				})

				continue
			}

			if entity, moved := claimed[rowID{link.Target, key}]; moved {
				u.relocate(entry, entity)

				continue
			}

			u.orphan(link, entry)
		}
	}

	return nil
}

// isNew reports whether a related entity the tracker does not know must be
// inserted rather than linked: it has no key yet, or one the application chose.
func isNew(model *meta.Model, entity unsafe.Pointer) bool {
	key := model.Key()

	return key == nil || !(key.Generated || key.Generator != nil) || key.Value(entity).IsZero()
}

// walk follows a node's relations, scheduling new entities and foreign keys.
func (u *unit) walk(n *node) error {
	for _, r := range n.model.Relations {
		if r.Pivot != nil && r.Pivot.ReadOnly {
			continue
		}

		if r.Empty(n.entity) && !u.tracksRelation(n, r) {
			continue
		}

		link, err := linkFor(r)
		if err != nil {
			return err
		}

		for _, entity := range r.Related(n.entity) {
			if err := u.follow(n, r, link, entity); err != nil {
				return err
			}
		}

		if n.entry != nil && r.Kind == meta.One {
			if _, loaded := n.entry.Parents[r]; loaded && link.BelongsTo && r.Empty(n.entity) &&
				nullable(link.Local) {
				u.unlink(n, link.Local)
			}
		}
	}

	return nil
}

// unlink clears the foreign key of a relation the application emptied. A dry
// unit notes the change instead.
func (u *unit) unlink(n *node, f *meta.Field) {
	switch v := f.Value(n.entity); {
	case v.IsZero():
	case u.dry:
		n.changed = append(n.changed, f)
	default:
		v.SetZero()
	}
}

func (u *unit) tracksRelation(n *node, r *meta.Relation) bool {
	if n.entry == nil {
		return false
	}

	_, members := n.entry.Members[r]
	_, parent := n.entry.Parents[r]

	return members || parent
}

// relocate points an entry at the entity's new address, after a slice moved.
func (u *unit) relocate(entry *track.Entry, entity unsafe.Pointer) {
	u.drop(entry)
	u.s.tracker.Move(entry, entity)
	n := u.add(entry.Model, entity)
	n.entry = entry
}

// drop removes the node an entry had, if it still owns its address.
func (u *unit) drop(entry *track.Entry) {
	if stale := u.nodes[entry.Entity]; stale != nil && stale.entry == entry {
		delete(u.nodes, entry.Entity)
		u.order = slices.DeleteFunc(u.order, func(other *node) bool { return other == stale })
	}
}

// follow schedules one related entity and the foreign key between the two. A
// tracked relation that still holds what was loaded adds no link, so that a
// foreign key changed directly is not overwritten.
func (u *unit) follow(n *node, r *meta.Relation, link meta.Link, entity unsafe.Pointer) error {
	seen := u.nodes[entity] != nil

	other := u.add(link.Target, entity)
	if !seen && other.entry == nil && isNew(link.Target, entity) {
		other.insert = true
	}

	switch {
	case u.unmoved(n, r, link, entity):
	case r.Pivot != nil:
		u.pivots = append(u.pivots, pivotChange{
			r:      r,
			link:   link,
			parent: n,
			target: other,
		})
	case link.BelongsTo:
		n.links = append(n.links, fkLink{other, link.Local, link.Foreign})
		u.morph(n, r)
	default:
		other.links = append(other.links, fkLink{n, link.Foreign, link.Local})
		u.morph(other, r)
	}

	if seen {
		return nil
	}

	return u.walk(other)
}

// morph records which table n's polymorphic foreign key refers to, unless the
// unit is dry; the link it goes with counts as a write either way.
func (u *unit) morph(n *node, r *meta.Relation) {
	if !u.dry {
		n.setMorph(r)
	}
}

// setMorph records which table a polymorphic foreign key refers to.
func (n *node) setMorph(r *meta.Relation) {
	if f := r.MorphField; f != nil && n.model.Has(f) {
		f.Value(n.entity).Set(reflect.ValueOf(r.MorphValue))
	}
}

// unmoved reports whether entity is where the tracker last saw it in r.
func (u *unit) unmoved(n *node, r *meta.Relation, link meta.Link, entity unsafe.Pointer) bool {
	if n.entry == nil {
		return false
	}

	key, ok := link.Target.KeyOf(entity)
	if !ok {
		return false
	}

	if r.Kind == meta.Many {
		_, member := n.entry.Members[r][key]

		return member
	}

	parent, loaded := n.entry.Parents[r]

	return loaded && parent == key
}

// orphan schedules a child removed from its relation, working from a private
// copy of the snapshot, because its slice element may hold another row by now.
// As in EF Core, a required child is deleted and an optional one is unlinked.
func (u *unit) orphan(link meta.Link, entry *track.Entry) {
	u.drop(entry)
	copied := entry.Scratch()
	n := &node{model: link.Target, entity: copied, entry: entry}
	u.nodes[copied] = n
	u.order = append(u.order, n)

	if nullable(link.Foreign) {
		link.Foreign.Value(copied).SetZero()
		n.changed = []*meta.Field{link.Foreign}
		n.forced = true

		return
	}

	n.remove = true
}

// nullable reports whether a field can hold NULL.
func nullable(f *meta.Field) bool {
	return f.Type.Kind() == reflect.Pointer || f.Type.Implements(valuerType)
}

var valuerType = reflect.TypeFor[driver.Valuer]()

// sorted returns the nodes with every new parent before its children. Only new
// parents matter: an existing row can be referred to in any order, and a cycle
// among existing rows is not a problem.
func (u *unit) sorted() ([]*node, error) {
	const (
		visiting = 1
		done     = 2
	)

	marks := make(map[*node]int, len(u.order))
	out := make([]*node, 0, len(u.order))

	var visit func(*node) error

	visit = func(n *node) error {
		switch marks[n] {
		case done:
			return nil
		case visiting:
			return ErrCycle
		}

		marks[n] = visiting

		for _, l := range n.links {
			if l.parent.insert {
				if err := visit(l.parent); err != nil {
					return err
				}
			}
		}

		marks[n] = done
		out = append(out, n)

		return nil
	}

	for _, n := range u.order {
		if !n.remove {
			if err := visit(n); err != nil {
				return nil, err
			}
		}
	}

	return out, nil
}

type rowID struct {
	model *meta.Model
	key   any
}

// removals returns the rows to delete, once each, with children first.
func (u *unit) removals() []*node {
	byKey := map[rowID]*node{}

	var out []*node

	for _, n := range u.order {
		if !n.remove {
			continue
		}

		key, ok := n.model.KeyOf(n.entity)
		if !ok {
			continue
		}

		id := rowID{n.model, key}
		if byKey[id] == nil {
			byKey[id] = n
			out = append(out, n)
		}
	}

	if len(out) < 2 {
		return out
	}

	children := map[*node][]*node{}

	for _, n := range out {
		for _, r := range n.model.Relations {
			link, err := linkFor(r)
			if err != nil || !link.BelongsTo {
				continue
			}

			if key, ok := link.Local.Key(n.entity); ok {
				if parent := byKey[rowID{link.Target, key}]; parent != nil && parent != n {
					children[parent] = append(children[parent], n)
				}
			}
		}
	}

	sorted := make([]*node, 0, len(out))
	seen := map[*node]bool{}

	var visit func(*node)

	visit = func(n *node) {
		if seen[n] {
			return
		}

		seen[n] = true

		for _, child := range children[n] {
			visit(child)
		}

		sorted = append(sorted, n)
	}

	for _, n := range out {
		visit(n)
	}

	return sorted
}

// assignForeignKeys copies parent keys into n, and returns the fields it changed.
func (n *node) assignForeignKeys() ([]*meta.Field, error) {
	var changed []*meta.Field

	for _, l := range n.links {
		key, ok := l.source.Key(l.parent.entity)
		if !ok {
			continue
		}

		if current, set := l.field.Key(n.entity); set && current == key {
			continue
		}

		if err := sql.ConvertAssign(driver.ScanContext{}, l.field.Addr(n.entity), key); err != nil {
			return nil, fmt.Errorf("ant: set foreign key %s: %w", l.field.Name, err)
		}

		changed = append(changed, l.field)
	}

	return changed, nil
}

// pending returns the fields to UPDATE. An untracked node writes only the foreign
// keys that were just assigned.
func (n *node) pending(assigned []*meta.Field) []*meta.Field {
	switch {
	case n.changed != nil:
		return n.changed
	case n.entry == nil:
		return assigned
	case n.forced:
		return n.model.Updatable()
	}

	return n.entry.Changed()
}

// writes estimates the statements a unit needs, to avoid a transaction when
// there is nothing to protect.
func (u *unit) writes() int {
	count := len(u.pivots)

	for _, n := range u.order {
		if n.insert || n.remove || n.forced || len(n.links) > 0 || n.changed != nil ||
			n.entry != nil && n.entity == n.entry.Entity && len(n.entry.Changed()) > 0 {
			count++
		}
	}

	return count
}

// writesAnything reports whether running the unit would write a row, which
// HasChanges asks. writes counts each node with a foreign key to set, which
// suits choosing a transaction; here a key that already holds its parent's does
// not count, since assignForeignKeys leaves it and nothing is written.
func (u *unit) writesAnything() bool {
	if len(u.pivots) > 0 {
		return true
	}

	for _, n := range u.order {
		if n.insert || n.remove || n.forced || n.changed != nil ||
			n.entry != nil && n.entity == n.entry.Entity && len(n.entry.Changed()) > 0 {
			return true
		}

		for _, l := range n.links {
			key, ok := l.source.Key(l.parent.entity)
			if current, set := l.field.Key(n.entity); ok && (!set || current != key) {
				return true
			}
		}
	}

	return false
}

// run writes the unit in one transaction and updates the tracker.
func (u *unit) run(ctx context.Context) error {
	order, err := u.sorted()
	if err != nil {
		return err
	}

	removals := u.removals()

	count := u.writes()
	if count == 0 {
		return nil
	}

	if err := u.saving(ctx); err != nil {
		return err
	}

	if f := u.s.settings.events.Saving; f != nil {
		f(ctx, u.changes())
	}

	u.s.wrote()

	err = u.s.transact(ctx, count > 1 || u.hooked(), func(to exec.Executor) error {
		for i := 0; i < len(order); {
			if batch := u.insertBatch(ctx, order[i:]); len(batch) > 1 {
				if err := u.createMany(ctx, to, batch); err != nil {
					return err
				}

				i += len(batch)

				continue
			}

			n := order[i]
			i++

			assigned, err := n.assignForeignKeys()
			if err != nil {
				return err
			}

			if err := u.write(ctx, to, n, assigned); err != nil {
				return err
			}
		}

		for _, change := range u.pivots {
			if err := change.run(ctx, u, to); err != nil {
				return err
			}
		}

		for _, n := range removals {
			if err := n.beforeDelete(ctx); err != nil {
				return err
			}

			if err := u.delete(ctx, to, n); err != nil {
				return err
			}

			if err := n.afterDelete(ctx); err != nil {
				return err
			}
		}

		return u.touch(ctx, to)
	})
	if err != nil {
		for _, n := range u.order {
			for _, f := range slices.Concat(n.generated, n.stamped) {
				f.Value(n.entity).SetZero()
			}

			n.generated, n.stamped = nil, nil

			for _, p := range n.restore {
				p.field.Value(n.entity).Set(p.value)
			}

			n.restore = nil
		}

		if f := u.s.settings.events.SaveFailed; f != nil {
			f(ctx, err)
		}

		return err
	}

	u.commit()
	u.notify(ctx)

	if f := u.s.settings.events.Saved; f != nil {
		f(ctx, u.changes())
	}

	return nil
}

// touch updates the parents of written children that ask for it, once each.
func (u *unit) touch(ctx context.Context, to exec.Executor) error {
	touched := map[rowID]bool{}

	for _, n := range u.order {
		if !n.insert && !n.remove && len(n.changed) == 0 {
			continue
		}

		for _, r := range n.model.Relations {
			if !r.Touch {
				continue
			}

			link, err := linkFor(r)
			if err != nil {
				return err
			}

			key, ok := link.Local.Key(n.entity)

			updated := link.Target.Updated
			if !ok || updated == nil || !link.BelongsTo || touched[rowID{link.Target, key}] {
				continue
			}

			touched[rowID{link.Target, key}] = true

			now := timeValue(updated, u.s.settings.timestamp()).Interface()
			if _, err := u.s.runOn(ctx, to, &qb.Update{
				Table: link.Target.Table,
				Set:   []qb.Assignment{{Column: updated.Column, Value: qb.Param{Value: now}}},
				Where: []qb.Cond{
					qb.Compare{
						Left:  link.Target.Col(link.Foreign),
						Op:    qb.OpEq,
						Right: qb.Param{Value: key},
					},
				},
			}); err != nil {
				return err
			}
		}
	}

	return nil
}

// hooked reports whether a hook will run, which a transaction must cover, since
// an After hook's error has to undo the write.
func (u *unit) hooked() bool {
	return slices.ContainsFunc(u.order, func(n *node) bool {
		h := hooksOf(n.model)

		return h.afterInsert || h.afterUpdate || h.afterDelete
	})
}

func (u *unit) write(ctx context.Context, to exec.Executor, n *node, assigned []*meta.Field) error {
	if n.insert {
		if err := n.beforeInsert(ctx); err != nil {
			return err
		}

		if err := u.create(ctx, to, n); err != nil {
			return err
		}

		return n.afterInsert(ctx)
	}

	if len(n.pending(assigned)) == 0 {
		return nil
	}

	if err := n.beforeUpdate(ctx); err != nil {
		return err
	}

	n.changed = n.pending(assigned)

	if len(n.changed) == 0 {
		return nil
	}

	if err := u.update(ctx, to, n); err != nil {
		return err
	}

	return n.afterUpdate(ctx)
}

// setTime stores a time in a timestamp field of either kind.
func setTime(f *meta.Field, entity unsafe.Pointer, now time.Time) {
	f.Value(entity).Set(timeValue(f, now))
}

func timeValue(f *meta.Field, now time.Time) reflect.Value {
	if f.Type == reflect.TypeFor[UnixTime]() {
		return reflect.ValueOf(UnixTime{Time: now})
	}

	return reflect.ValueOf(now)
}

// stamp fills automatic values before an insert.
func (u *unit) stamp(n *node) {
	if v := n.model.Version; v != nil && v.Value(n.entity).IsZero() {
		v.Value(n.entity).SetInt(1)
		n.stamped = append(n.stamped, v)
	}

	now := u.s.settings.timestamp()

	for _, f := range []*meta.Field{n.model.Created, n.model.Updated} {
		if f != nil && f.Value(n.entity).IsZero() {
			setTime(f, n.entity, now)
			n.stamped = append(n.stamped, f)
		}
	}

	if d := n.model.Discriminator; d != nil && d.Value(n.entity).IsZero() {
		d.Value(n.entity).Set(reflect.ValueOf(n.model.DiscriminatorValue))
		n.stamped = append(n.stamped, d)
	}

	for _, f := range n.model.Keys {
		if f.Generator != nil && f.Value(n.entity).IsZero() {
			f.Generator(n.entity)
			n.stamped = append(n.stamped, f)
		}
	}
}

func (u *unit) create(ctx context.Context, to exec.Executor, n *node) error {
	if n.model.ReadOnly {
		return fmt.Errorf("%w: %s", ErrReadOnly, n.model.Type.Name())
	}

	u.stamp(n)
	stmt := &qb.Insert{Table: n.model.Table}

	var (
		row       []qb.Expr
		returning []*meta.Field
	)

	for _, f := range n.model.Writable() {
		if f.FromDatabase() && f.Value(n.entity).IsZero() {
			returning = append(returning, f)
			stmt.Returning = append(stmt.Returning, qb.Selected{Value: qb.Column{Name: f.Column}})

			continue
		}

		stmt.Columns = append(stmt.Columns, f.Column)
		row = append(row, fieldParam(f, f.Value(n.entity).Interface()))
	}

	for _, f := range n.model.Fields {
		if f.ReadOnly && !f.Computed && !f.WriteOnly {
			returning = append(returning, f)
			stmt.Returning = append(stmt.Returning, qb.Selected{Value: qb.Column{Name: f.Column}})
		}
	}

	if len(row) > 0 {
		stmt.Rows = [][]qb.Expr{row}
	}

	if len(returning) > 0 && !u.s.dialect.SupportsReturning() {
		stmt.Returning = nil

		return u.createThenRead(ctx, to, n, stmt, returning)
	}

	text, args, err := u.s.dialect.Render(stmt)
	if err != nil {
		return err
	}

	if len(returning) == 0 {
		_, err := to.ExecContext(ctx, text, args...)

		return err
	}

	rows, err := to.QueryContext(ctx, text, args...)
	if err != nil {
		return err
	}
	defer rows.Close()

	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return err
		}

		return errors.New("ant: insert returned no row")
	}

	dest := make([]any, len(returning))

	for i, f := range returning {
		dest[i] = f.Addr(n.entity)
	}

	if err := rows.Scan(dest...); err != nil {
		return err
	}

	n.generated = returning

	return rows.Close()
}

// createThenRead inserts on a database with no RETURNING: the key comes from
// LastInsertId, and anything else the database filled is read back by that key.
func (u *unit) createThenRead(
	ctx context.Context,
	to exec.Executor,
	n *node,
	stmt *qb.Insert,
	returning []*meta.Field,
) error {
	text, args, err := u.s.dialect.Render(stmt)
	if err != nil {
		return err
	}

	result, err := to.ExecContext(ctx, text, args...)
	if err != nil {
		return err
	}

	var (
		rest []qb.Selected
		dest []any
	)

	for _, f := range returning {
		if f == n.model.Key() && f.Generated {
			id, err := result.LastInsertId()
			if err != nil {
				return err
			}

			if err := assignKey(f, n.entity, id); err != nil {
				return err
			}

			continue
		}

		rest = append(rest, qb.Selected{Value: qb.Column{Name: f.Column}})
		dest = append(dest, f.Addr(n.entity))
	}

	n.generated = returning

	if len(rest) == 0 {
		return nil
	}

	read := &qb.Select{
		Columns: rest,
		From:    qb.Source{Table: n.model.Table},
		Where:   keyConditions(n.model, n.entity),
	}

	text, args, err = u.s.dialect.Render(read)
	if err != nil {
		return err
	}

	rows, err := to.QueryContext(ctx, text, args...)
	if err != nil {
		return err
	}
	defer rows.Close()

	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return err
		}

		return errors.New("ant: inserted row was not found")
	}

	if err := rows.Scan(dest...); err != nil {
		return err
	}

	return rows.Close()
}

// assignKey sets a generated key from the int64 a driver reports.
func assignKey(key *meta.Field, entity unsafe.Pointer, id int64) error {
	return sql.ConvertAssign(driver.ScanContext{}, key.Addr(entity), id)
}

// insertBatchLimit caps the rows in one INSERT, well under the parameter limit
// for any sensible table.
const insertBatchLimit = 200

// insertBatch returns the nodes at the start of order that can share one INSERT:
// new rows of one model, none depending on another in the batch, with no hook that
// needs each row written by itself, and with the same columns. Only a generated
// key may come back, because keys are matched to rows by sorting them.
func (u *unit) insertBatch(ctx context.Context, order []*node) []*node {
	first := order[0]
	hooks := hooksOf(first.model)
	// Without a transaction, rows before a failure must stay, so each goes alone.
	if !first.insert || first.model.ReadOnly || !u.s.settings.autoTx {
		return nil
	}

	if ctx.Value(skipHooks{}) == nil &&
		(hooks.validate || hooks.beforeInsert || hooks.afterInsert) {
		return nil
	}

	for _, f := range first.model.Fields {
		if (f.ReadOnly && !f.Computed && !f.WriteOnly) || f.Default {
			return nil
		}
	}

	if len(first.model.Keys) == 0 {
		return nil
	}

	key := first.model.Key() // nil for a composite key, which is never generated

	generated := key != nil && key.Generated && key.Value(first.entity).IsZero()

	if slices.ContainsFunc(first.model.Relations, func(r *meta.Relation) bool { return r.Touch }) {
		return nil
	}

	if generated && !u.s.dialect.SupportsReturning() {
		return nil // LastInsertId reports one key, so each row goes alone
	}

	limit := min(
		insertBatchLimit,
		dialect.SyntaxOf(u.s.dialect).ParameterLimit()/max(len(first.model.Writable()), 1),
	)
	batch := make([]*node, 0, min(len(order), limit))
	members := map[*node]bool{}

	for _, n := range order {
		if !n.insert || n.model != first.model || len(batch) == limit ||
			(key != nil && key.Generated && key.Value(n.entity).IsZero()) != generated {
			break
		}

		if slices.ContainsFunc(n.links, func(l fkLink) bool { return members[l.parent] }) {
			break
		}

		batch = append(batch, n)
		members[n] = true
	}

	return batch
}

// createMany inserts a batch in one statement.
func (u *unit) createMany(ctx context.Context, to exec.Executor, batch []*node) error {
	model := batch[0].model
	key := model.Key()
	generated := key != nil && key.Generated && key.Value(batch[0].entity).IsZero()
	columns := insertColumns(model, generated)
	stmt := &qb.Insert{Table: model.Table}

	for _, f := range columns {
		stmt.Columns = append(stmt.Columns, f.Column)
	}

	for _, n := range batch {
		if _, err := n.assignForeignKeys(); err != nil {
			return err
		}

		if err := n.checkRules(ctx); err != nil {
			return err
		}

		u.stamp(n)

		row := make([]qb.Expr, len(columns))

		for i, f := range columns {
			row[i] = fieldParam(f, f.Value(n.entity).Interface())
		}

		stmt.Rows = append(stmt.Rows, row)
	}

	if !generated {
		_, err := u.s.runOn(ctx, to, stmt)

		return err
	}

	stmt.Returning = []qb.Selected{{Value: qb.Column{Name: key.Column}}}

	text, args, err := u.s.dialect.Render(stmt)
	if err != nil {
		return err
	}

	rows, err := to.QueryContext(ctx, text, args...)
	if err != nil {
		return err
	}

	keys, err := collect[int64](rows)
	if err != nil {
		return err
	}

	if len(keys) != len(batch) {
		return fmt.Errorf("ant: insert returned %d keys for %d rows", len(keys), len(batch))
	}
	// As in InsertMany, ascending keys match rows in order.
	slices.Sort(keys)

	for i, n := range batch {
		if err := sql.ConvertAssign(driver.ScanContext{}, key.Addr(n.entity), keys[i]); err != nil {
			return err
		}

		n.generated = []*meta.Field{key}
	}

	return nil
}

// keyCondition finds the node's row by the key it was loaded with.
func (n *node) keyCondition() ([]qb.Cond, error) {
	if len(n.model.Keys) == 0 {
		return nil, ErrNoKey
	}

	source := n.entity

	if n.entry != nil && n.entry.Original() != nil {
		source = n.entry.Original()
	}

	return keyConditions(n.model, source), nil
}

// keyConditions matches the primary key of the model value at entity.
func keyConditions(model *meta.Model, entity unsafe.Pointer) []qb.Cond {
	out := make([]qb.Cond, len(model.Keys))

	for i, f := range model.Keys {
		out[i] = keyEquals(model, f, f.Value(entity).Interface())[0]
	}

	return out
}

func (u *unit) update(ctx context.Context, to exec.Executor, n *node) error {
	where, err := n.keyCondition()
	if err != nil {
		return err
	}

	if n.model.ReadOnly {
		return fmt.Errorf("%w: %s", ErrReadOnly, n.model.Type.Name())
	}

	if updated := n.model.Updated; updated != nil && !slices.Contains(n.changed, updated) {
		if entity := n.entity; n.entry != nil && entity == n.entry.Entity {
			n.restore = append(n.restore, prior{updated, copyOf(updated, entity)})
			setTime(updated, entity, u.s.settings.timestamp())

			n.changed = append(n.changed, updated)
		}
	}

	where = append(where, n.tokenConditions()...)
	u.bumpVersion(n)

	stmt := &qb.Update{Table: n.model.Table, Where: where}

	for _, f := range n.changed {
		stmt.Set = append(
			stmt.Set,
			qb.Assignment{Column: f.Column, Value: fieldParam(f, f.Value(n.entity).Interface())},
		)
	}

	return u.writeOne(ctx, to, n, stmt)
}

// tokenConditions requires the row to still hold the values this entity loaded.
func (n *node) tokenConditions() []qb.Cond {
	if n.entry == nil || n.entry.Original() == nil {
		return nil
	}

	var out []qb.Cond

	for _, f := range n.model.Tokens() {
		value := f.Value(n.entry.Original()).Interface()
		if isNull(value) {
			out = append(out, qb.IsNull{Left: n.model.Col(f)})
		} else {
			out = append(
				out,
				qb.Compare{Left: n.model.Col(f), Op: qb.OpEq, Right: qb.Param{Value: value}},
			)
		}
	}

	return out
}

// bumpVersion moves the version field on, restoring it if the unit fails.
func (u *unit) bumpVersion(n *node) {
	v := n.model.Version
	if v == nil || n.entry == nil || n.entity != n.entry.Entity {
		return
	}

	n.restore = append(n.restore, prior{v, copyOf(v, n.entity)})

	value := v.Value(n.entity)
	value.SetInt(value.Int() + 1)

	if !slices.Contains(n.changed, v) {
		n.changed = append(n.changed, v)
	}
}

func (u *unit) delete(ctx context.Context, to exec.Executor, n *node) error {
	where, err := n.keyCondition()
	if err != nil {
		return err
	}

	if n.model.ReadOnly {
		return fmt.Errorf("%w: %s", ErrReadOnly, n.model.Type.Name())
	}

	where = append(where, n.tokenConditions()...)

	if soft := n.model.SoftDelete; soft != nil {
		deleted := meta.DeletedAt{
			Null: sql.Null[time.Time]{V: u.s.settings.timestamp(), Valid: true},
		}

		return u.writeOne(ctx, to, n, &qb.Update{
			Table: n.model.Table,
			Where: where,
			Set:   []qb.Assignment{{Column: soft.Column, Value: qb.Param{Value: deleted}}},
		})
	}

	return u.writeOne(ctx, to, n, &qb.Delete{Table: n.model.Table, Where: where})
}

// writeOne runs a statement that must affect exactly one row.
func (u *unit) writeOne(ctx context.Context, to exec.Executor, n *node, stmt qb.Statement) error {
	affected, err := u.s.runOn(ctx, to, stmt)
	if err != nil {
		return err
	}

	if affected == 0 {
		key, _ := n.model.KeyOf(n.entity)

		return &ConcurrencyError{Model: n.model.Type.Name(), Key: key}
	}

	return nil
}

// commit brings the tracker up to date after a successful write. Rows that left
// go first, because a new entity may have taken over a removed one's address.
func (u *unit) commit() {
	tracker := u.s.tracker

	var undo []track.Change

	for _, n := range u.order {
		switch entry := n.entry; {
		case entry == nil:
		case n.remove:
			undo = append(undo, tracker.Deleted(entry))
		case n.entity != entry.Entity:
			undo = append(undo, tracker.Forget(entry))
		}
	}

	for _, n := range u.order {
		wrote := n.insert || len(n.changed) > 0

		entry := n.entry

		switch {
		case n.remove || entry != nil && n.entity != entry.Entity:
		case entry != nil && wrote:
			undo = append(undo, tracker.Saved(entry, track.Unchanged, u.undoing(n)))

			if n.insert {
				tracker.Rekey(entry)
			}
		case entry == nil && wrote && u.s.settings.tracking && len(n.model.Keys) > 0:
			undo = append(undo, tracker.Added(n.model, n.entity, u.undoing(n)))
		}
	}

	for _, n := range u.order {
		if !n.remove && n.entry != nil {
			undo = append(undo, u.remember(n))
		}
	}

	u.s.record(undo...)
}

// remember records what a node's loaded relations hold now.
func (u *unit) remember(n *node) track.Change {
	entry := n.entry
	oldMembers, oldParents := entry.Members, entry.Parents

	members := make(map[*meta.Relation]map[any]*track.Entry, len(oldMembers))

	for r := range oldMembers {
		link, err := linkFor(r)
		if err != nil {
			continue
		}

		current := map[any]*track.Entry{}

		for _, entity := range r.Related(n.entity) {
			if key, ok := link.Target.KeyOf(entity); ok {
				current[key] = u.s.tracker.Get(entity)
			}
		}

		members[r] = current
	}

	parents := make(map[*meta.Relation]any, len(oldParents))

	for r := range oldParents {
		link, err := linkFor(r)
		if err != nil {
			continue
		}

		for _, entity := range r.Related(n.entity) {
			if key, ok := link.Target.KeyOf(entity); ok {
				parents[r] = key
			}
		}
	}

	return entry.SetLinks(members, parents)
}

// SaveChanges writes every change to tracked entities in one transaction:
// inserts and updates with parents first, then deletes with children first. It
// updates only the columns that changed. If anything fails, nothing is saved
// and the tracker keeps its pending changes, so the call can be retried.
func (c *Context) SaveChanges(ctx context.Context) error {
	s := c.state
	if s == nil {
		return ErrNotOpen
	}

	ctx, release, err := s.claim(ctx, true)
	if err != nil {
		return err
	}

	defer release()

	s.saving.Lock()
	defer s.saving.Unlock()

	attempts := max(s.settings.retry.Attempts, 1)

	if s.tx != nil {
		attempts = 1 // the transaction around this decides
	}

	for attempt := 1; ; attempt++ {
		u := newUnit(s)
		if err := u.collect(); err != nil {
			return err
		}

		err := u.run(ctx)
		if err == nil || attempt >= attempts || !IsRetryable(err) {
			return err
		}

		if err := backoff(ctx, attempt); err != nil {
			return err
		}
	}
}
