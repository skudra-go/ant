// Package track implements the change tracker: identity map, snapshots, and
// diffing. Entities are held by pointer, so the tracker keeps them alive until
// it is cleared.
package track

import (
	"cmp"
	"maps"
	"slices"
	"sync"
	"sync/atomic"
	"unsafe"

	"github.com/skudra-go/ant/internal/meta"
)

// State is the lifecycle state of an entity.
type State int

// Entity states, matching EF Core's.
const (
	Detached State = iota
	Added
	Unchanged
	Modified
	Deleted
)

// String returns the state's name.
func (s State) String() string {
	return [...]string{"Detached", "Added", "Unchanged", "Modified", "Deleted"}[s]
}

// Entry is one tracked entity.
type Entry struct {
	Model  *meta.Model
	Entity unsafe.Pointer
	// State is Added, Unchanged, Modified, or Deleted. Unchanged means nothing
	// has been marked. Changed tells whether the fields differ.
	State    State
	snapshot unsafe.Pointer
	id       identity
	keyed    bool
	seq      uint64
	// columns and links hold the writes to the entry's stored values and to
	// its relations that a transaction may yet take back.
	columns, links history
	// Members holds the entries of a loaded one-to-many relation by key, and
	// Parents the key of a loaded many-to-one relation, to detect changes.
	Members map[*meta.Relation]map[any]*Entry
	Parents map[*meta.Relation]any
}

// Refresh makes the current values the snapshot.
func (e *Entry) Refresh() { e.snapshot = snapshotOf(e.Model, e.Entity) }

// Changed returns the stored fields that differ from the snapshot. The key is
// never included.
func (e *Entry) Changed() []*meta.Field {
	if e.snapshot == nil {
		return nil
	}

	var out []*meta.Field

	for _, f := range e.Model.Updatable() {
		if !equal(f, e.Entity, e.snapshot) {
			out = append(out, f)
		}
	}

	return out
}

// KeyChanged reports whether the primary key differs from the snapshot.
func (e *Entry) KeyChanged() bool {
	if e.snapshot == nil {
		return false
	}

	return slices.ContainsFunc(
		e.Model.Keys,
		func(f *meta.Field) bool { return !equal(f, e.Entity, e.snapshot) },
	)
}

// Holds reports whether the entity at entity is this entry's row. Slices reuse
// memory, so an address alone does not say which row it holds.
func (e *Entry) Holds(entity unsafe.Pointer) bool {
	if !e.keyed {
		return true
	}

	key, ok := e.Model.KeyOf(entity)

	return ok && key == e.id.key
}

// Original returns the snapshot's copy of the entity, or nil if there is none.
func (e *Entry) Original() unsafe.Pointer { return e.snapshot }

// Scratch returns a private copy of the snapshot that a caller may change.
func (e *Entry) Scratch() unsafe.Pointer {
	if e.snapshot == nil {
		return nil
	}

	return snapshotOf(e.Model, e.snapshot)
}

// Saved records a state and snapshot as saved, and returns the change.
func (e *Entry) Saved(state State) Change { return e.saved(state, nil) }

// saved is Saved, with the restore of a rollback passed through wrap.
func (e *Entry) saved(state State, wrap func(func()) func()) Change {
	oldState, oldSnapshot := e.State, e.snapshot
	e.State = state
	e.Refresh()

	return e.columns.record(
		wrapped(wrap, func() {
			e.State, e.snapshot = oldState, oldSnapshot
		}),
		e.stale,
	)
}

// stale is what a rollback does when a write after its own stands: the rows
// may hold neither write's values, so an entry left Unchanged becomes Modified,
// and the next save writes every column, which is right either way.
func (e *Entry) stale() {
	if e.State == Unchanged {
		e.State = Modified
	}
}

func wrapped(wrap func(func()) func(), restore func()) func() {
	if wrap == nil {
		return restore
	}

	return wrap(restore)
}

