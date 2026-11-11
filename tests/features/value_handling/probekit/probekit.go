// Package probekit is the harness the value-handling probes share: a table per
// Go type, and the checks every type goes through. The probes are split into
// packages of their own so that they compile in parallel.
package probekit

import (
	"cmp"
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"os"
	"reflect"
	"regexp"
	"runtime/debug"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/skudra-go/ant"
	"github.com/skudra-go/ant/tests/features/fixture"
)

// ---------------------------------------------------------------------------
// Generic scalar harness: one table per Go type V, with a NOT NULL / nullable
// column v, and a second table with a nullable pointer column p.
// ---------------------------------------------------------------------------

type PrRow[V any] struct {
	ID int64
	V  V
}

func (*PrRow[V]) Configure(b *ant.ModelBuilder[PrRow[V]]) { b.Table(PrTable[V]("row")) }

type PrNRow[V any] struct {
	ID int64
	P  *V
}

func (*PrNRow[V]) Configure(b *ant.ModelBuilder[PrNRow[V]]) { b.Table(PrTable[V]("nrow")) }

type PrDB[V any] struct {
	ant.Context
	Rows  ant.Table[PrRow[V]]
	NRows ant.Table[PrNRow[V]]
}

func PrTable[V any](kind string) string {
	full := reflect.TypeFor[V]().String()
	h := fnv.New32a()
	h.Write([]byte(full))

	clean := strings.Map(
		func(r rune) rune {
			switch {
			case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
				return r
			case r >= 'A' && r <= 'Z':
				return r + 32
			}

			return '_'
		},
		full,
	)

	if len(clean) > 30 {
		clean = clean[:30]
	}

	return fmt.Sprintf("pr_%s_%s_%08x", kind, clean, h.Sum32())
}

// Spec describes one Go type and the column it is stored in.
type Spec[V any] struct {
	Name     string
	SQL      string // column type
	Vals     []V    // distinct, ascending in the database's order
	Nullable bool   // the column allows NULL (some vals may be stored as NULL)
	IsNull   func(V) bool
	Same     func(a, b V) bool // default reflect.DeepEqual
	Norm     func(V) V         // documented conversion applied on write
	Eq       func(*V, V) ant.Cond
	In       func(*V, ...V) ant.Cond
	Gt       func(*V, V) ant.Cond
	NoPtr    bool // skip the pointer-column phase
	NoEq     bool // the database type has no = operator
}

func (s Spec[V]) ddl() string {
	null := " NOT NULL"

	if s.Nullable {
		null = ""
	}

	return fmt.Sprintf(
		`CREATE TABLE %s (id BIGSERIAL PRIMARY KEY, v %s%s);
		 CREATE TABLE %s (id BIGSERIAL PRIMARY KEY, p %s)`,
		PrTable[V]("row"),
		s.SQL,
		null,
		PrTable[V]("nrow"),
		s.SQL,
	)
}

func (s Spec[V]) equal(a, b V) bool {
	if s.Same != nil {
		return s.Same(a, b)
	}

	return reflect.DeepEqual(a, b)
}

func (s Spec[V]) want(v V) V {
	if s.Norm != nil {
		return s.Norm(v)
	}

	return v
}

func (s Spec[V]) null(v V) bool { return s.IsNull != nil && s.IsNull(v) }

// PrStep runs fn as a subtest, turning a panic into a failure, and reports whether
// it passed.
func PrStep(t *testing.T, name string, fn func(t *testing.T)) bool {
	t.Helper()

	return t.Run(name, func(t *testing.T) {
		KnownHere(t)

		defer func() {
			if r := recover(); r != nil {
				t.Errorf("PANIC: %v\n%s", r, debug.Stack())
			}
		}()

		fn(t)
	})
}

