package track

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"maps"
	"math"
	"reflect"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"
	"unsafe"

	"github.com/skudra-go/ant/internal/meta"
)

type address struct {
	City string
	Tags []string
}

type row struct {
	ID    int64
	Name  string
	Bio   *string
	Data  []byte
	At    time.Time
	Tags  []string
	Home  address
	Attrs map[string]string `ant:"column=attrs"`
	Peers []row
}

func fresh() *row {
	return &row{
		ID:    1,
		Name:  "a",
		Bio:   new("b"),
		Data:  []byte("c"),
		Tags:  []string{"d"},
		At:    time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		Home:  address{City: "e", Tags: []string{"f"}},
		Attrs: map[string]string{"g": "h"},
	}
}

func changed(e *Entry) []string {
	var out []string

	for _, f := range e.Changed() {
		out = append(out, f.Short)
	}

	return out
}

func TestChanged(t *testing.T) {
	cases := []struct {
		name   string
		change func(*row)
		want   string
	}{
		{"nothing", func(*row) {}, ""},
		{"plain field", func(r *row) { r.Name = "x" }, "Name"},
		{"set to zero", func(r *row) { r.Name = "" }, "Name"},
		{"pointer replaced", func(r *row) { r.Bio = new("x") }, "Bio"},
		{"pointer replaced with equal value", func(r *row) { r.Bio = new("b") }, ""},
		{"pointer set to nil", func(r *row) { r.Bio = nil }, "Bio"},
		{"written through pointer", func(r *row) { *r.Bio = "x" }, "Bio"},
		{"bytes edited in place", func(r *row) { r.Data[0] = 'x' }, "Data"},
		{"slice edited in place", func(r *row) { r.Tags[0] = "x" }, "Tags"},
		{"slice appended", func(r *row) { r.Tags = append(r.Tags, "x") }, "Tags"},
		{"map edited in place", func(r *row) { r.Attrs["g"] = "x" }, "Attrs"},
		{"nested struct field", func(r *row) { r.Home.City = "x" }, "Home"},
		{"slice inside struct", func(r *row) { r.Home.Tags[0] = "x" }, "Home"},
		{
			"same instant in another zone",
			func(r *row) { r.At = r.At.In(time.FixedZone("x", 3600)) },
			"",
		},
		{"another instant", func(r *row) { r.At = r.At.Add(time.Second) }, "At"},
		{"the key is not a change", func(r *row) { r.ID = 2 }, ""},
		{"a relation is not a change", func(r *row) { r.Peers = []row{{}} }, ""},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := fresh()
			e := New().Track(meta.Of[row](), unsafe.Pointer(r), Unchanged)
			c.change(r)

			got := ""

			if names := changed(e); len(names) > 0 {
				got = names[0]
			}

			if got != c.want {
				t.Errorf("changed = %q, want %q", got, c.want)
			}
		})
	}
}

func TestSavedAndUndo(t *testing.T) {
	r := fresh()
	e := New().Track(meta.Of[row](), unsafe.Pointer(r), Unchanged)
	r.Name = "x"

	undo := e.Saved(Unchanged)

	if len(e.Changed()) != 0 {
		t.Error("a saved value still counts as changed")
	}

	undo.Undo()

	if got := changed(e); len(got) != 1 || got[0] != "Name" {
		t.Errorf("after undo: %v", got)
	}
}

func TestLoadReturnsTheFirstInstance(t *testing.T) {
	tr, m := New(), meta.Of[row]()

	a, b := fresh(), fresh()
	if got := tr.Load(m, unsafe.Pointer(a)); got != unsafe.Pointer(a) {
		t.Fatal("first load replaced")
	}

	if got := tr.Load(m, unsafe.Pointer(b)); got != unsafe.Pointer(a) {
		t.Fatal("second load not resolved to the first")
	}

	if tr.Len() != 1 {
		t.Errorf("len = %d", tr.Len())
	}
}

func TestCopyKeepsBothInstances(t *testing.T) {
	tr, m := New(), meta.Of[row]()
	a, b := fresh(), fresh()
	tr.Load(m, unsafe.Pointer(a))
	tr.Copy(m, unsafe.Pointer(b))

	if tr.Len() != 2 {
		t.Fatalf("len = %d", tr.Len())
	}

	if got := tr.Find(m, int64(1)); got == nil || got.Entity != unsafe.Pointer(a) {
		t.Error("identity moved to the copy")
	}
}

func TestRemoveMovesNothingElse(t *testing.T) {
	tr, m := New(), meta.Of[row]()
	a := fresh()
	tr.Track(m, unsafe.Pointer(a), Unchanged)
	tr.Remove(unsafe.Pointer(a))

	if tr.Len() != 0 || tr.Find(m, int64(1)) != nil {
		t.Error("entry survived Remove")
	}

	tr.Remove(unsafe.Pointer(a)) // a second Remove is harmless
}

func TestNoKeyIsNotTracked(t *testing.T) {
	type keyless struct{ Name string }

	tr, m := New(), meta.Of[keyless]()

	v := &keyless{"a"}
	if got := tr.Load(m, unsafe.Pointer(v)); got != unsafe.Pointer(v) || tr.Len() != 0 {
		t.Error("keyless entity was tracked")
	}
}

func TestEntriesAreOldestFirst(t *testing.T) {
	tr, m := New(), meta.Of[row]()

	for i := range int64(50) {
		r := fresh()
		r.ID = 100 - i
		tr.Track(m, unsafe.Pointer(r), Unchanged)
	}

	entries := tr.Entries()
	for i := 1; i < len(entries); i++ {
		if entries[i-1].seq >= entries[i].seq {
			t.Fatal("out of order")
		}
	}
}

func TestMoveFollowsTheEntity(t *testing.T) {
	tr, m := New(), meta.Of[row]()
	a := fresh()
	e := tr.Track(m, unsafe.Pointer(a), Unchanged)
	b := *a
	b.Name = "changed in the new location"
	tr.Move(e, unsafe.Pointer(&b))

	if tr.Get(unsafe.Pointer(a)) != nil || tr.Get(unsafe.Pointer(&b)) != e {
		t.Error("the address map did not move")
	}

	if got := changed(e); len(got) != 1 || got[0] != "Name" {
		t.Errorf("changed = %v", got)
	}
}