// SetMembers records the entries a loaded one-to-many relation holds. The maps
// are replaced, never changed, so an undo keeps the ones it saw.
func (e *Entry) SetMembers(r *meta.Relation, members map[any]*Entry) {
	next := maps.Clone(e.Members)
	if next == nil {
		next = map[*meta.Relation]map[any]*Entry{}
	}

	next[r] = members
	e.Members = next
}

// SetParent records the key a loaded many-to-one relation holds.
func (e *Entry) SetParent(r *meta.Relation, key any) {
	next := maps.Clone(e.Parents)
	if next == nil {
		next = map[*meta.Relation]any{}
	}

	next[r] = key
	e.Parents = next
}

// SetLinks records what the loaded relations hold once a save wrote them, and
// returns the change.
//
// A load made since, inside the same transaction or outside it, does not stop a
// rollback: one inside read rows the rollback takes back, and one outside read
// the committed rows, which are what the rollback restores. A save made since
// does, as history says: until it is settled the rollback waits, and if it
// commits, the newer save wrote what it saw, so the rollback takes back only
// what this save changed and the newer one left as it was: a member this save
// added goes, one it dropped comes back, and a member the newer save added
// stays, so that removing it later deletes its row.
func (e *Entry) SetLinks(
	members map[*meta.Relation]map[any]*Entry,
	parents map[*meta.Relation]any,
) Change {
	oldMembers, oldParents := e.Members, e.Parents
	e.Members, e.Parents = members, parents

	return e.links.record(
		func() { e.Members, e.Parents = oldMembers, oldParents },
		func() {
			e.Members = revertMembers(e.Members, members, oldMembers)
			e.Parents = revert(e.Parents, parents, oldParents)
		},
	)
}

// revertMembers takes the change a save made to each relation's members, from
// before to saved, out of what they are now, where nothing changed it since.
func revertMembers(
	now, saved, before map[*meta.Relation]map[any]*Entry,
) map[*meta.Relation]map[any]*Entry {
	out := maps.Clone(now)

	for r, members := range saved {
		if current, ok := now[r]; ok {
			out[r] = revert(current, members, before[r])
		}
	}

	return out
}

// revert takes the change from before to saved out of now: each key the change
// set, if it still holds what the change set, goes back to what it was before,
// and each key the change took out comes back, unless something set it since.
// Maps are replaced, never changed, so a new one is returned.
func revert[K, V comparable](now, saved, before map[K]V) map[K]V {
	out := maps.Clone(now)

	for k, v := range saved {
		old, had := before[k]

		switch current, ok := now[k]; {
		case had && old == v, !ok || current != v:
		case had:
			out[k] = old
		default:
			delete(out, k)
		}
	}

	for k, old := range before {
		if _, kept := saved[k]; kept {
			continue
		}

		if _, set := now[k]; !set {
			if out == nil {
				out = map[K]V{}
			}

			out[k] = old
		}
	}

	return out
}

// Reloaded records an entry as read back from the database over before, a copy
// of the entity as it was, and returns the change, so that a rollback takes the
// read back with the rows it read: the state and snapshot, and each stored
// value the read replaced, unless it was changed since, which stands.
func (e *Entry) Reloaded(before unsafe.Pointer) Change { return e.reloaded(before, nil) }

// reloaded is Reloaded, with the restore of a rollback passed through wrap.
func (e *Entry) reloaded(before unsafe.Pointer, wrap func(func()) func()) Change {
	oldState, oldSnapshot := e.State, e.snapshot
	e.State = Unchanged
	e.Refresh()
	read := e.snapshot

	return e.columns.record(
		wrapped(wrap, func() {
			for _, f := range e.Model.Stored() {
				if equal(f, e.Entity, read) {
					f.Value(e.Entity).Set(f.Value(before))
				}
			}

			e.State, e.snapshot = oldState, oldSnapshot
		}),
		e.stale,
	)
}

