// Package track implements the change tracker: identity map, snapshots, and
// diffing. Entities are held by pointer, so the tracker keeps them alive until
// it is cleared.
package track

import (
	"cmp"
	"maps"
	"reflect"
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
	gone     bool // let go of, as by Detach, until tracked again
	listed   bool // in the tracker's order
	// element says the entity lives in a relation slice, as Copy tracks it, so
	// that an append can move it with the slice.
	element bool
	seq     uint64
	// pending holds the writes a transaction may yet take back. Most entries
	// are only ever read, so it is made when the first write is recorded.
	pending *histories
	// Members holds the entries of a loaded one-to-many relation by key, and
	// Parents the key of a loaded many-to-one relation, to detect changes.
	Members map[*meta.Relation]map[any]*Entry
	Parents map[*meta.Relation]any
}

// histories holds the writes to an entry's stored values, in columns, and to
// its relations, in links, that a transaction may yet take back.
type histories struct {
	columns, links history
}

// writes returns the entry's histories, making them on first use.
func (e *Entry) writes() *histories {
	if e.pending == nil {
		e.pending = new(histories)
	}

	return e.pending
}

// Element reports whether the entity was tracked where it lives in a relation
// slice, which an append can move.
func (e *Entry) Element() bool { return e.element }

// Unsettled reports whether a write to the entry's relations may yet be taken
// back.
func (e *Entry) Unsettled() bool { return e.pending != nil && len(e.pending.links.writes) > 0 }

// Refresh makes the current values the snapshot.
func (e *Entry) Refresh() { e.snapshot = snapshotOf(e.Model, e.Entity) }

// Copy holds the stored values of an entity as they were when it was taken.
type Copy struct{ snapshot unsafe.Pointer }

// CopyOf copies the stored values of the model value at entity.
func CopyOf(m *meta.Model, entity unsafe.Pointer) Copy { return Copy{snapshotOf(m, entity)} }

// Taken reports whether c holds a copy.
func (c Copy) Taken() bool { return c.snapshot != nil }

// Stored makes c the snapshot: the values the row holds, where the entity may
// hold others since, as an After hook's edit after the write.
func (e *Entry) Stored(c Copy) {
	if c.snapshot != nil {
		e.snapshot = c.snapshot
	}
}

// Changed returns the stored fields that differ from the snapshot. The key is
// never included.
func (e *Entry) Changed() []*meta.Field {
	if e.snapshot == nil {
		return nil
	}

	var out []*meta.Field

	for _, f := range e.Model.Updatable() {
		if !unchanged(e.Model, f, e.Entity, e.snapshot) {
			out = append(out, f)
		}
	}

	return out
}

// Dirty reports whether Changed would return any field.
func (e *Entry) Dirty() bool {
	if e.snapshot == nil {
		return false
	}

	for _, f := range e.Model.Updatable() {
		if !unchanged(e.Model, f, e.Entity, e.snapshot) {
			return true
		}
	}

	return false
}

// Matches reports whether row holds what the snapshot holds in each of fields:
// whether the row, read again, is as the entry loaded it.
func (e *Entry) Matches(row unsafe.Pointer, fields []*meta.Field) bool {
	if e.snapshot == nil {
		return false
	}

	for _, f := range fields {
		if !unchanged(e.Model, f, row, e.snapshot) {
			return false
		}
	}

	return true
}

// KeyChanged reports whether the primary key differs from the snapshot.
func (e *Entry) KeyChanged() bool {
	if e.snapshot == nil {
		return false
	}

	for _, f := range e.Model.Keys {
		if !unchanged(e.Model, f, e.Entity, e.snapshot) {
			return true
		}
	}

	return false
}

// Holds reports whether the entity at entity is this entry's row. Slices reuse
// memory, so an address alone does not say which row it holds.
func (e *Entry) Holds(entity unsafe.Pointer) bool {
	if !e.keyed {
		return true
	}

	id, ok := keyOf(e.Model, entity)

	return ok && id == e.id
}