func TestForgetUsesTheStoredKey(t *testing.T) {
	tr, m := New(), meta.Of[row]()
	a := fresh()
	e := tr.Track(m, unsafe.Pointer(a), Unchanged)
	a.ID = 99 // memory now describes another row

	tr.Forget(e)

	if tr.Find(m, int64(1)) != nil || tr.Len() != 0 {
		t.Error("entry survived Forget")
	}
}

func TestScratchIsPrivate(t *testing.T) {
	tr, m := New(), meta.Of[row]()
	a := fresh()
	e := tr.Track(m, unsafe.Pointer(a), Unchanged)
	scratch := (*row)(e.Scratch())
	*scratch.Bio = "x"
	scratch.Name = "x"

	if len(e.Changed()) != 0 {
		t.Error("changing the scratch copy changed the snapshot")
	}
}

func TestKeyChanged(t *testing.T) {
	a := fresh()

	e := New().Track(meta.Of[row](), unsafe.Pointer(a), Unchanged)
	if e.KeyChanged() {
		t.Error("key reported changed")
	}

	a.ID = 2

	if !e.KeyChanged() {
		t.Error("key change missed")
	}
}

// Rollback work runs at once on a free tracker, and otherwise when the operation
// that holds it lets go, in the order it came.
func TestUndoWaitsForTheClaim(t *testing.T) {
	tr := New()

	var ran []int

	tr.Settle(func() { ran = append(ran, 1) })

	if len(ran) != 1 {
		t.Fatalf("on a free tracker the work runs at once, ran %v", ran)
	}

	c := tr.TryClaim()
	tr.Settle(func() { ran = append(ran, 2) })
	tr.Settle(func() { ran = append(ran, 3) })

	if len(ran) != 1 {
		t.Fatalf("while held the work waits, ran %v", ran)
	}

	if tr.TryClaim() != nil {
		t.Fatal("a second claim succeeded")
	}

	tr.Release(c)
	tr.Release(c)

	if fmt.Sprint(ran) != "[1 2 3]" {
		t.Fatalf("the release ran %v", ran)
	}

	if c := tr.TryClaim(); c == nil {
		t.Fatal("the tracker stayed held")
	} else if tr.Owns(c) {
		tr.Release(c)
	}
}

// However claims and rollbacks interleave, every piece of work runs once.
func TestUndoUnderContention(t *testing.T) {
	tr := New()

	var (
		wg  sync.WaitGroup
		ran atomic.Int64
	)

	for range 50 {
		wg.Go(func() {
			for range 100 {
				if c := tr.TryClaim(); c != nil {
					tr.Release(c)
				}

				tr.Settle(func() { ran.Add(1) })
			}
		})
	}

	wg.Wait()

	if got := ran.Load(); got != 5000 {
		t.Fatalf("ran %d of 5000", got)
	}
}

// A claim runs rollback work that was queued while the tracker was free, which
// happens when the work arrives as the last holder lets go.
func TestClaimRunsQueuedUndo(t *testing.T) {
	tr := New()
	ran := false

	tr.mu.Lock()
	tr.undo = append(tr.undo, func() { ran = true })
	tr.mu.Unlock()

	c := tr.TryClaim()
	defer tr.Release(c)

	if !ran {
		t.Fatal("the claim came back before the queued work ran")
	}
}

// An undo restores what its change replaced, unless the entry changed since:
// then the newer change stands, and an unchanged entry becomes Modified, so the
// next save writes every column.
func TestUndoAfterALaterChange(t *testing.T) {
	m := meta.OfType(reflect.TypeFor[row]())
	tr := New()
	r := &row{ID: 1, Name: "a"}
	e := tr.Track(m, unsafe.Pointer(r), Added)

	t.Run("in time, it restores", func(t *testing.T) {
		undo := e.Saved(Unchanged)
		undo.Undo()

		if e.State != Added {
			t.Fatalf("state %s", e.State)
		}
	})

	t.Run("newest first, each finds the entry as it left it", func(t *testing.T) {
		saved := e.Saved(Unchanged)
		links := e.SetLinks(nil, nil)
		links.Undo()
		saved.Undo()

		if e.State != Added {
			t.Fatalf("state %s", e.State)
		}
	})

	t.Run("too late, the newer write stands", func(t *testing.T) {
		undo := e.Saved(Unchanged)
		e.Saved(Unchanged).Keep() // a newer save, by another operation, that commits
		undo.Undo()

		if e.State != Modified {
			t.Fatalf("state %s, want Modified", e.State)
		}
	})

	t.Run("loading a relation since does not make it late", func(t *testing.T) {
		e := New().Track(m, unsafe.Pointer(&row{ID: 2}), Added)
		undo := e.Saved(Unchanged)
		e.SetMembers(nil, nil)
		e.SetParent(nil, int64(1))
		undo.Undo()

		if e.State != Added {
			t.Fatalf("state %s, want Added", e.State)
		}
	})

	t.Run("a later load does not stop the relations coming back", func(t *testing.T) {
		e := New().Track(m, unsafe.Pointer(&row{ID: 3}), Unchanged)
		e.SetLinks(nil, nil)

		undo := e.SetLinks(map[*meta.Relation]map[any]*Entry{nil: {}}, nil)
		e.SetMembers(nil, map[any]*Entry{})
		undo.Undo()

		if e.Members != nil {
			t.Fatal("the rollback left the relations as a later load had them")
		}
	})

	t.Run("a reload undoes with the values it replaced, but not later edits", func(t *testing.T) {
		r := &row{ID: 4, Name: "before", Bio: nil}
		e := New().Track(m, unsafe.Pointer(r), Added)
		saved := e.Saved(Unchanged)

		before := *r
		r.Name = "read"
		reloaded := e.Reloaded(unsafe.Pointer(&before))

		bio := "edited after the reload"
		r.Bio = &bio

		reloaded.Undo()
		saved.Undo()

		switch {
		case e.State != Added:
			t.Fatalf("state %s, want Added", e.State)
		case r.Name != "before":
			t.Fatalf("name %q, want the value the reload replaced", r.Name)
		case r.Bio == nil || *r.Bio != bio:
			t.Fatal("the rollback took back an edit made after the reload")
		}
	})
}