// Change is a change a write made to the tracker, which its transaction may
// yet take back. Undo takes it back when the write rolls back, and Keep lets it
// go when the write commits, or was made outside a transaction. One of the two
// is called, once, while the tracker is claimed.
type Change struct {
	undo, keep func()
}

// Undo takes the change back.
func (c Change) Undo() {
	if c.undo != nil {
		c.undo()
	}
}

// Keep lets the change stand.
func (c Change) Keep() {
	if c.keep != nil {
		c.keep()
	}
}

// history holds the writes to one part of an entry that are not settled yet,
// oldest first. A rollback can come late, once another operation has let go of
// the tracker, and in any order, as two transactions can each save the entry
// and roll back. So a write that rolls back puts back what it replaced only once
// no write after it can stand: at once if it is the newest, or as soon as those
// after it roll back too. A write after it that commits is in the rows, and
// then nothing can be put back, so the rollback does what late says instead.
// Kept writes are let go once nothing open is left below them, so the history
// stays as short as the transactions still open.
type history struct{ writes []*write }

type write struct {
	restore, late func()
	undone, kept  bool
}

// record adds a write, which restore takes back and late answers for, and
// returns its change.
func (h *history) record(restore, late func()) Change {
	w := &write{restore: restore, late: late}
	h.writes = append(h.writes, w)

	return Change{
		undo: func() {
			w.undone = true

			h.settle()
		},
		keep: func() {
			w.kept = true

			h.settle()
		},
	}
}

// settle takes back the writes that rolled back with nothing open after them,
// newest first, and lets go of those that nothing can come back to.
func (h *history) settle() {
	for n := len(h.writes); n > 0 && h.writes[n-1].undone; n = len(h.writes) {
		top := h.writes[n-1]
		h.writes = h.writes[:n-1]

		top.restore()
	}

	k := -1

	for i, w := range h.writes {
		if w.kept {
			k = i
		}
	}

	if k < 0 {
		return
	}

	// Below the newest kept write, a rollback can no longer put anything back,
	// and a kept write is done with.
	var open []*write

	for _, w := range h.writes[:k] {
		switch {
		case w.undone:
			w.late()
		case !w.kept:
			open = append(open, w)
		}
	}

	if len(open) > 0 {
		open = append(open, h.writes[k])
	}

	h.writes = append(open, h.writes[k+1:]...)
}

type identity struct {
	model *meta.Model
	key   any
}

// Tracker holds the tracked entities of one context. It is safe for concurrent
// use, but an entity should not change while SaveChanges reads it.
type Tracker struct {
	mu      sync.Mutex
	held    chan struct{} // holds a token while an operation claims the tracker
	owner   atomic.Pointer[Claim]
	undo    []func() // rollback work waiting for the claim, under mu
	seq     uint64
	entries map[unsafe.Pointer]*Entry
	first   map[identity]*Entry
	// deletions counts the deletions of each row whose entries were forgotten,
	// so that a rollback that comes late can tell a newer deletion happened. It
	// holds a row only while a change that counts on it is open.
	deletions map[identity]*deletion
	// OnTrack and OnState report entities starting to be tracked and changing
	// state. They run with the tracker locked, so they must not call back into it.
	OnTrack func(*Entry)
	OnState func(e *Entry, from, to State)
}

// Claim is one operation's hold on a tracker. Operations that load into the
// tracker or save from it write into the entities it holds, so two at once would
// write into the same ones.
type Claim struct {
	_ byte // a zero-size value could share its address with another
}

// TryClaim reserves the tracker for one operation, or returns nil if another
// holds it.
func (t *Tracker) TryClaim() *Claim {
	select {
	case t.held <- struct{}{}:
		return t.own()
	default:
		return nil
	}
}