// Probe runs every phase for one type.
func Probe[V any](t *testing.T, s Spec[V]) {
	t.Helper()

	// The test that calls probe checks nothing after it, so it runs in parallel
	// with the others.
	fixture.Parallel(t)

	t.Run(s.Name, func(t *testing.T) {
		KnownHere(t)

		// Each phase is a schema of its own, so the phases run in parallel too.
		phase := func(name string, run func(t *testing.T)) {
			t.Run(name, func(t *testing.T) {
				t.Parallel()
				run(t)
			})
		}

		phase("crud", func(t *testing.T) {
			fixture.RunWith(t, s.ddl(), func(t *testing.T, db *PrDB[V]) { s.crud(t, db) })
		})

		if !s.NoPtr {
			phase("pointers", func(t *testing.T) {
				fixture.RunWith(t, s.ddl(), func(t *testing.T, db *PrDB[V]) { s.pointers(t, db) })
			})
		}

		phase("tracked", func(t *testing.T) {
			fixture.RunTrackedWith(t, s.ddl(), func(t *testing.T, db *PrDB[V]) { s.tracked(t, db) })
		})

		if !s.NoPtr {
			phase("pointers tracked", func(t *testing.T) {
				fixture.RunTrackedWith(t, s.ddl(), func(t *testing.T, db *PrDB[V]) {
					s.pointersTracked(t, db)
				})
			})
		}
	})
}

func idsOf[V any](rows []*PrRow[V]) []int64 {
	out := make([]int64, len(rows))

	for i, r := range rows {
		out[i] = r.ID
	}

	return out
}