// HasOriginal reports whether the entry holds the values its row was loaded or
// last saved with.
func (e *Entry) HasOriginal() bool { return e.snapshot != nil }

// OriginalValue returns f's value as it was loaded or last saved, or the zero
// Value if there is none, or f is computed.
func (e *Entry) OriginalValue(f *meta.Field) reflect.Value {
	if e.snapshot == nil {
		return reflect.Value{}
	}

	return e.Model.SnapshotLayout().Value(e.snapshot, f)
}

// OriginalKey returns the key the row was loaded or last saved with, as KeyOf
// gives it, or false if there is none.
func (e *Entry) OriginalKey() (any, bool) {
	if e.snapshot == nil {
		return nil, false
	}

	return e.Model.SnapshotLayout().Key(e.Model, e.snapshot)
}

// Scratch returns a private copy of the entity as it was loaded or last saved,
// which a caller may change, or nil. Relations and computed fields are zero.
func (e *Entry) Scratch() unsafe.Pointer {
	if e.snapshot == nil {
		return nil
	}

	return entityOf(e.Model, e.snapshot)
}

// Current returns a private copy of the entity as it is now, which a caller may
// change. Relations are left out, as in a snapshot.
func (e *Entry) Current() unsafe.Pointer { return copyOf(e.Model, e.Entity) }

// Saved records a state and snapshot as saved, and returns the change.
func (e *Entry) Saved(state State) Change { return e.saved(state, nil) }