// Restore does not take back an address another entry holds now.
func TestRestoreKeepsANewerEntry(t *testing.T) {
	m := meta.OfType(reflect.TypeFor[row]())
	tr := New()
	r := &row{ID: 1}
	old := tr.Track(m, unsafe.Pointer(r), Unchanged)
	undo := tr.Forget(old)

	newer := tr.Track(m, unsafe.Pointer(r), Added)

	undo.Undo()

	if tr.Get(unsafe.Pointer(r)) != newer {
		t.Fatal("the forgotten entry took the address back")
	}
}

// A removal asked for after a save outlives the save's rollback.
func TestRemovalOutlivesRollback(t *testing.T) {
	m := meta.OfType(reflect.TypeFor[row]())

	t.Run("a new row is forgotten, since it was never stored", func(t *testing.T) {
		tr := New()
		r := &row{ID: 1}
		e := tr.Track(m, unsafe.Pointer(r), Added)
		undo := tr.Saved(e, Unchanged, nil)
		tr.Track(m, unsafe.Pointer(r), Deleted)
		undo.Undo()

		if tr.Get(unsafe.Pointer(r)) != nil {
			t.Fatalf("still tracked as %s", e.State)
		}
	})

	t.Run("a stored row stays to be deleted", func(t *testing.T) {
		tr := New()
		r := &row{ID: 2}
		e := tr.Track(m, unsafe.Pointer(r), Modified)
		undo := tr.Saved(e, Unchanged, nil)
		tr.Track(m, unsafe.Pointer(r), Deleted)
		undo.Undo()

		if e.State != Deleted {
			t.Fatalf("state %s, want Deleted", e.State)
		}
	})

	t.Run("without a removal, the save is undone", func(t *testing.T) {
		tr := New()
		r := &row{ID: 3}
		e := tr.Track(m, unsafe.Pointer(r), Added)
		undo := tr.Saved(e, Unchanged, nil)
		undo.Undo()

		if e.State != Added {
			t.Fatalf("state %s, want Added", e.State)
		}
	})
}

// Rollback work that panics still lets the tracker go.
func TestPanickingUndoLetsGo(t *testing.T) {
	tr := New()
	c := tr.TryClaim()
	tr.Settle(func() { panic("broken undo") })

	func() {
		defer func() { _ = recover() }()

		tr.Release(c)
	}()

	if c := tr.TryClaim(); c == nil {
		t.Fatal("the tracker stayed held after a panic in Release")
	} else {
		tr.Release(c)
	}

	tr.mu.Lock()
	tr.undo = append(tr.undo, func() { panic("broken undo") })
	tr.mu.Unlock()

	func() {
		defer func() { _ = recover() }()

		tr.TryClaim()
	}()

	if c := tr.TryClaim(); c == nil {
		t.Fatal("the tracker stayed held after a panic in a claim")
	} else {
		tr.Release(c)
	}
}

// A deletion's undo tracks the row again, unless the row was deleted again
// since, as a late rollback finds when another operation deleted it after the
// transaction did. Undos of one transaction run newest first, and each applies.
func TestDeletionUndoLeavesANewerDeletionAlone(t *testing.T) {
	m := meta.Of[row]()

	t.Run("on time", func(t *testing.T) {
		tr := New()
		a := fresh()
		tr.Track(m, unsafe.Pointer(a), Unchanged)

		undo := tr.ForgetRow(m, int64(1))
		undo.Undo()

		if tr.Find(m, int64(1)) == nil {
			t.Error("the row was not tracked again")
		}

		if len(tr.deletions) != 0 {
			t.Error("a row whose deletion was taken back is still counted")
		}
	})

	t.Run("after a newer deletion", func(t *testing.T) {
		tr := New()
		a := fresh()

		for _, older := range []func(e *Entry) Change{
			func(*Entry) Change { return tr.ForgetRow(m, int64(1)) },
			tr.Deleted,
		} {
			undo := older(tr.Track(m, unsafe.Pointer(a), Unchanged))

			tr.ForgetRow(m, int64(1)).Keep() // deleted again, by an operation that commits
			undo.Undo()

			if tr.Find(m, int64(1)) != nil || tr.Get(unsafe.Pointer(a)) != nil {
				t.Error("a late undo tracked a row that was deleted since")
			}
		}
	})

	t.Run("newest first, in one transaction", func(t *testing.T) {
		tr := New()
		a, b := fresh(), fresh()
		tr.Track(m, unsafe.Pointer(a), Unchanged)
		first := tr.ForgetRow(m, int64(1))

		tr.Track(m, unsafe.Pointer(b), Unchanged)
		second := tr.ForgetRow(m, int64(1))

		second.Undo()

		if got := tr.Find(m, int64(1)); got == nil || got.Entity != unsafe.Pointer(b) {
			t.Fatal("the newer deletion was not taken back")
		}

		tr.Remove(unsafe.Pointer(b))
		first.Undo()

		if got := tr.Find(m, int64(1)); got == nil || got.Entity != unsafe.Pointer(a) {
			t.Error("the older deletion was not taken back after it")
		}
	})

	t.Run("a row never tracked is not counted", func(t *testing.T) {
		tr := New()
		tr.ForgetRow(m, int64(7))

		if len(tr.deletions) != 0 {
			t.Errorf("counted %d rows", len(tr.deletions))
		}
	})
}