func (s Spec[V]) crud(t *testing.T, db *PrDB[V]) {
	ctx := t.Context()
	f := ant.Fields[PrRow[V]]()
	n := len(s.Vals)
	ids := make([]int64, n)

	find := func(t *testing.T, id int64) V {
		t.Helper()

		got, err := db.Rows.NoTracking().Find(ctx, id)
		require.NoError(t, err, "Find %d", id)

		return got.V
	}

	idsWhere := func(t *testing.T, c ...ant.Cond) []int64 {
		t.Helper()

		rows, err := db.Rows.NoTracking().Where(c...).OrderBy(&f.ID).All(ctx)
		require.NoError(t, err)

		return idsOf(rows)
	}

	if !PrStep(t, "insert", func(t *testing.T) {
		KnownHere(t)

		for i, v := range s.Vals {
			e := &PrRow[V]{V: v}
			require.NoError(t, db.Rows.Insert(ctx, e), "insert %#v", v)
			require.NotZero(t, e.ID)

			ids[i] = e.ID

			if !s.equal(e.V, v) {
				t.Errorf("insert changed the entity: wrote %#v, entity now %#v", v, e.V)
			}

			if got := find(t, e.ID); !s.equal(got, s.want(v)) {
				t.Errorf("insert %#v: read back %#v, want %#v", v, got, s.want(v))
			}
		}
	}) {
		return
	}

	PrStep(t, "all", func(t *testing.T) {
		KnownHere(t)

		rows, err := db.Rows.NoTracking().OrderBy(&f.ID).All(ctx)
		require.NoError(t, err)
		require.Len(t, rows, n)

		for i, r := range rows {
			if !s.equal(r.V, s.want(s.Vals[i])) {
				t.Errorf("All[%d]: got %#v, want %#v", i, r.V, s.want(s.Vals[i]))
			}
		}
	})

	if s.Eq != nil {
		PrStep(t, "eq", func(t *testing.T) {
			KnownHere(t)

			for i, v := range s.Vals {
				got := idsWhere(t, s.Eq(&f.V, v))
				if !slices.Equal(got, []int64{ids[i]}) {
					t.Errorf("Eq(%#v): rows %v, want [%d] (val index %d)", v, got, ids[i], i)
				}
			}
		})
	}

	if s.In != nil {
		PrStep(t, "in", func(t *testing.T) {
			KnownHere(t)

			// In is Eq of each value, so a value stored as NULL matches NULL.
			matches := func(v, w V) bool {
				if s.null(v) || s.null(w) {
					return s.null(v) && s.null(w)
				}

				return s.equal(v, w)
			}

			if got := idsWhere(t, s.In(&f.V, s.Vals...)); !slices.Equal(got, ids) {
				t.Errorf(
					"In(all %d values): rows %v, want %v (a NULL matches as in Eq)",
					n,
					got,
					ids,
				)
			}

			if got := idsWhere(t, s.In(&f.V)); len(got) != 0 {
				t.Errorf("In(): rows %v, want none", got)
			}

			if got := idsWhere(t, ant.Not(s.In(&f.V))); len(got) != n {
				t.Errorf("Not(In()): %d rows, want %d", len(got), n)
			}

			if n >= 3 {
				a, b := s.Vals[0], s.Vals[n-1]
				want := []int64{}

				for _, v := range []V{a, b} {
					for i, w := range s.Vals {
						if matches(v, w) {
							want = append(want, ids[i])
						}
					}
				}

				slices.Sort(want)
				want = slices.Compact(want)

				if got := idsWhere(t, s.In(&f.V, a, b)); !slices.Equal(got, want) {
					t.Errorf("In(first,last): rows %v, want %v", got, want)
				}
			}
		})
	}

	if s.Gt != nil {
		PrStep(t, "gt", func(t *testing.T) {
			KnownHere(t)

			for i, v := range s.Vals {
				if s.null(v) {
					continue
				}

				var want []int64

				for j := i + 1; j < n; j++ {
					if !s.null(s.Vals[j]) {
						want = append(want, ids[j])
					}
				}

				if got := idsWhere(t, s.Gt(&f.V, v)); !slices.Equal(got, want) {
					t.Errorf("Gt(%#v): rows %v, want %v", v, got, want)
				}
			}
		})
	}

	PrStep(t, "update", func(t *testing.T) {
		KnownHere(t)

		for i, id := range ids {
			v := s.Vals[(i+1)%n]
			cnt, err := db.Rows.Where(ant.Eq(&f.ID, id)).Update(ctx, ant.Set(&f.V, v))
			require.NoError(t, err, "Set %#v", v)
			require.EqualValues(t, 1, cnt)

			if got := find(t, id); !s.equal(got, s.want(v)) {
				t.Errorf("Update Set(%#v): read back %#v, want %#v", v, got, s.want(v))
			}
		}

		for _, v := range s.Vals {
			cnt, err := db.Rows.Where(ant.IsNotNull(&f.ID)).Update(ctx, ant.Set(&f.V, v))
			require.NoError(t, err, "Set %#v", v)
			require.EqualValues(t, n, cnt)

			rows, err := db.Rows.NoTracking().All(ctx)
			require.NoError(t, err)

			for _, r := range rows {
				if !s.equal(r.V, s.want(v)) {
					t.Errorf("bulk Set(%#v): row %d reads %#v, want %#v", v, r.ID, r.V, s.want(v))
				}
			}

			if s.Eq != nil {
				if got := idsWhere(t, s.Eq(&f.V, v)); len(got) != n {
					t.Errorf("after bulk Set(%#v): Eq finds %d rows, want %d", v, len(got), n)
				}
			}
		}

		v := s.Vals[n-1]
		got, err := db.Rows.Where(ant.Eq(&f.ID, ids[0])).UpdateReturning(ctx, ant.Set(&f.V, v))
		require.NoError(t, err)
		require.Len(t, got, 1)

		if !s.equal(got[0].V, s.want(v)) {
			t.Errorf("UpdateReturning(%#v): got %#v, want %#v", v, got[0].V, s.want(v))
		}
	})

	PrStep(t, "insertmany", func(t *testing.T) {
		KnownHere(t)

		for _, batch := range []int{1000, 2} {
			rows := make([]*PrRow[V], n)

			for i, v := range s.Vals {
				rows[i] = &PrRow[V]{V: v}
			}

			cnt, err := db.Rows.InsertMany(ctx, rows, ant.BatchSize(batch))
			require.NoError(t, err)
			require.EqualValues(t, n, cnt)

			for i, r := range rows {
				require.NotZero(t, r.ID, "InsertMany did not copy back a key (batch %d)", batch)

				if got := find(t, r.ID); !s.equal(got, s.want(s.Vals[i])) {
					t.Errorf(
						"InsertMany(batch %d) %#v: read back %#v, want %#v",
						batch,
						s.Vals[i],
						got,
						s.want(s.Vals[i]),
					)
				}
			}
		}
	})

	PrStep(t, "upsert", func(t *testing.T) {
		KnownHere(t)

		for i := range s.Vals {
			id := int64(1_000_000 + i)
			first, second := s.Vals[i], s.Vals[(i+1)%n]

			e := &PrRow[V]{ID: id, V: first}
			ok, err := db.Rows.Upsert(ctx, e, ant.OnConflict(&f.ID))
			require.NoError(t, err, "upsert insert %#v", first)
			require.True(t, ok)

			if !s.equal(e.V, s.want(first)) {
				t.Errorf("Upsert insert %#v: entity holds %#v, want %#v", first, e.V, s.want(first))
			}

			if got := find(t, id); !s.equal(got, s.want(first)) {
				t.Errorf("Upsert insert %#v: read back %#v", first, got)
			}

			e2 := &PrRow[V]{ID: id, V: second}
			ok, err = db.Rows.Upsert(ctx, e2, ant.OnConflict(&f.ID))
			require.NoError(t, err, "upsert update %#v", second)
			require.True(t, ok)

			if !s.equal(e2.V, s.want(second)) {
				t.Errorf(
					"Upsert update %#v: entity holds %#v, want %#v",
					second,
					e2.V,
					s.want(second),
				)
			}

			if got := find(t, id); !s.equal(got, s.want(second)) {
				t.Errorf("Upsert update %#v: read back %#v, want %#v", second, got, s.want(second))
			}

			e3 := &PrRow[V]{ID: id, V: first}
			ok, err = db.Rows.Upsert(ctx, e3, ant.OnConflict(&f.ID).DoNothing())
			require.NoError(t, err)
			require.False(t, ok, "DoNothing on an existing row")

			if got := find(t, id); !s.equal(got, s.want(second)) {
				t.Errorf("Upsert DoNothing changed the row to %#v", got)
			}
		}

		// InsertMany with a conflict clause overwrites by key.
		rows := make([]*PrRow[V], n)

		for i := range s.Vals {
			rows[i] = &PrRow[V]{ID: int64(1_000_000 + i), V: s.Vals[n-1-i]}
		}

		_, err := db.Rows.InsertMany(ctx, rows, ant.WithConflict(ant.OnConflict(&f.ID)))
		require.NoError(t, err)

		for i, r := range rows {
			if got := find(t, r.ID); !s.equal(got, s.want(s.Vals[n-1-i])) {
				t.Errorf("InsertMany+conflict %#v: read back %#v", s.Vals[n-1-i], got)
			}
		}

		// Conflict.Set and DoUpdate with an explicit column.
		for i := range s.Vals {
			id := int64(1_000_000 + i)
			third := s.Vals[(i+2)%n]
			e := &PrRow[V]{ID: id, V: s.Vals[i]}
			ok, err := db.Rows.Upsert(ctx, e, ant.OnConflict(&f.ID).Set(ant.Set(&f.V, third)))
			require.NoError(t, err)
			require.True(t, ok)

			if got := find(t, id); !s.equal(got, s.want(third)) {
				t.Errorf("Upsert Conflict.Set(%#v): read back %#v", third, got)
			}

			e = &PrRow[V]{ID: id, V: s.Vals[i]}
			ok, err = db.Rows.Upsert(ctx, e, ant.OnConflict(&f.ID).DoUpdate(ant.F(&f.V)))
			require.NoError(t, err)
			require.True(t, ok)

			if got := find(t, id); !s.equal(got, s.want(s.Vals[i])) {
				t.Errorf("Upsert DoUpdate(V) %#v: read back %#v", s.Vals[i], got)
			}
		}
	})

	PrStep(t, "updatemany reload deletereturning", func(t *testing.T) {
		KnownHere(t)

		rows := make([]*PrRow[V], n)

		for i, v := range s.Vals {
			rows[i] = &PrRow[V]{V: v}
		}

		require.NoError(t, db.Rows.Insert(ctx, rows...))

		for i, r := range rows {
			r.V = s.Vals[(i+1)%n]
		}

		cnt, err := db.Rows.UpdateMany(ctx, rows, ant.F(&f.V))
		require.NoError(t, err)
		require.EqualValues(t, n, cnt)

		for i, r := range rows {
			if got := find(t, r.ID); !s.equal(got, s.want(s.Vals[(i+1)%n])) {
				t.Errorf("UpdateMany %#v: read back %#v", s.Vals[(i+1)%n], got)
			}
		}

		// Reload overwrites the entity with the stored (converted) value.
		for i, r := range rows {
			_, err := db.Rows.Where(ant.Eq(&f.ID, r.ID)).Update(ctx, ant.Set(&f.V, s.Vals[i]))
			require.NoError(t, err)
			require.NoError(t, db.Rows.Reload(ctx, r))

			if !s.equal(r.V, s.want(s.Vals[i])) {
				t.Errorf("Reload: entity holds %#v, want %#v", r.V, s.want(s.Vals[i]))
			}
		}

		ids := make([]int64, n)

		for i, r := range rows {
			ids[i] = r.ID
		}

		deleted, err := db.Rows.Where(ant.In(&f.ID, ids...)).DeleteReturning(ctx)
		require.NoError(t, err)
		require.Len(t, deleted, n)

		slices.SortFunc(deleted, func(a, b *PrRow[V]) int { return cmp.Compare(a.ID, b.ID) })

		for i, r := range deleted {
			if !s.equal(r.V, s.want(s.Vals[i])) {
				t.Errorf("DeleteReturning: row %d holds %#v, want %#v", i, r.V, s.want(s.Vals[i]))
			}
		}
	})
}