// saved is Saved, with the restore of a rollback passed through wrap.
func (e *Entry) saved(state State, wrap func(func()) func()) Change {
	oldState, oldSnapshot := e.State, e.snapshot
	e.State = state
	e.Refresh()

	return e.writes().columns.record(
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

	return e.writes().links.record(
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

	return e.writes().columns.record(
		wrapped(wrap, func() {
			for _, f := range e.Model.Stored() {
				if unchanged(e.Model, f, e.Entity, read) {
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

// IsZero reports whether the change is of nothing, with nothing to take back or
// to keep.
func (c Change) IsZero() bool { return c.undo == nil && c.keep == nil }

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
// and roll back. So writes are settled newest first, each once every write
// after it has settled, which is the order a single transaction undoes them in:
// one that rolls back with nothing kept after it puts back what it replaced, and
// one that rolls back under a kept write, which is in the rows, does what late
// says instead. Until then a settled write waits, and the result does not
// depend on the order the transactions end in. The history holds no more than
// the writes of transactions still open, and those below them.
type history struct{ writes []*write }

type write struct {
	restore, late func()
	undone, kept  bool
	// overtaken means a write after this one was kept, so it rolls back late.
	overtaken bool
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

// settle takes the settled writes off the top, newest first, until it reaches
// one still open.
func (h *history) settle() {
	for n := len(h.writes); n > 0; n = len(h.writes) {
		top := h.writes[n-1]
		if !top.undone && !top.kept {
			return
		}

		// The slot is cleared, so the array does not keep the write alive.
		h.writes[n-1] = nil
		h.writes = h.writes[:n-1]

		switch {
		case top.kept:
		case top.overtaken:
			top.late()
		default:
			top.restore()
		}

		if n > 1 && (top.kept || top.overtaken) {
			h.writes[n-2].overtaken = true
		}
	}

	h.writes = nil
}

// identity is a row: its model and its key. An integer key, which most are, is
// held as n with key nil, so that finding or tracking a row by it puts nothing on
// the heap; any other key is in key. Every identity is made by identityOf or
// keyOf, so two identities of one row are equal.
type identity struct {
	model *meta.Model
	n     int64
	key   any
}

// identityOf returns the identity of the row of m with key, in the form
// meta.KeyValue gives it.
func identityOf(m *meta.Model, key any) identity {
	if n, ok := key.(int64); ok {
		return identity{model: m, n: n}
	}

	return identity{model: m, key: key}
}

// value returns the key as meta.KeyValue gives it.
func (id identity) value() any {
	if id.key == nil {
		return id.n
	}

	return id.key
}

// firsts holds, for each model, the first tracked instance of each row, by key.
// Most keys are integers, which every loaded row is looked up by and most are
// then stored under, so those have a map of their own: its keys are hashed as
// integers and take 8 bytes, where a key in an interface is hashed through its
// type and takes 16, next to the model. The model's rows are found once for a
// whole load.
type firsts map[*meta.Model]*rows

type rows struct {
	ints  map[int64]*Entry
	other map[any]*Entry
}

// of returns the rows of a model, making them if make is set, or nil.
func (f firsts) of(m *meta.Model, make bool) *rows {
	r := f[m]
	if r == nil && make {
		r = &rows{}
		f[m] = r
	}

	return r
}

func (r *rows) get(id identity) *Entry {
	if r == nil {
		return nil
	}

	if id.key == nil {
		return r.ints[id.n]
	}

	return r.other[id.key]
}

// claim makes e the first instance of its row, unless the row has one.
func (r *rows) claim(id identity, e *Entry) {
	if id.key == nil {
		if _, seen := r.ints[id.n]; !seen {
			if r.ints == nil {
				r.ints = map[int64]*Entry{}
			}

			r.ints[id.n] = e
		}

		return
	}

	if _, seen := r.other[id.key]; !seen {
		if r.other == nil {
			r.other = map[any]*Entry{}
		}

		r.other[id.key] = e
	}
}

// release lets go of e as the first instance of its row, if it is.
func (r *rows) release(id identity, e *Entry) {
	if r == nil {
		return
	}

	if id.key == nil {
		if r.ints[id.n] == e {
			delete(r.ints, id.n)
		}

		return
	}

	if r.other[id.key] == e {
		delete(r.other, id.key)
	}
}

func (f firsts) get(id identity) *Entry { return f.of(id.model, false).get(id) }

func (f firsts) claim(id identity, e *Entry) { f.of(id.model, true).claim(id, e) }

func (f firsts) release(id identity, e *Entry) { f.of(id.model, false).release(id, e) }

// Tracker holds the tracked entities of one context. It is safe for concurrent
// use, but an entity should not change while SaveChanges reads it.
type Tracker struct {
	mu      sync.Mutex
	held    chan struct{} // holds a token while an operation claims the tracker
	owner   atomic.Pointer[Claim]
	undo    []func() // rollback work waiting for the claim, under mu
	seq     uint64
	entries map[unsafe.Pointer]*Entry
	first   firsts
	// order lists the entries oldest first, so that Entries need not sort them
	// on every save. It can also hold entries that are tracked no longer, which
	// compact takes out, and sorted says it is still in the order of seq.
	order  []*Entry
	sorted bool
	// deletions holds the deletions of each row whose entries were forgotten,
	// so that a rollback that comes late can tell a newer deletion happened. It
	// holds a row only while one of its deletions is unsettled.
	deletions map[identity]*history
	// swept holds the seq at each model's last bulk delete. An entry begun by
	// then may be of a row that delete removed, which ant cannot name.
	swept map[*meta.Model]uint64
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
		held:    make(chan struct{}, 1),
		entries: map[unsafe.Pointer]*Entry{},
		first:   firsts{},
		sorted:  true,
	}
}

func keyOf(m *meta.Model, entity unsafe.Pointer) (identity, bool) {
	if n, ok, fast := m.IntKey(entity); fast {
		return identity{model: m, n: n}, ok
	}

	v, ok := m.KeyOf(entity)

	return identityOf(m, v), ok
}

func (t *Tracker) add(m *meta.Model, entity unsafe.Pointer, state State) *Entry {
	e := &Entry{}

	var snapshot unsafe.Pointer

	if state != Added {
		snapshot = reflect.New(m.SnapshotLayout().Type).UnsafePointer()
	}

	id, keyed := keyOf(m, entity)
	t.start(e, m, entity, state, snapshot, id, keyed, nil)

	return e
}

// start fills in a new entry and tracks it. snapshot is memory for its
// snapshot, or nil for an entry that has none, id is its key, if keyed, and
// known the rows of its model, if the caller has them.
func (t *Tracker) start(
	e *Entry,
	m *meta.Model,
	entity unsafe.Pointer,
	state State,
	snapshot unsafe.Pointer,
	id identity,
	keyed bool,
	known *rows,
) {
	t.seq++

	e.Model, e.Entity, e.State, e.seq = m, entity, state, t.seq

	if snapshot != nil {
		snapshotInto(m, snapshot, entity)
		e.snapshot = snapshot
	}

	t.entries[entity] = e
	t.list(e)

	// An entity's memory can later hold another row, as when a slice shifts, so
	// the key is kept in the entry.
	if keyed {
		e.id, e.keyed = id, true

		if known == nil {
			known = t.first.of(m, true)
		}

		known.claim(id, e)
	}

	if t.OnTrack != nil {
		t.OnTrack(e)
	}
}

// list puts an entry that entries holds into order, if it is not there.
func (t *Tracker) list(e *Entry) {
	if e.listed {
		return
	}

	if n := len(t.order); n > 0 && t.order[n-1].seq > e.seq {
		t.sorted = false
	}

	e.listed = true
	t.order = append(t.order, e)
}

// compact takes the entries that are tracked no longer out of order.
func (t *Tracker) compact() {
	kept := t.order[:0]

	for _, e := range t.order {
		if t.entries[e.Entity] == e {
			kept = append(kept, e)
		} else {
			e.listed = false
		}
	}

	clear(t.order[len(kept):])
	t.order = kept

	if len(kept) == 0 {
		t.order, t.sorted = nil, true
	}
}

// identify records the entry's key, if it has one yet. An entity's memory can
// later hold another row, as when a slice shifts, so the key is kept here.
func (t *Tracker) identify(e *Entry) {
	if id, ok := keyOf(e.Model, e.Entity); ok {
		e.id, e.keyed = id, true

		t.first.claim(id, e)
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

	known := t.first.of(m, true)

	if existing := known.get(id); existing != nil {
		return existing.Entity
	}

	e := &Entry{}
	t.start(
		e,
		m,
		entity,
		Unchanged,
		reflect.New(m.SnapshotLayout().Type).UnsafePointer(),
		id,
		true,
		known,
	)

	return entity
}

// LoadAll is Load for the rows of one query: it puts in place of each entity
// the instance to use. The tracker is locked once, and the entries and
// snapshots of the rows come from blocks, not one allocation each.
func (t *Tracker) LoadAll(m *meta.Model, entities []unsafe.Pointer) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if len(m.Keys) == 0 {
		return
	}

	// A tracker that holds nothing yet gets maps of the size it will need, so
	// that they do not grow step by step.
	if len(t.entries) == 0 && len(entities) > 8 {
		t.entries = make(map[unsafe.Pointer]*Entry, len(entities))
	}

	known := t.first.of(m, true)

	if len(known.ints) == 0 && len(entities) > 8 && m.IntegerKey() {
		known.ints = make(map[int64]*Entry, len(entities))
	}

	var from block

	for i, entity := range entities {
		id, ok := keyOf(m, entity)
		if !ok {
			continue
		}

		if existing := known.get(id); existing != nil {
			entities[i] = existing.Entity

			continue
		}

		e, snapshot := from.next(m, len(entities)-i)
		t.start(e, m, entity, Unchanged, snapshot, id, true, known)
	}
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

	e := t.put(m, entity, Unchanged)
	e.element = true

	return e
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

// Saved records e as saved in state, and returns the change. revert, if not nil,
// takes back what the save wrote into the entity, such as a generated key, when
// the change is taken back with nothing kept after it; it is given the address
// the entity has by then. A removal asked for after the save outlives its
// rollback: an entry that was new before it is forgotten, since its row goes
// with the rollback, and any other stays Deleted, as its row comes back.
func (t *Tracker) Saved(e *Entry, state State, revert func(entity unsafe.Pointer)) Change {
	return e.saved(state, func(restore func()) func() {
		return t.keepRemoval(e, func() {
			restore()
			t.revert(e, revert)
		})
	})
}

// revert runs revert on e's entity, and then lets go of the key e was known by
// if the entity no longer holds it, as when the key was generated by the insert
// the rollback took back.
func (t *Tracker) revert(e *Entry, revert func(entity unsafe.Pointer)) {
	if revert == nil {
		return
	}

	revert(e.Entity)

	t.mu.Lock()
	defer t.mu.Unlock()

	if e.keyed && !e.Holds(e.Entity) {
		t.first.release(e.id, e)

		e.id, e.keyed = identity{}, false
	}
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
// tracking it and runs revert, as Saved does, unless a write since stands. The
// entry is let go wherever its entity lives by then, as a save moves an entity
// whose slice grew.
func (t *Tracker) Added(
	m *meta.Model,
	entity unsafe.Pointer,
	revert func(entity unsafe.Pointer),
) Change {
	// put reports to OnTrack, which may panic, and must not leave the lock held.
	e := func() *Entry {
		t.mu.Lock()
		defer t.mu.Unlock()

		return t.put(m, entity, Unchanged)
	}()

	return e.writes().columns.record(
		func() {
			t.Forget(e)

			if revert != nil {
				revert(e.Entity)
			}
		},
		e.stale,
	)
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
	t.list(e)
}

// Get returns the entry for an entity, or nil.
func (t *Tracker) Get(entity unsafe.Pointer) *Entry {
	t.mu.Lock()
	defer t.mu.Unlock()

	return t.entries[entity]
}

// Tracks reports whether e is still tracked, which it is not once detached or
// forgotten. An entry whose address another took, as when a slice shifted,
// still is.
func (t *Tracker) Tracks(e *Entry) bool {
	t.mu.Lock()
	defer t.mu.Unlock()

	return !e.gone
}

// Find returns the first tracked instance with the given key, or nil.
func (t *Tracker) Find(m *meta.Model, key any) *Entry {
	t.mu.Lock()
	defer t.mu.Unlock()

	return t.first.get(identityOf(m, key))
}

// Current returns the first tracked instance with the given key, or nil if there
// is none, or if it was tracked before a bulk delete of its model, which may
// have removed its row. An Added instance has no row to remove, so it stays.
func (t *Tracker) Current(m *meta.Model, key any) *Entry {
	t.mu.Lock()
	defer t.mu.Unlock()

	e := t.first.get(identityOf(m, key))
	if e != nil && e.State != Added && e.seq <= t.swept[m] {
		return nil
	}

	return e
}

// Swept records a bulk delete of m's rows, after which Current reads none of the
// instances tracked so far.
func (t *Tracker) Swept(m *meta.Model) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.swept == nil {
		t.swept = map[*meta.Model]uint64{}
	}

	t.swept[m] = t.seq
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

	id := identityOf(m, key)

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

// undoDeletion records a deletion of row id, whose entries gone the tracker
// forgot, and returns the change, whose rollback tracks them again. A rollback
// can come late, once another operation has let go of the tracker, and that
// operation may have deleted the row again, which finds no entries to forget,
// as they are forgotten already. So the deletions of a row are kept in a
// history of the row's own, settled as an entry's writes are: a rollback with
// a kept deletion after it leaves the row untracked, to be read afresh, and one
// with nothing kept after it tracks the entries again, once every deletion
// after it has settled. The history goes once all its deletions have settled.
// It runs with the tracker locked, so its work does not lock it again.
func (t *Tracker) undoDeletion(id identity, gone []*Entry) Change {
	h := t.deletions[id]

	if h == nil {
		if len(gone) == 0 {
			return Change{}
		}

		h = &history{}

		if t.deletions == nil {
			t.deletions = map[identity]*history{}
		}

		t.deletions[id] = h
	}

	c := h.record(
		func() {
			for _, e := range gone {
				t.restore(e)
			}
		},
		func() {},
	)

	settle := func(f func()) func() {
		return func() {
			t.mu.Lock()
			defer t.mu.Unlock()

			f()

			if len(h.writes) == 0 && t.deletions[id] == h {
				delete(t.deletions, id)
			}
		}
	}

	return Change{undo: settle(c.Undo), keep: settle(c.Keep)}
}

// Mark returns how far the tracker has counted its entries, for ForgetBetween.
func (t *Tracker) Mark() uint64 {
	t.mu.Lock()
	defer t.mu.Unlock()

	return t.seq
}

// ForgetBetween stops tracking every entry that began after mark and by end, two
// values of Mark, as a rolled back transaction needs: it loaded and added them,
// and its rows are gone, so a retry loads and adds them again, and the next save
// must not write them again.
func (t *Tracker) ForgetBetween(mark, end uint64) {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.forgetBetween(mark, end)
}

// ForgetAlone is ForgetBetween, unless an entry that began by mark holds one of
// those entries among the members of a loaded relation: let go of, it would look
// taken out of that relation to the next save. It reports whether it let go.
func (t *Tracker) ForgetAlone(mark, end uint64) bool {
	t.mu.Lock()
	defer t.mu.Unlock()

	for _, e := range t.entries {
		if e.seq > mark {
			continue
		}

		for _, members := range e.Members {
			for _, member := range members {
				if member.seq > mark && member.seq <= end {
					return false
				}
			}
		}
	}

	t.forgetBetween(mark, end)

	return true
}

// forgetBetween is ForgetBetween, with the tracker locked.
func (t *Tracker) forgetBetween(mark, end uint64) {
	var gone []*Entry

	began := func(e *Entry) bool { return e.seq > mark && e.seq <= end }

	for _, e := range t.entries {
		if began(e) {
			gone = append(gone, e)
		}
	}

	for _, known := range t.first {
		for _, e := range known.ints {
			if began(e) && !slices.Contains(gone, e) {
				gone = append(gone, e)
			}
		}

		for _, e := range known.other {
			if began(e) && !slices.Contains(gone, e) {
				gone = append(gone, e)
			}
		}
	}

	for _, e := range gone {
		t.forget(e)
	}
}

func (t *Tracker) forget(e *Entry) {
	e.gone = true

	if t.entries[e.Entity] == e {
		delete(t.entries, e.Entity)
	}

	if e.keyed {
		t.first.release(e.id, e)
	}

	// The order is not searched for one entry. It is cleaned once it holds
	// more that are gone than that are tracked.
	if len(t.order) > 2*len(t.entries)+32 {
		t.compact()
	}
}

// Restore tracks an entry again, undoing Forget, unless another entry has taken
// its address since.
func (t *Tracker) Restore(e *Entry) {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.restore(e)
}

// restore is Restore, with the tracker locked.
func (t *Tracker) restore(e *Entry) {
	if other, ok := t.entries[e.Entity]; ok && other != e {
		return
	}

	t.entries[e.Entity] = e
	t.list(e)
	e.gone = false

	if e.keyed {
		t.first.claim(e.id, e)
	}
}

// Clear stops tracking everything.
func (t *Tracker) Clear() {
	t.mu.Lock()
	defer t.mu.Unlock()

	for _, e := range t.entries {
		e.gone = true
	}

	for _, e := range t.order {
		e.listed = false
	}

	clear(t.entries)
	clear(t.first)

	t.order, t.sorted = nil, true
}

// Entries returns every entry, oldest first.
func (t *Tracker) Entries() []*Entry {
	t.mu.Lock()
	defer t.mu.Unlock()

	// Every entry that entries holds is in order, once, so the two hold the
	// same entries when they are as long as each other.
	if len(t.order) != len(t.entries) {
		t.compact()
	}

	if !t.sorted {
		slices.SortFunc(t.order, func(a, b *Entry) int { return cmp.Compare(a.seq, b.seq) })
		t.sorted = true
	}

	return slices.Clone(t.order)
}

// Len returns the number of tracked entities.
func (t *Tracker) Len() int {
	t.mu.Lock()
	defer t.mu.Unlock()

	return len(t.entries)
}