// A relation save's undo restores what the relations held, unless a newer save
// recorded them since: then it takes back only its own change, where the newer
// save left it.
func TestLinksUndoKeepsANewerSave(t *testing.T) {
	m := meta.Of[row]()
	tr := New()
	owner := tr.Track(m, unsafe.Pointer(fresh()), Unchanged)

	var r meta.Relation

	a, b, c := &Entry{}, &Entry{}, &Entry{}

	links := func(members map[any]*Entry, parent any) (
		map[*meta.Relation]map[any]*Entry,
		map[*meta.Relation]any,
	) {
		parents := map[*meta.Relation]any{}

		if parent != nil {
			parents[&r] = parent
		}

		return map[*meta.Relation]map[any]*Entry{&r: members}, parents
	}

	t.Run("on time", func(t *testing.T) {
		owner.SetLinks(links(map[any]*Entry{1: a}, 10))

		undo := owner.SetLinks(links(map[any]*Entry{2: b}, 20))
		owner.SetMembers(&r, map[any]*Entry{2: b, 3: c}) // a load does not count

		undo.Undo()

		if !reflect.DeepEqual(owner.Members[&r], map[any]*Entry{1: a}) || owner.Parents[&r] != 10 {
			t.Errorf("got %v and %v", owner.Members[&r], owner.Parents)
		}
	})

	t.Run("after a newer save", func(t *testing.T) {
		owner.SetLinks(links(map[any]*Entry{1: a}, 10))

		// The older save drops 1, adds 2, and moves the parent; the newer one
		// adds 3 and leaves the rest.
		undo := owner.SetLinks(links(map[any]*Entry{2: b}, 20))
		owner.SetLinks(links(map[any]*Entry{2: b, 3: c}, 20)).Keep()

		undo.Undo()

		if want := map[any]*Entry{1: a, 3: c}; !reflect.DeepEqual(owner.Members[&r], want) {
			t.Errorf("members %v, want %v", owner.Members[&r], want)
		}

		if owner.Parents[&r] != 10 {
			t.Errorf("parent %v, want 10", owner.Parents[&r])
		}
	})

	t.Run("after a newer save that changed the same member", func(t *testing.T) {
		owner.SetLinks(links(map[any]*Entry{1: a}, 10))

		undo := owner.SetLinks(links(map[any]*Entry{1: a, 2: b}, 20))
		owner.SetLinks(links(map[any]*Entry{1: a, 2: c}, 30)).Keep()

		undo.Undo()

		if want := map[any]*Entry{1: a, 2: c}; !reflect.DeepEqual(owner.Members[&r], want) {
			t.Errorf("members %v, want %v", owner.Members[&r], want)
		}

		if owner.Parents[&r] != 30 {
			t.Errorf("parent %v, want the newer save's 30", owner.Parents[&r])
		}
	})
}

// Two transactions may each save an entry and roll back, in either order: a
// rollback whose write has a newer one after it waits for that one, and puts
// back what it replaced once the newer one rolls back too, or answers as late
// if the newer one commits.
func TestOverlappingRollbacks(t *testing.T) {
	m := meta.OfType(reflect.TypeFor[row]())

	cases := []struct {
		name   string
		settle func(older, newer Change)
		want   State
	}{
		{
			"the newer rolls back first",
			func(older, newer Change) { newer.Undo(); older.Undo() },
			Added,
		},
		{
			"the older rolls back first",
			func(older, newer Change) { older.Undo(); newer.Undo() },
			Added,
		},
		{
			"the older rolls back, the newer commits",
			func(older, newer Change) { older.Undo(); newer.Keep() },
			Modified,
		},
		{
			"the newer commits, the older rolls back",
			func(older, newer Change) { newer.Keep(); older.Undo() },
			Modified,
		},
		{
			"the older commits, the newer rolls back",
			func(older, newer Change) { older.Keep(); newer.Undo() },
			Unchanged,
		},
		{"both commit", func(older, newer Change) { older.Keep(); newer.Keep() }, Unchanged},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			e := New().Track(m, unsafe.Pointer(&row{ID: 1}), Added)
			older := e.Saved(Unchanged)
			newer := e.Saved(Unchanged)

			tt.settle(older, newer)

			if e.State != tt.want {
				t.Errorf("state %s, want %s", e.State, tt.want)
			}

			if len(e.unsettled().columns.writes) != 0 {
				t.Errorf(
					"%d writes still held after both settled",
					len(e.unsettled().columns.writes),
				)
			}
		})
	}

	t.Run("relations, both rolled back, oldest last", func(t *testing.T) {
		var r meta.Relation

		a, b := &Entry{}, &Entry{}
		e := New().Track(m, unsafe.Pointer(&row{ID: 2}), Unchanged)
		e.Members = map[*meta.Relation]map[any]*Entry{&r: {}}

		older := e.SetLinks(map[*meta.Relation]map[any]*Entry{&r: {1: a}}, nil)
		newer := e.SetLinks(map[*meta.Relation]map[any]*Entry{&r: {1: a, 2: b}}, nil)

		older.Undo()
		newer.Undo()

		if len(e.Members[&r]) != 0 {
			t.Errorf("members %v, want none, as both saves rolled back", e.Members[&r])
		}
	})
}

// Writes that commit are let go, so an entry saved again and again holds none,
// and an open write under them holds only that a kept one came after it.
func TestKeptWritesAreLetGo(t *testing.T) {
	e := New().Track(meta.OfType(reflect.TypeFor[row]()), unsafe.Pointer(&row{ID: 1}), Unchanged)

	open := e.Saved(Unchanged)

	for range 100 {
		e.Saved(Unchanged).Keep()
		e.SetLinks(nil, nil).Keep()
	}

	if len(e.unsettled().columns.writes) != 1 || len(e.unsettled().links.writes) != 0 {
		t.Errorf(
			"held %d and %d writes, want only the open one",
			len(e.unsettled().columns.writes),
			len(e.unsettled().links.writes),
		)
	}

	if !e.unsettled().columns.writes[0].overtaken {
		t.Error("the open write forgot that a kept write came after it")
	}

	open.Keep()

	if e.unsettled().columns.writes != nil {
		t.Errorf("held %d writes once all were kept", len(e.unsettled().columns.writes))
	}
}

// When every write has rolled back, the history lets go of its array, whose
// slots would otherwise keep each write's snapshot alive.
func TestSettledHistoryHoldsNothing(t *testing.T) {
	e := New().Track(meta.OfType(reflect.TypeFor[row]()), unsafe.Pointer(&row{ID: 1}), Unchanged)

	var changes []Change

	for range 8 {
		changes = append(changes, e.Saved(Unchanged))
	}

	array := e.unsettled().columns.writes[:cap(e.unsettled().columns.writes)]

	for _, c := range changes[4:] {
		c.Undo()
	}

	for i, w := range array[4:] {
		if w != nil {
			t.Fatalf("slot %d still holds a write that rolled back", 4+i)
		}
	}

	for _, c := range changes[:4] {
		c.Undo()
	}

	if e.unsettled().columns.writes != nil {
		t.Error("an empty history kept its array")
	}
}

// outcome is how one write's transaction ends.
type outcome bool

const (
	commits    outcome = true
	rollsBack  outcome = false
	maxHistory         = 4
)