func (s Spec[V]) tracked(t *testing.T, db *PrDB[V]) {
	ctx := t.Context()
	f := ant.Fields[PrRow[V]]()
	n := len(s.Vals)

	PrStep(t, "savechanges", func(t *testing.T) {
		KnownHere(t)

		e := &PrRow[V]{V: s.Vals[0]}
		require.NoError(t, db.Rows.Insert(ctx, e))

		if fixture.HasChanges(t, db) {
			t.Errorf("HasChanges after Insert of %#v: %s", s.Vals[0], fixture.DebugTracked(t, db))
		}

		for i := 1; i <= n; i++ {
			v := s.Vals[i%n]
			e.V = v

			if n > 1 && !fixture.HasChanges(t, db) {
				t.Errorf("HasChanges is false after changing the field to %#v", v)
			}

			require.NoError(t, db.SaveChanges(ctx), "SaveChanges %#v", v)

			if fixture.HasChanges(t, db) {
				t.Errorf("HasChanges after SaveChanges of %#v: %s", v, fixture.DebugTracked(t, db))
			}

			got, err := db.Rows.NoTracking().Find(ctx, e.ID)
			require.NoError(t, err)

			if !s.equal(got.V, s.want(v)) {
				t.Errorf("SaveChanges %#v: read back %#v, want %#v", v, got.V, s.want(v))
			}
		}
	})

	PrStep(t, "tracked load is clean", func(t *testing.T) {
		KnownHere(t)

		for _, v := range s.Vals {
			require.NoError(t, db.Rows.Insert(ctx, &PrRow[V]{V: v}))
		}

		require.NoError(t, db.ClearTracked(ctx))
		rows, err := db.Rows.OrderBy(&f.ID).All(ctx)
		require.NoError(t, err)

		for _, r := range rows {
			if !fixture.HasChanges(t, db) {
				continue
			}

			t.Errorf(
				"HasChanges right after loading (row %d = %#v): %s",
				r.ID,
				r.V,
				fixture.DebugTracked(t, db),
			)

			break
		}

		require.NoError(t, db.SaveChanges(ctx))
	})
}