// own makes a new claim the owner. Rollback work that was queued before it ran
// first, so the operation starts from what the tracker should hold.
func (t *Tracker) own() *Claim {
	c := new(Claim)
	t.owner.Store(c)

	defer func() {
		if recovered := recover(); recovered != nil {
			t.letGo()

			panic(recovered)
		}
	}()

	t.runUndo()

	return c
}

// Owns reports whether c is the claim that holds the tracker now. A claim that
// was released no longer counts, whoever kept it.
func (t *Tracker) Owns(c *Claim) bool { return c != nil && t.owner.Load() == c }

// Release ends c's hold on the tracker, first running any rollback work that
// waited for it. It lets go even if that work panics, so the tracker cannot stay
// held. Releasing it twice does nothing.
func (t *Tracker) Release(c *Claim) {
	for t.Owns(c) {
		func() {
			defer t.letGo()

			t.runUndo()
		}()

		// Work queued after runUndo found the tracker held, so it is run here,
		// unless another operation has the tracker by now and will run it.
		if !t.hasUndo() {
			return
		}

		c = t.TryClaim()
	}
}

func (t *Tracker) letGo() {
	t.owner.Store(nil)
	<-t.held
}

// Settle runs the work of a transaction that ended, rolled back or committed, on
// tracked entries. It needs the claim, but must not wait for one: the operation
// that holds it may be waiting for the rolling-back transaction's connection. So
// if another operation holds the tracker, the work runs when that operation
// releases it.
func (t *Tracker) Settle(f func()) {
	t.mu.Lock()
	t.undo = append(t.undo, f)
	t.mu.Unlock()

	if c := t.TryClaim(); c != nil {
		t.Release(c)
	}
}

func (t *Tracker) hasUndo() bool {
	t.mu.Lock()
	defer t.mu.Unlock()

	return len(t.undo) > 0
}

// runUndo runs the queued rollback work in order. The work calls back into the
// tracker, so it runs without the lock.
func (t *Tracker) runUndo() {
	t.mu.Lock()
	work := t.undo
	t.undo = nil
	t.mu.Unlock()

	for _, f := range work {
		f()
	}
}

// Empty reports whether nothing is tracked.
func (t *Tracker) Empty() bool {
	t.mu.Lock()
	defer t.mu.Unlock()

	return len(t.entries) == 0
}

// New returns an empty tracker.
func New() *Tracker {
	return &Tracker{
		held:      make(chan struct{}, 1),
		entries:   map[unsafe.Pointer]*Entry{},
		first:     map[identity]*Entry{},
		deletions: map[identity]*deletion{},
	}
}

func keyOf(m *meta.Model, entity unsafe.Pointer) (identity, bool) {
	v, ok := m.KeyOf(entity)

	return identity{m, v}, ok
}

func (t *Tracker) add(m *meta.Model, entity unsafe.Pointer, state State) *Entry {
	t.seq++

	e := &Entry{
		Model:  m,
		Entity: entity,
		State:  state,
		seq:    t.seq,
	}

	if state != Added {
		e.Refresh()
	}

	t.entries[entity] = e
	t.identify(e)

	if t.OnTrack != nil {
		t.OnTrack(e)
	}

	return e
}

// identify records the entry's key, if it has one yet. An entity's memory can
// later hold another row, as when a slice shifts, so the key is kept here.
func (t *Tracker) identify(e *Entry) {
	if id, ok := keyOf(e.Model, e.Entity); ok {
		e.id, e.keyed = id, true

		if _, seen := t.first[id]; !seen {
			t.first[id] = e
		}
	}
}

// Load tracks a loaded entity and returns the instance to use: an already
// tracked one with the same key, or entity itself. Models with no key are not
// tracked.
func (t *Tracker) Load(m *meta.Model, entity unsafe.Pointer) unsafe.Pointer {
	t.mu.Lock()
	defer t.mu.Unlock()

	id, ok := keyOf(m, entity)
	if !ok {
		return entity
	}

	if existing, seen := t.first[id]; seen {
		return existing.Entity
	}

	return t.add(m, entity, Unchanged).Entity
}