// orders returns every order of 0..n-1.
func orders(n int) [][]int {
	if n == 0 {
		return [][]int{{}}
	}

	var out [][]int

	for _, rest := range orders(n - 1) {
		for i := 0; i <= len(rest); i++ {
			order := slices.Insert(slices.Clone(rest), i, n-1)
			out = append(out, order)
		}
	}

	return out
}

// outcomes returns every way n writes can end.
func outcomes(n int) [][]outcome {
	out := [][]outcome{{}}

	for range n {
		var next [][]outcome

		for _, o := range out {
			next = append(
				next,
				append(slices.Clone(o), commits),
				append(slices.Clone(o), rollsBack),
			)
		}

		out = next
	}

	return out
}

// settleIn ends each write's change, in the order given.
func settleIn(changes []Change, ends []outcome, order []int) {
	for _, i := range order {
		if ends[i] == commits {
			changes[i].Keep()
		} else {
			changes[i].Undo()
		}
	}
}

// Writes that overlap, each in a transaction of its own, may end in any order.
// Whatever the order, an entry ends as a single transaction would leave it:
// as the newest write that committed left it, or as it was before them all if
// none did, and Modified if a write under the newest kept one rolled back, so
// the next save writes every column.
func TestColumnsSettleInAnyOrder(t *testing.T) {
	m := meta.OfType(reflect.TypeFor[row]())

	for n := 1; n <= maxHistory; n++ {
		for _, ends := range outcomes(n) {
			for _, order := range orders(n) {
				r := &row{ID: 1, Name: "before"}
				e := New().Track(m, unsafe.Pointer(r), Unchanged)

				changes := make([]Change, n)

				for i := range changes {
					r.Name = fmt.Sprint("write ", i)
					changes[i] = e.Saved(Unchanged)
				}

				settleIn(changes, ends, order)

				newest, under := -1, false

				for i, end := range ends {
					if end == commits {
						newest = i
					}
				}

				for _, end := range ends[:max(newest, 0)] {
					under = under || end == rollsBack
				}

				want, state := "before", Unchanged

				if newest >= 0 {
					want = fmt.Sprint("write ", newest)
				}

				if under {
					state = Modified
				}

				if got := (*row)(e.Scratch()).Name; got != want || e.State != state {
					t.Errorf(
						"ends %v in order %v: snapshot %q and %s, want %q and %s",
						ends,
						order,
						got,
						e.State,
						want,
						state,
					)
				}

				if e.unsettled().columns.writes != nil {
					t.Errorf(
						"ends %v in order %v: %d writes held",
						ends,
						order,
						len(e.unsettled().columns.writes),
					)
				}
			}
		}
	}
}

// The same holds for the members of loaded relations: each member ends as the
// newest write that committed and changed it left it, or as it was before them
// all. With A={1}, B={1,2}, and C={1,2,3}, where only B commits, the members are
// {2}: 1 went in with A and 3 with C, which rolled back.
func TestLinksSettleInAnyOrder(t *testing.T) {
	m := meta.Of[row]()

	var r meta.Relation

	entries := map[int]*Entry{}

	for k := range 6 {
		entries[k] = &Entry{}
	}

	// Each write sets the members in sets[i+1] and the parent i+1, over what
	// the one before it set.
	sets := [][]int{{0}, {0, 1}, {1, 2}, {1, 2, 3}, {3, 4}}

	members := func(keys []int) map[any]*Entry {
		out := map[any]*Entry{}

		for _, k := range keys {
			out[k] = entries[k]
		}

		return out
	}

	for n := 1; n <= maxHistory; n++ {
		for _, ends := range outcomes(n) {
			for _, order := range orders(n) {
				e := New().Track(m, unsafe.Pointer(fresh()), Unchanged)
				e.Members = map[*meta.Relation]map[any]*Entry{&r: members(sets[0])}
				e.Parents = map[*meta.Relation]any{&r: 0}

				changes := make([]Change, n)

				for i := range changes {
					changes[i] = e.SetLinks(
						map[*meta.Relation]map[any]*Entry{&r: members(sets[i+1])},
						map[*meta.Relation]any{&r: i + 1},
					)
				}

				settleIn(changes, ends, order)

				want, parent := map[any]*Entry{}, any(0)

				for k := range 6 {
					in := slices.Contains(sets[0], k)

					for i, end := range ends {
						if end == commits &&
							slices.Contains(sets[i], k) != slices.Contains(sets[i+1], k) {
							in = slices.Contains(sets[i+1], k)
						}
					}

					if in {
						want[k] = entries[k]
					}
				}

				for i, end := range ends {
					if end == commits {
						parent = i + 1
					}
				}

				if got := e.Members[&r]; !maps.Equal(got, want) || e.Parents[&r] != parent {
					t.Errorf(
						"ends %v in order %v: members %v and parent %v, want %v and %v",
						ends,
						order,
						keysOf(got),
						e.Parents[&r],
						keysOf(want),
						parent,
					)
				}

				if e.unsettled().links.writes != nil {
					t.Errorf(
						"ends %v in order %v: %d writes held",
						ends,
						order,
						len(e.unsettled().links.writes),
					)
				}
			}
		}
	}
}

func keysOf(members map[any]*Entry) []int {
	var out []int

	for k := range members {
		out = append(out, k.(int))
	}

	slices.Sort(out)

	return out
}

// Deletions of one row by overlapping transactions may end in any order. The
// row's entries come back if no deletion committed, in the state each was in,
// and stay gone if one did; either way the tracker lets go of the record.
func TestDeletionsSettleInAnyOrder(t *testing.T) {
	m := meta.Of[row]()

	for n := 1; n <= maxHistory; n++ {
		for _, ends := range outcomes(n) {
			for _, order := range orders(n) {
				tr := New()
				r := fresh()
				e := tr.Track(m, unsafe.Pointer(r), Deleted) // a pending removal, saved

				changes := make([]Change, n)

				for i := range changes {
					if i == 0 {
						changes[i] = tr.Deleted(e)
					} else {
						changes[i] = tr.ForgetRow(m, int64(1)) // finds nothing to forget
					}
				}

				settleIn(changes, ends, order)

				committed := slices.Contains(ends, commits)

				switch got := tr.Find(m, int64(1)); {
				case committed && got != nil:
					t.Errorf("ends %v in order %v: a deleted row is tracked", ends, order)
				case !committed && got != e:
					t.Errorf("ends %v in order %v: the row did not come back", ends, order)
				case !committed && e.State != Deleted:
					t.Errorf("ends %v in order %v: the removal is no longer pending", ends, order)
				}

				if len(tr.deletions) != 0 {
					t.Errorf(
						"ends %v in order %v: %d rows still recorded",
						ends,
						order,
						len(tr.deletions),
					)
				}
			}
		}
	}
}