func (s Spec[V]) pointers(t *testing.T, db *PrDB[V]) {
	ctx := t.Context()
	f := ant.Fields[PrNRow[V]]()
	n := len(s.Vals)

	idsWhere := func(t *testing.T, c ...ant.Cond) []int64 {
		t.Helper()

		rows, err := db.NRows.NoTracking().Where(c...).OrderBy(&f.ID).All(ctx)
		require.NoError(t, err)

		out := make([]int64, len(rows))

		for i, r := range rows {
			out[i] = r.ID
		}

		return out
	}

	read := func(t *testing.T, id int64) *V {
		t.Helper()

		got, err := db.NRows.NoTracking().Find(ctx, id)
		require.NoError(t, err)

		return got.P
	}

	var (
		nilID int64
		ids   = make([]int64, n)
	)

	if !PrStep(t, "insert", func(t *testing.T) {
		KnownHere(t)

		e := &PrNRow[V]{}
		require.NoError(t, db.NRows.Insert(ctx, e))
		nilID = e.ID

		if p := read(t, nilID); p != nil {
			t.Errorf("nil pointer read back as %#v, want nil", *p)
		}

		for i := range s.Vals {
			v := s.Vals[i]
			e := &PrNRow[V]{P: &v}
			require.NoError(t, db.NRows.Insert(ctx, e), "insert &%#v", v)
			ids[i] = e.ID

			p := read(t, e.ID)
			if p == nil {
				if !s.null(v) {
					t.Errorf("pointer to %#v read back as nil", v)
				}

				continue
			}

			if !s.equal(*p, s.want(v)) {
				t.Errorf("pointer to %#v read back as %#v, want %#v", v, *p, s.want(v))
			}
		}
	}) {
		return
	}

	PrStep(t, "conditions", func(t *testing.T) {
		KnownHere(t)

		nulls := []int64{nilID}

		for i, v := range s.Vals {
			if s.null(v) {
				nulls = append(nulls, ids[i])
			}
		}

		slices.Sort(nulls)

		if got := idsWhere(t, ant.IsNull(&f.P)); !slices.Equal(got, nulls) {
			t.Errorf("IsNull(P): rows %v, want %v", got, nulls)
		}

		if got := idsWhere(t, ant.IsNotNull(&f.P)); len(got) != n+1-len(nulls) {
			t.Errorf("IsNotNull(P): %d rows, want %d", len(got), n+1-len(nulls))
		}

		if got := idsWhere(t, ant.Eq(&f.P, (*V)(nil))); !slices.Equal(got, nulls) {
			t.Errorf("Eq(P, nil): rows %v, want %v", got, nulls)
		}

		if got := idsWhere(t, ant.Ne(&f.P, (*V)(nil))); len(got) != n+1-len(nulls) {
			t.Errorf("Ne(P, nil): %d rows, want %d", len(got), n+1-len(nulls))
		}

		for i := range s.Vals {
			v := s.Vals[i]
			if s.null(v) || s.NoEq {
				continue
			}

			if got := idsWhere(t, ant.Eq(&f.P, &v)); !slices.Equal(got, []int64{ids[i]}) {
				t.Errorf("Eq(P, &%#v): rows %v, want [%d]", v, got, ids[i])
			}
		}

		if n >= 2 && !s.null(s.Vals[0]) && !s.null(s.Vals[n-1]) && !s.NoEq {
			a, b := s.Vals[0], s.Vals[n-1]
			if got := idsWhere(
				t,
				ant.In(&f.P, &a, &b),
			); !slices.Equal(
				got,
				[]int64{ids[0], ids[n-1]},
			) {
				t.Errorf("In(P, &first, &last): rows %v, want [%d %d]", got, ids[0], ids[n-1])
			}

			if got := idsWhere(t, ant.In(&f.P, &a, nil)); !slices.Equal(got, []int64{ids[0]}) {
				t.Logf(
					"NOTE In(P, &first, nil): rows %v (NULL row %d, first %d)",
					got,
					nilID,
					ids[0],
				)
			}
		}

		if got := idsWhere(t, ant.In(&f.P)); len(got) != 0 {
			t.Errorf("In(P) empty: rows %v", got)
		}
	})

	PrStep(t, "update", func(t *testing.T) {
		KnownHere(t)

		// A value to NULL, and NULL to a value.
		cnt, err := db.NRows.Where(ant.Eq(&f.ID, ids[0])).Update(ctx, ant.Set(&f.P, (*V)(nil)))
		require.NoError(t, err)
		require.EqualValues(t, 1, cnt)

		if p := read(t, ids[0]); p != nil {
			t.Errorf("Set(P, nil): read back %#v, want nil", *p)
		}

		v := s.Vals[n-1]
		cnt, err = db.NRows.Where(ant.Eq(&f.ID, nilID)).Update(ctx, ant.Set(&f.P, &v))
		require.NoError(t, err)
		require.EqualValues(t, 1, cnt)

		if p := read(t, nilID); p == nil || !s.equal(*p, s.want(v)) {
			t.Errorf("Set(P, &%#v): read back %v", v, p)
		}
	})

	PrStep(t, "insertmany", func(t *testing.T) {
		KnownHere(t)

		rows := []*PrNRow[V]{{}}

		for i := range s.Vals {
			rows = append(rows, &PrNRow[V]{P: &s.Vals[i]}, &PrNRow[V]{})
		}

		cnt, err := db.NRows.InsertMany(ctx, rows, ant.BatchSize(3))
		require.NoError(t, err)
		require.EqualValues(t, len(rows), cnt)

		for _, r := range rows {
			p := read(t, r.ID)

			switch {
			case r.P == nil && p != nil:
				t.Errorf("InsertMany: nil pointer read back as %#v", *p)
			case r.P != nil && p == nil && !s.null(*r.P):
				t.Errorf("InsertMany: pointer to %#v read back as nil", *r.P)
			case r.P != nil && p == nil:
			case r.P != nil && !s.equal(*p, s.want(*r.P)):
				t.Errorf("InsertMany: pointer to %#v read back as %#v", *r.P, *p)
			}
		}
	})

	PrStep(t, "upsert", func(t *testing.T) {
		KnownHere(t)

		v := s.Vals[n-1]
		e := &PrNRow[V]{ID: 5_000_000, P: &v}
		ok, err := db.NRows.Upsert(ctx, e, ant.OnConflict(&f.ID))
		require.NoError(t, err)
		require.True(t, ok)

		e2 := &PrNRow[V]{ID: 5_000_000}
		ok, err = db.NRows.Upsert(ctx, e2, ant.OnConflict(&f.ID))
		require.NoError(t, err)
		require.True(t, ok)

		if e2.P != nil {
			t.Errorf("Upsert with a nil pointer: entity holds %#v", *e2.P)
		}

		if p := read(t, 5_000_000); p != nil {
			t.Errorf("Upsert with a nil pointer over a value: read back %#v, want nil", *p)
		}
	})
}