// Copy tracks a loaded value that lives inside a relation slice. Such a value
// cannot be swapped for an existing instance, so it gets its own entry, and
// changes to either are saved.
func (t *Tracker) Copy(m *meta.Model, entity unsafe.Pointer) *Entry {
	t.mu.Lock()
	defer t.mu.Unlock()

	if len(m.Keys) == 0 {
		return nil
	}

	return t.put(m, entity, Unchanged)
}

// Track starts tracking entity in the given state, or changes its state.
func (t *Tracker) Track(m *meta.Model, entity unsafe.Pointer, state State) *Entry {
	t.mu.Lock()
	defer t.mu.Unlock()

	return t.put(m, entity, state)
}

func (t *Tracker) put(m *meta.Model, entity unsafe.Pointer, state State) *Entry {
	if e, ok := t.entries[entity]; ok {
		if e.State != state && t.OnState != nil {
			t.OnState(e, e.State, state)
		}

		e.State = state

		return e
	}

	return t.add(m, entity, state)
}

// Saved records e as saved in state, and returns the change. A removal asked
// for after the save outlives its rollback: an entry that was new before it is
// forgotten, since its row goes with the rollback, and any other stays Deleted,
// as its row comes back.
func (t *Tracker) Saved(e *Entry, state State) Change {
	return e.saved(state, func(restore func()) func() { return t.keepRemoval(e, restore) })
}

// Reloaded is Entry.Reloaded, keeping a removal asked for after the reload as
// Saved does.
func (t *Tracker) Reloaded(e *Entry, before unsafe.Pointer) Change {
	return e.reloaded(before, func(restore func()) func() { return t.keepRemoval(e, restore) })
}

// keepRemoval wraps a restore so that a removal asked for after the change it
// takes back survives it: an entry that was new before is forgotten, since its row
// goes with the rollback, and any other stays Deleted.
func (t *Tracker) keepRemoval(e *Entry, restore func()) func() {
	return func() {
		removed := e.State == Deleted

		restore()

		switch {
		case !removed:
		case e.State == Added:
			t.Forget(e)
		default:
			e.State = Deleted
		}
	}
}

// Added tracks a written entity, and returns the change, whose rollback stops
// tracking it, unless a write since stands.
func (t *Tracker) Added(m *meta.Model, entity unsafe.Pointer) Change {
	// put reports to OnTrack, which may panic, and must not leave the lock held.
	e := func() *Entry {
		t.mu.Lock()
		defer t.mu.Unlock()

		return t.put(m, entity, Unchanged)
	}()

	return e.columns.record(func() { t.Remove(entity) }, e.stale)
}

// Rekey records the key of an entity that received one on insert.
func (t *Tracker) Rekey(e *Entry) {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.identify(e)
}

// Move records that an entry's entity now lives at a new address, as happens
// when appending to a relation slice reallocates it.
func (t *Tracker) Move(e *Entry, entity unsafe.Pointer) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.entries[e.Entity] == e {
		delete(t.entries, e.Entity)
	}

	e.Entity = entity
	t.entries[entity] = e
}

// Get returns the entry for an entity, or nil.
func (t *Tracker) Get(entity unsafe.Pointer) *Entry {
	t.mu.Lock()
	defer t.mu.Unlock()

	return t.entries[entity]
}

// Find returns the first tracked instance with the given key, or nil.
func (t *Tracker) Find(m *meta.Model, key any) *Entry {
	t.mu.Lock()
	defer t.mu.Unlock()

	return t.first[identity{m, key}]
}

// Remove stops tracking an entity.
func (t *Tracker) Remove(entity unsafe.Pointer) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if e, ok := t.entries[entity]; ok {
		t.forget(e)
	}
}