// A rollback with nothing kept after it takes back what the save wrote into
// the entity, at the address it has by then, and the key it was known by goes,
// so the next save inserts it again. A value changed since stays, and a late
// rollback leaves the values alone.
func TestSavedRevertsTheEntity(t *testing.T) {
	m := meta.Of[row]()

	zeroID := func(entity unsafe.Pointer) {
		if r := (*row)(entity); r.ID == 7 {
			r.ID = 0
		}
	}

	t.Run("in time", func(t *testing.T) {
		tr := New()
		r := &row{}
		e := tr.Track(m, unsafe.Pointer(r), Added)
		r.ID = 7 // the key the insert generated
		undo := tr.Saved(e, Unchanged, zeroID)
		tr.Rekey(e)

		moved := *r
		tr.Move(e, unsafe.Pointer(&moved))

		undo.Undo()

		switch {
		case moved.ID != 0:
			t.Errorf("key %d left where the entity lives", moved.ID)
		case e.State != Added:
			t.Errorf("state %s", e.State)
		case tr.Find(m, int64(7)) != nil:
			t.Error("the entry is still known by the key the rollback took back")
		}
	})

	t.Run("late", func(t *testing.T) {
		tr := New()
		r := &row{}
		e := tr.Track(m, unsafe.Pointer(r), Added)
		r.ID = 7
		undo := tr.Saved(e, Unchanged, zeroID)
		tr.Rekey(e)
		tr.Saved(e, Unchanged, zeroID).Keep()

		undo.Undo()

		if r.ID != 7 || tr.Find(m, int64(7)) != e {
			t.Error("a late rollback took back the values a kept write stands on")
		}
	})

	t.Run("an entity added by the write", func(t *testing.T) {
		tr := New()
		r := &row{ID: 7}
		undo := tr.Added(m, unsafe.Pointer(r), zeroID)
		e := tr.Get(unsafe.Pointer(r))

		moved := *r
		tr.Move(e, unsafe.Pointer(&moved))

		undo.Undo()

		if tr.Get(unsafe.Pointer(&moved)) != nil || moved.ID != 0 {
			t.Error("the rollback did not follow the entity its slice moved")
		}
	})
}

// A deletion's count goes once no change that needs it is open, whether the
// deletions committed or rolled back.
func TestDeletionCountsAreRetired(t *testing.T) {
	m := meta.Of[row]()

	t.Run("committed", func(t *testing.T) {
		tr := New()
		tr.Track(m, unsafe.Pointer(fresh()), Unchanged)
		tr.ForgetRow(m, int64(1)).Keep()

		if len(tr.deletions) != 0 {
			t.Errorf("counted %d rows after the deletion committed", len(tr.deletions))
		}
	})

	t.Run("an older one rolled back after a newer one committed", func(t *testing.T) {
		tr := New()
		tr.Track(m, unsafe.Pointer(fresh()), Unchanged)
		older := tr.ForgetRow(m, int64(1))
		tr.ForgetRow(m, int64(1)).Keep()

		if len(tr.deletions) != 1 {
			t.Fatal("the count went while a rollback could still need it")
		}

		older.Undo()

		if len(tr.deletions) != 0 || tr.Find(m, int64(1)) != nil {
			t.Error("the count stayed, or the row came back though deleted again")
		}
	})
}

// A panic in OnTrack, which the application gives, leaves the tracker usable.
func TestAddedPanicInOnTrackLeavesTheTrackerUnlocked(t *testing.T) {
	tr, m := New(), meta.Of[row]()
	tr.OnTrack = func(*Entry) { panic("the application's callback") }

	func() {
		defer func() { _ = recover() }()

		tr.Added(m, unsafe.Pointer(fresh()), nil)
	}()

	done := make(chan struct{})

	go func() {
		tr.Len()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("the tracker stayed locked after the panic")
	}
}

type stamp struct{ at time.Time }

type values struct {
	ID    int64
	F     float64
	F32   float32
	P     *float64
	Arr   []float64
	When  sql.Null[time.Time]
	Stamp stamp
	Doc   map[string]any
}