// ---------------------------------------------------------------------------
// helpers for building specs
// ---------------------------------------------------------------------------

func Ord[V cmp.Ordered](name, sqlType string, vals ...V) Spec[V] {
	return Spec[V]{
		Name: name,
		SQL:  sqlType,
		Vals: vals,
		Eq:   ant.Eq[V],
		In:   ant.In[V],
		Gt:   ant.Gt[V],
	}
}

func FloatSame[V ~float32 | ~float64](a, b V) bool {
	return a == b || (a != a && b != b)
}

func TimeSame(a, b time.Time) bool { return a.Equal(b) }

// ---------------------------------------------------------------------------
// tracked pointers
// ---------------------------------------------------------------------------

func (s Spec[V]) pointersTracked(t *testing.T, db *PrDB[V]) {
	ctx := t.Context()
	n := len(s.Vals)

	read := func(t *testing.T, id int64) *V {
		t.Helper()

		got, err := db.NRows.NoTracking().Find(ctx, id)
		require.NoError(t, err)

		return got.P
	}

	PrStep(t, "nil to value to nil to value in place", func(t *testing.T) {
		KnownHere(t)

		e := &PrNRow[V]{}
		require.NoError(t, db.NRows.Insert(ctx, e))
		require.False(t, fixture.HasChanges(t, db))

		for i := range s.Vals {
			v := s.Vals[i]
			e.P = &v
			require.True(t, fixture.HasChanges(t, db), "nil -> &%#v not seen as a change", v)
			require.NoError(t, db.SaveChanges(ctx), "SaveChanges &%#v", v)

			if fixture.HasChanges(t, db) {
				t.Errorf("HasChanges after saving &%#v: %s", v, fixture.DebugTracked(t, db))
			}

			if p := read(t, e.ID); p == nil && !s.null(v) {
				t.Errorf("saved &%#v, read nil", v)
			} else if p != nil && !s.equal(*p, s.want(v)) {
				t.Errorf("saved &%#v, read %#v", v, *p)
			}

			e.P = nil

			require.True(
				t,
				fixture.HasChanges(t, db) || s.null(v),
				"&%#v -> nil not seen as a change",
				v,
			)

			require.NoError(t, db.SaveChanges(ctx))

			if p := read(t, e.ID); p != nil {
				t.Errorf("saved nil, read %#v", *p)
			}
		}

		// Changing what the pointer points to, with the pointer itself kept.
		if n >= 2 {
			a, b := s.Vals[0], s.Vals[n-1]
			e.P = &a

			require.NoError(t, db.SaveChanges(ctx))

			*e.P = b

			if !s.equal(a, b) && !fixture.HasChanges(t, db) {
				t.Errorf("in-place change of the pointee %#v -> %#v not seen", a, b)
			}

			require.NoError(t, db.SaveChanges(ctx))

			if p := read(t, e.ID); p == nil && !s.null(b) {
				t.Errorf("in-place change saved, read nil")
			} else if p != nil && !s.equal(*p, s.want(b)) {
				t.Errorf("in-place change to %#v saved, read %#v", b, *p)
			}
		}
	})
}