// Forget stops tracking an entry, which may no longer own its address, and
// returns the change.
func (t *Tracker) Forget(e *Entry) Change {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.forget(e)

	return Change{undo: func() { t.Restore(e) }}
}

// ForgetRow stops tracking every instance of a row that a write deleted, and
// returns how to undo that, as Deleted does.
func (t *Tracker) ForgetRow(m *meta.Model, key any) Change {
	t.mu.Lock()
	defer t.mu.Unlock()

	id := identity{m, key}

	var gone []*Entry

	for _, e := range t.entries {
		if e.keyed && e.id == id {
			gone = append(gone, e)
		}
	}

	for _, e := range gone {
		t.forget(e)
	}

	return t.undoDeletion(id, gone)
}

// Deleted stops tracking an entry whose row a write deleted, and returns the
// change, whose rollback tracks it again, unless the row was deleted again since.
func (t *Tracker) Deleted(e *Entry) Change {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.forget(e)

	if !e.keyed {
		return Change{undo: func() { t.Restore(e) }}
	}

	return t.undoDeletion(e.id, []*Entry{e})
}

// deletion counts the deletions of one row, and the changes still open that
// count on them.
type deletion struct{ count, open int }

// undoDeletion counts a deletion of row id, whose entries gone the tracker
// forgot, and returns the change, whose rollback tracks them again. A rollback
// can come late, once another operation has let go of the tracker, and that
// operation may have deleted the row again, which finds no entries to mark, as
// they are forgotten already. So the deletions of such a row are counted, and a
// rollback that a newer one came after leaves the row untracked, to be read
// afresh. Rollbacks of one transaction run newest first, and each puts the count
// back, so the one before it still applies. The count goes once no change is
// open that could need it.
func (t *Tracker) undoDeletion(id identity, gone []*Entry) Change {
	d := t.deletions[id]

	if d == nil {
		if len(gone) == 0 {
			return Change{}
		}

		d = &deletion{}
		t.deletions[id] = d
	}

	d.count++
	d.open++
	n := d.count

	settled := func() {
		if d.open--; d.open == 0 && t.deletions[id] == d {
			delete(t.deletions, id)
		}
	}

	return Change{
		undo: func() {
			t.mu.Lock()

			late := d.count != n
			if !late {
				d.count = n - 1
			}

			settled()
			t.mu.Unlock()

			if late {
				return
			}

			for _, e := range gone {
				t.Restore(e)
			}
		},
		keep: func() {
			t.mu.Lock()
			defer t.mu.Unlock()

			settled()
		},
	}
}

func (t *Tracker) forget(e *Entry) {
	if t.entries[e.Entity] == e {
		delete(t.entries, e.Entity)
	}

	if e.keyed && t.first[e.id] == e {
		delete(t.first, e.id)
	}
}

// Restore tracks an entry again, undoing Forget, unless another entry has taken
// its address since.
func (t *Tracker) Restore(e *Entry) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if other, ok := t.entries[e.Entity]; ok && other != e {
		return
	}

	t.entries[e.Entity] = e

	if e.keyed {
		if _, seen := t.first[e.id]; !seen {
			t.first[e.id] = e
		}
	}
}

// Clear stops tracking everything.
func (t *Tracker) Clear() {
	t.mu.Lock()
	defer t.mu.Unlock()

	clear(t.entries)
	clear(t.first)
}

// Entries returns every entry, oldest first.
func (t *Tracker) Entries() []*Entry {
	t.mu.Lock()
	defer t.mu.Unlock()

	out := make([]*Entry, 0, len(t.entries))

	for _, e := range t.entries {
		out = append(out, e)
	}

	slices.SortFunc(out, func(a, b *Entry) int { return cmp.Compare(a.seq, b.seq) })

	return out
}

// Len returns the number of tracked entities.
func (t *Tracker) Len() int {
	t.mu.Lock()
	defer t.mu.Unlock()

	return len(t.entries)
}