// A NaN that stays one, and a time at the same instant in another zone, are no
// change, however they are held. Maps and slices inside an any are copied, so
// an edit inside them is one.
func TestChangedValues(t *testing.T) {
	nan := math.NaN()
	at := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	zone := time.FixedZone("x", 3600)

	newValues := func() *values {
		return &values{
			ID:    1,
			F:     nan,
			F32:   float32(nan),
			P:     new(nan),
			Arr:   []float64{1, nan},
			When:  sql.Null[time.Time]{V: at, Valid: true},
			Stamp: stamp{at: at},
			Doc: map[string]any{
				"m":   map[string]any{"z": true},
				"k":   []any{1.0},
				"n":   json.Number("2"),
				"big": json.Number("9007199254740993"),
				"f":   json.Number("2.5"),
			},
		}
	}

	cases := []struct {
		name   string
		change func(*values)
		want   string
	}{
		{"NaN stays NaN", func(*values) {}, ""},
		{"NaN to a number", func(v *values) { v.F = 1 }, "F"},
		{"float32 NaN to a number", func(v *values) { v.F32 = 1 }, "F32"},
		{"NaN through a pointer", func(v *values) { *v.P = 1 }, "P"},
		{"NaN in a slice", func(v *values) { v.Arr[1] = 2 }, "Arr"},
		{"sql.Null time in another zone", func(v *values) { v.When.V = at.In(zone) }, ""},
		{
			"sql.Null time at another instant",
			func(v *values) {
				v.When.V = at.Add(time.Second)
			},
			"When",
		},
		{"unexported time in another zone", func(v *values) { v.Stamp.at = at.In(zone) }, ""},
		{"unexported time at another instant", func(v *values) { v.Stamp.at = at.Add(1) }, "Stamp"},
		{
			"a map inside an any",
			func(v *values) {
				v.Doc["m"].(map[string]any)["z"] = false
			},
			"Doc",
		},
		{"a slice inside an any", func(v *values) { v.Doc["k"].([]any)[0] = 2.0 }, "Doc"},
		{"a nil any", func(v *values) { v.Doc["k"] = nil }, "Doc"},
		{"a json.Number to an equal float64", func(v *values) { v.Doc["n"] = 2.0 }, ""},
		{"a json.Number to an equal int", func(v *values) { v.Doc["n"] = 2 }, ""},
		{"a json.Number to an equal uint8", func(v *values) { v.Doc["n"] = uint8(2) }, ""},
		{"a json.Number to an equal float32", func(v *values) { v.Doc["f"] = float32(2.5) }, ""},
		{"a json.Number to another number", func(v *values) { v.Doc["n"] = 3.0 }, "Doc"},
		{"a json.Number to its string", func(v *values) { v.Doc["n"] = "2" }, "Doc"},
		{
			"a large json.Number to the float64 nearest it",
			func(v *values) {
				v.Doc["big"] = float64(9007199254740993)
			},
			"Doc",
		},
		{
			"a large json.Number to its int64",
			func(v *values) {
				v.Doc["big"] = int64(9007199254740993)
			},
			"",
		},
		{
			"a float64 to an equal json.Number",
			func(v *values) {
				v.Doc["k"] = []any{json.Number("1")}
			},
			"",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			v := newValues()
			e := New().Track(meta.Of[values](), unsafe.Pointer(v), Unchanged)
			c.change(v)

			got := ""

			if names := changed(e); len(names) > 0 {
				got = names[0]
			}

			if got != c.want {
				t.Errorf("changed = %q, want %q", got, c.want)
			}
		})
	}
}

// unsettled returns the entry's histories, empty if it never recorded a write.
func (e *Entry) unsettled() histories {
	if e.pending == nil {
		return histories{}
	}

	return *e.pending
}

// numbered returns n rows with the keys from, from+1, and so on.
func numbered(from int64, n int) []*row {
	out := make([]*row, n)

	for i := range out {
		out[i] = fresh()
		out[i].ID = from + int64(i)
	}

	return out
}

func pointers(rows []*row) []unsafe.Pointer {
	out := make([]unsafe.Pointer, len(rows))

	for i, r := range rows {
		out[i] = unsafe.Pointer(r)
	}

	return out
}

// LoadAll does for the rows of a query what Load does for one: each new row is
// tracked with a snapshot of its own, and a row the tracker holds, or that came
// earlier in the same load, gives way to the first instance.
func TestLoadAllMatchesLoad(t *testing.T) {
	m := meta.Of[row]()

	// More rows than one block, so that blocks of every size are used.
	const count = 3*blockLimit + 17

	one, all := New(), New()

	held := fresh()
	held.ID = 5

	for _, tr := range []*Tracker{one, all} {
		tr.Load(m, unsafe.Pointer(held))
	}

	rows := numbered(1, count)
	rows = append(rows, numbered(40, 3)...) // rows that came earlier in the load

	want := make([]unsafe.Pointer, len(rows))

	for i, p := range pointers(rows) {
		want[i] = one.Load(m, p)
	}

	got := pointers(rows)
	all.LoadAll(m, got)

	if !slices.Equal(got, want) {
		t.Fatal("LoadAll resolved rows to other instances than Load")
	}

	if got[4] != unsafe.Pointer(held) || got[count] != unsafe.Pointer(rows[39]) {
		t.Error("a row that was tracked did not give way to the first instance")
	}

	if one.Len() != all.Len() || all.Len() != count {
		t.Fatalf("tracked %d rows, Load tracked %d, want %d", all.Len(), one.Len(), count)
	}

	entries, wantEntries := all.Entries(), one.Entries()

	for i, e := range entries {
		w := wantEntries[i]

		if e.Entity != w.Entity || e.seq != w.seq || e.State != Unchanged || !e.keyed ||
			e.id != w.id {
			t.Fatalf("entry %d = %+v, want %+v", i, e, w)
		}

		if e.snapshot == nil || e.snapshot == e.Entity || e.Dirty() || e.KeyChanged() {
			t.Fatalf("entry %d has no clean snapshot of its own", i)
		}

		if all.Get(e.Entity) != e || all.Find(m, e.id.value()) != e {
			t.Fatalf("entry %d is not found by address and by key", i)
		}
	}

	// Snapshots lie side by side in a block: a change to one row must show in
	// that row alone, through a pointer and a slice too.
	for _, i := range []int{0, 7, 8, blockLimit, len(entries) - 1} {
		r := (*row)(entries[i].Entity)
		r.Name, *r.Bio, r.Tags[0], r.Home.Tags[0] = "n", "n", "n", "n"
		r.Attrs["g"] = "n"
	}

	for i, e := range entries {
		dirty := slices.Contains([]int{0, 7, 8, blockLimit, len(entries) - 1}, i)

		if e.Dirty() != dirty {
			t.Fatalf("entry %d: dirty = %v", i, e.Dirty())
		}

		if dirty && !slices.Equal(changed(e), []string{"Name", "Bio", "Tags", "Home", "Attrs"}) {
			t.Fatalf("entry %d: changed = %v", i, changed(e))
		}

		if original := (*row)(e.Scratch()); original.Name != "a" || *original.Bio != "b" ||
			original.Tags[0] != "d" || original.Peers != nil {
			t.Fatalf("entry %d: snapshot = %+v", i, original)
		}
	}
}

func TestLoadAllSkipsWhatHasNoKey(t *testing.T) {
	type keyless struct{ Name string }

	tr := New()

	v := &keyless{"a"}
	loaded := []unsafe.Pointer{unsafe.Pointer(v)}
	tr.LoadAll(meta.Of[keyless](), loaded)

	if loaded[0] != unsafe.Pointer(v) || tr.Len() != 0 {
		t.Error("keyless entity was tracked")
	}

	type optional struct {
		ID   *int64
		Name string
	}

	// A NULL key is no key, and the rows after it are still tracked.
	rows := []*optional{{Name: "a"}, {ID: new(int64(2))}}
	loaded = []unsafe.Pointer{unsafe.Pointer(rows[0]), unsafe.Pointer(rows[1])}
	tr.LoadAll(meta.Of[optional](), loaded)

	if tr.Len() != 1 || tr.Get(unsafe.Pointer(rows[1])) == nil {
		t.Errorf("tracked %d rows, want the one with a key", tr.Len())
	}

	tr.LoadAll(meta.Of[row](), nil) // nothing to load is no error
}