// PrMoney: value-receiver Valuer, pointer-receiver Scanner, text NUMERIC.
type PrMoney struct{ Cents int64 }

func (m PrMoney) Value() (driver.Value, error) {
	sign, c := "", m.Cents
	if c < 0 {
		sign, c = "-", -c
	}

	return fmt.Sprintf("%s%d.%02d", sign, c/100, c%100), nil
}

func (m *PrMoney) Scan(src any) error {
	text, ok := src.(string)
	if !ok {
		return fmt.Errorf("prMoney: unexpected %T", src)
	}

	neg := strings.HasPrefix(text, "-")
	whole, frac, _ := strings.Cut(strings.TrimPrefix(text, "-"), ".")

	w, err := strconv.ParseInt(whole, 10, 64)
	if err != nil {
		return err
	}

	f, err := strconv.ParseInt(frac, 10, 64)
	if err != nil {
		return err
	}

	m.Cents = w*100 + f

	if neg {
		m.Cents = -m.Cents
	}

	return nil
}

// PrOptional: stores 0 as NULL, and scans NULL back as 0.
type PrOptional struct{ N int64 }

func (o PrOptional) Value() (driver.Value, error) {
	if o.N == 0 {
		return nil, nil
	}

	return o.N, nil
}

func (o *PrOptional) Scan(src any) error {
	if src == nil {
		o.N = 0

		return nil
	}

	n, ok := src.(int64)
	if !ok {
		return fmt.Errorf("prOptional: unexpected %T", src)
	}

	o.N = n

	return nil
}

// PrColor: an int enum stored by name.
type PrColor int

var prColorNames = []string{"red", "green", "blue"}

func (c PrColor) Value() (driver.Value, error) {
	if int(c) < 0 || int(c) >= len(prColorNames) {
		return nil, fmt.Errorf("prColor: %d out of range", int(c))
	}

	return prColorNames[c], nil
}

func (c *PrColor) Scan(src any) error {
	var name string

	switch s := src.(type) {
	case string:
		name = s
	case []byte:
		name = string(s)
	default:
		return fmt.Errorf("prColor: unexpected %T", src)
	}

	i := slices.Index(prColorNames, name)
	if i < 0 {
		return fmt.Errorf("prColor: unknown %q", name)
	}

	*c = PrColor(i)

	return nil
}

// PrTags: a slice type that is its own JSON column.
type PrTags []string

func (t PrTags) Value() (driver.Value, error) {
	if t == nil {
		return nil, nil
	}

	b, err := json.Marshal([]string(t))

	return string(b), err
}

func (t *PrTags) Scan(src any) error {
	var b []byte

	switch s := src.(type) {
	case nil:
		*t = nil

		return nil
	case string:
		b = []byte(s)
	case []byte:
		b = s
	default:
		return fmt.Errorf("prTags: unexpected %T", src)
	}

	return json.Unmarshal(b, (*[]string)(t))
}

type PrPair struct {
	A int
	B string
}

// RefusesInvalidUTF8 checks that inserting v fails before anything is stored.
func RefusesInvalidUTF8[V any](t *testing.T, v V, sqlType string) {
	t.Helper()

	ddl := Spec[V]{SQL: sqlType}.ddl()
	fixture.RunWith(t, ddl, func(t *testing.T, db *PrDB[V]) {
		err := db.Rows.Insert(t.Context(), &PrRow[V]{V: v})
		require.ErrorContains(t, err, "not valid UTF-8")

		n, err := db.Rows.Count(t.Context())
		require.NoError(t, err)
		require.Zero(t, n)
	})
}

// knownProbes are probes that fail for a reason docs/databases.md gives, a limit
// of a driver or of the standard library that ant does not work around, by test
// name. ANT_RUN_KNOWN=1 runs them, to see whether that has changed.
var knownProbes = []struct {
	pattern *regexp.Regexp
	reason  string
}{
	// database/sql's Null[T].Value refuses a uint64 with the high bit set.
	{
		regexp.MustCompile(`^TestProbeNullTypes/sql\.Null\[uint64\]_high_bit/`),
		"a database/sql limit",
	},
	// database/sql cannot scan NULL into a named []byte.
	{
		regexp.MustCompile(`^TestProbeBytes/(named_\[\]byte|json\.RawMessage)_with_NULL/`),
		"a database/sql limit",
	},
	// pgx hands an INTERVAL to database/sql as text, which is no time.Duration.
	{regexp.MustCompile(`^TestProbeOtherNative/time\.Duration_in_INTERVAL/`), "a pgx limit"},
	// database/sql cannot scan NULL into a net.IP, a named []byte.
	{regexp.MustCompile(`^TestProbeOtherNative/net\.IP_in_INET/`), "a database/sql limit"},
}

// KnownHere skips the running test if it fails for a known reason, unless
// ANT_RUN_KNOWN is set.
func KnownHere(t *testing.T) {
	t.Helper()

	if os.Getenv("ANT_RUN_KNOWN") != "" {
		return
	}

	for _, k := range knownProbes {
		if k.pattern.MatchString(t.Name()) {
			t.Skipf("known failure, %s; ANT_RUN_KNOWN=1 runs it", k.reason)
		}
	}
}