// Keys of every kind find their row, and a key of one model is not a key of
// another.
func TestFirstInstancesByKeyKind(t *testing.T) {
	type named struct {
		ID   string
		Name string
	}

	type timed struct {
		ID   time.Time
		Name string
	}

	tr := New()
	at := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	a, b, c := fresh(), &named{ID: "1"}, &timed{ID: at}
	tr.Load(meta.Of[row](), unsafe.Pointer(a))
	tr.Load(meta.Of[named](), unsafe.Pointer(b))
	tr.Load(meta.Of[timed](), unsafe.Pointer(c))

	if e := tr.Find(meta.Of[row](), int64(1)); e == nil || e.Entity != unsafe.Pointer(a) {
		t.Error("integer key not found")
	}

	if e := tr.Find(meta.Of[named](), "1"); e == nil || e.Entity != unsafe.Pointer(b) {
		t.Error("string key not found")
	}

	if e := tr.Find(meta.Of[timed](), at); e == nil || e.Entity != unsafe.Pointer(c) {
		t.Error("time key not found")
	}

	if tr.Find(meta.Of[named](), int64(1)) != nil || tr.Find(meta.Of[row](), "1") != nil {
		t.Error("a key of one kind found a row of another")
	}

	tr.Remove(unsafe.Pointer(b))
	tr.Remove(unsafe.Pointer(c))

	if tr.Find(meta.Of[named](), "1") != nil || tr.Find(meta.Of[timed](), at) != nil {
		t.Error("a removed row is still found by key")
	}

	// A second instance takes over nothing while the first is tracked, and a
	// later load is the first once it is gone.
	again := fresh()
	if got := tr.Load(meta.Of[row](), unsafe.Pointer(again)); got != unsafe.Pointer(a) {
		t.Error("second instance replaced the first")
	}

	tr.Remove(unsafe.Pointer(a))

	if got := tr.Load(meta.Of[row](), unsafe.Pointer(again)); got != unsafe.Pointer(again) {
		t.Error("the row was not tracked again once its first instance was gone")
	}
}

// Entries stays oldest first, and holds exactly what is tracked, through
// everything that takes entries out and puts them back.
func TestEntriesFollowEveryChange(t *testing.T) {
	tr, m := New(), meta.Of[row]()

	check := func(when string) {
		t.Helper()

		entries := tr.Entries()
		if len(entries) != tr.Len() {
			t.Fatalf("%s: %d entries, %d tracked", when, len(entries), tr.Len())
		}

		for i, e := range entries {
			if tr.Get(e.Entity) != e {
				t.Fatalf("%s: entry %d is not tracked", when, i)
			}

			if i > 0 && entries[i-1].seq >= e.seq {
				t.Fatalf("%s: entry %d is out of order", when, i)
			}
		}
	}

	rows := numbered(1, 200)
	loaded := pointers(rows)
	tr.LoadAll(m, loaded)
	check("loaded")

	entries := tr.Entries()

	// What Entries returns is the caller's own.
	entries[0] = nil

	check("after the caller changed its copy")

	entries = tr.Entries()

	// Enough forgotten that the order is cleaned on the way, and not.
	var changes []Change

	for i, e := range entries {
		if i%3 != 0 {
			changes = append(changes, tr.Forget(e))
		}
	}

	check("most forgotten")

	// Restored entries come back in their old places, though they are put back
	// newest first.
	for _, c := range slices.Backward(changes) {
		c.Undo()
	}

	check("restored")

	if tr.Len() != 200 {
		t.Fatalf("%d tracked after restoring, want 200", tr.Len())
	}

	// An entry that moved keeps its place.
	moved := fresh()
	moved.ID = 3
	tr.Move(entries[2], unsafe.Pointer(moved))
	check("moved")

	if got := tr.Entries()[2]; got != entries[2] {
		t.Error("a moved entry lost its place")
	}

	// An entry whose address another took is out, and back in once it moves to
	// an address of its own.
	taken := tr.Track(m, entries[5].Entity, Unchanged)
	if taken != entries[5] {
		t.Fatal("tracking a tracked address made a new entry")
	}

	mark := tr.Mark()
	late := numbered(1000, 40)
	tr.LoadAll(m, pointers(late))
	check("more loaded")

	end := tr.Mark()
	after := numbered(2000, 10)
	tr.LoadAll(m, pointers(after))

	tr.ForgetBetween(mark, end)
	check("forgotten between the marks")

	if tr.Len() != 210 {
		t.Fatalf("%d tracked after ForgetBetween, want 210: those after the end stay", tr.Len())
	}

	tr.Clear()
	check("cleared")

	if len(tr.Entries()) != 0 {
		t.Error("entries survived Clear")
	}

	// A restore after Clear tracks the entry again, once.
	tr.Restore(entries[9])
	tr.Restore(entries[9])
	tr.Restore(entries[1])
	check("restored after Clear")

	if got := tr.Entries(); len(got) != 2 || got[0] != entries[1] || got[1] != entries[9] {
		t.Errorf("restored entries = %d, or out of order", len(got))
	}
}

// An entry that never recorded a write holds no history, and one that did
// lets go of what it settled.
func TestHistoriesAreMadeOnTheFirstWrite(t *testing.T) {
	tr, m := New(), meta.Of[row]()

	a := fresh()
	e := tr.Track(m, unsafe.Pointer(a), Unchanged)

	if e.pending != nil || e.Unsettled() {
		t.Fatal("a loaded entry holds histories")
	}

	c := e.SetLinks(map[*meta.Relation]map[any]*Entry{}, map[*meta.Relation]any{})

	if !e.Unsettled() {
		t.Error("an open write to the relations is not unsettled")
	}

	if c.IsZero() || !(Change{}).IsZero() {
		t.Error("IsZero is wrong about a change")
	}

	c.Keep()

	if e.Unsettled() {
		t.Error("a kept write is still unsettled")
	}
}
