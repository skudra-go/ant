package native

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"math"
	"net"
	"net/netip"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/skudra-go/ant"
	"github.com/skudra-go/ant/dialect/postgres"
	"github.com/skudra-go/ant/secure"
	"github.com/skudra-go/ant/tests/features/fixture"
	"github.com/skudra-go/ant/tests/features/value_handling/probekit"
)

// Named types, as the named package probes them.
type (
	prStatus string
)

// ---------------------------------------------------------------------------
// postgres.Array, postgres.Point, other native types
// ---------------------------------------------------------------------------

func arrayNullable[E string | int | int32 | int64 | float64 | bool](
	name, sqlType string,
	same func(a, b postgres.Array[E]) bool,
	vals []postgres.Array[E],
) probekit.Spec[postgres.Array[E]] {
	return probekit.Spec[postgres.Array[E]]{
		Name:     name,
		SQL:      sqlType,
		Nullable: true,
		Vals:     vals,
		Same:     same,
		IsNull:   func(a postgres.Array[E]) bool { return a == nil },
	}
}

func TestProbeArrays(t *testing.T) {
	probekit.KnownHere(t)

	probekit.Probe(
		t,
		arrayNullable("postgres.Array[string]", "TEXT[]", nil, []postgres.Array[string]{
			nil,
			{},
			{""},
			{"", ""},
			{"a", "b"},
			{
				"NULL",
				"null",
				"a,b",
				"{x}",
				`\`,
				`"`,
				`\"`,
				"  sp  ",
				"\u65e5\u672c",
				"tab\t",
				"nl\n",
				"'",
				"x y,z",
			},
		}),
	)

	probekit.Probe(t, arrayNullable("postgres.Array[int]", "BIGINT[]", nil, []postgres.Array[int]{
		nil, {}, {0}, {-1, 1}, {math.MinInt64, math.MaxInt64},
	}))

	probekit.Probe(
		t,
		arrayNullable("postgres.Array[int32]", "INTEGER[]", nil, []postgres.Array[int32]{
			nil, {}, {math.MinInt32, math.MaxInt32},
		}),
	)

	probekit.Probe(
		t,
		arrayNullable("postgres.Array[int64]", "BIGINT[]", nil, []postgres.Array[int64]{
			nil, {}, {math.MinInt64, 0, math.MaxInt64},
		}),
	)

	probekit.Probe(
		t,
		arrayNullable("postgres.Array[bool]", "BOOLEAN[]", nil, []postgres.Array[bool]{
			nil, {}, {true}, {false, true, false},
		}),
	)

	probekit.Probe(t, arrayNullable(
		"postgres.Array[float64]",
		"DOUBLE PRECISION[]",
		func(a, b postgres.Array[float64]) bool {
			return (a == nil) == (b == nil) && slices.EqualFunc(a, b, probekit.FloatSame[float64])
		},
		[]postgres.Array[float64]{
			nil,
			{},
			{0, 0.1, -1.5},
			{math.Inf(-1), math.MaxFloat64, math.SmallestNonzeroFloat64, math.Inf(1), math.NaN()},
		},
	))

	probekit.Probe(t, probekit.Spec[postgres.Point]{
		Name: "postgres.Point extremes",
		SQL:  "POINT",
		Vals: []postgres.Point{
			{},
			{X: 1.5, Y: -2.5},
			{X: math.MaxFloat64, Y: -math.MaxFloat64},
			{X: math.SmallestNonzeroFloat64, Y: 1e-300},
			{X: 1e21, Y: 123456789.123456789},
		},
		NoEq: true,
	})

	probekit.Probe(t, probekit.Spec[postgres.Point]{
		Name: "postgres.Point with Inf and NaN",
		SQL:  "POINT",
		Vals: []postgres.Point{{X: math.Inf(1), Y: math.Inf(-1)}, {X: math.NaN(), Y: 1}},
		Same: func(a, b postgres.Point) bool {
			return probekit.FloatSame(a.X, b.X) && probekit.FloatSame(a.Y, b.Y)
		},
		NoPtr: true,
	})
}

func TestProbeOtherNative(t *testing.T) {
	probekit.KnownHere(t)

	probekit.Probe(t, probekit.Spec[time.Time]{
		Name: "time.Time in DATE",
		SQL:  "DATE",
		Vals: []time.Time{
			time.Date(1900, 1, 1, 0, 0, 0, 0, time.UTC),
			time.Date(2024, 2, 29, 0, 0, 0, 0, time.UTC),
			time.Date(9999, 12, 31, 0, 0, 0, 0, time.UTC),
		},
		Same: probekit.TimeSame,
		Eq:   ant.Eq[time.Time],
		In:   ant.In[time.Time],
		Gt:   ant.Gt[time.Time],
	})

	probekit.Probe(t, probekit.Spec[time.Duration]{
		Name:  "time.Duration in INTERVAL",
		SQL:   "INTERVAL",
		Vals:  []time.Duration{-time.Hour, 0, time.Microsecond, 36 * time.Hour},
		Eq:    ant.Eq[time.Duration],
		In:    ant.In[time.Duration],
		NoPtr: true,
	})

	probekit.Probe(t, probekit.Spec[float32]{
		Name: "float32 in DOUBLE PRECISION",
		SQL:  "DOUBLE PRECISION",
		Vals: []float32{-1.5, 0, 0.1, 16777217},
		Eq:   ant.Eq[float32],
		In:   ant.In[float32],
		Gt:   ant.Gt[float32],
	})

	probekit.Probe(t, probekit.Spec[float64]{
		Name: "float64 in REAL (lossy by design)",
		SQL:  "REAL",
		Vals: []float64{-1.5, 0, 0.5, 1e10},
		Eq:   ant.Eq[float64],
		In:   ant.In[float64],
		Gt:   ant.Gt[float64],
	})

	probekit.Probe(t, probekit.Spec[netip.Addr]{
		Name: "netip.Addr in INET",
		SQL:  "INET",
		Vals: []netip.Addr{
			netip.MustParseAddr("10.0.0.1"), netip.MustParseAddr("::1"),
		},
		Eq:    ant.Eq[netip.Addr],
		In:    ant.In[netip.Addr],
		NoPtr: true,
	})

	probekit.Probe(t, probekit.Spec[net.IP]{
		Name:     "net.IP in INET",
		SQL:      "INET",
		Nullable: true,
		Vals:     []net.IP{nil, net.ParseIP("10.0.0.1").To4()},
		Same:     func(a, b net.IP) bool { return a.Equal(b) },
		IsNull:   func(ip net.IP) bool { return ip == nil },
		NoPtr:    true,
	})

	probekit.Probe(t, probekit.Spec[map[string]any]{
		Name:     "map[string]any in JSONB (no ant.JSON)",
		SQL:      "JSONB",
		Nullable: true,
		Vals:     []map[string]any{nil, {}, {"a": 1.0, "b": []any{"x"}}},
		IsNull:   func(m map[string]any) bool { return m == nil },
		NoPtr:    true,
	})

	probekit.Probe(t, probekit.Spec[[]string]{
		Name:     "[]string in TEXT[] (raw slice)",
		SQL:      "TEXT[]",
		Nullable: true,
		Vals:     [][]string{nil, {}, {"a", "b"}},
		IsNull:   func(m []string) bool { return m == nil },
		NoPtr:    true,
	})

	probekit.Probe(t, probekit.Spec[[]int64]{
		Name:     "[]int64 in BIGINT[] (raw slice)",
		SQL:      "BIGINT[]",
		Nullable: true,
		Vals:     [][]int64{nil, {}, {1, 2}},
		IsNull:   func(m []int64) bool { return m == nil },
		NoPtr:    true,
	})
}

// ---------------------------------------------------------------------------
// Valuer / Scanner errors
// ---------------------------------------------------------------------------

type prColorRow struct {
	ID int64
	C  probekit.PrColor
}

type prColorDB struct {
	ant.Context
	Rows ant.Table[prColorRow]
}

func TestProbeValuerScannerErrors(t *testing.T) {
	probekit.KnownHere(t)

	const ddl = `CREATE TABLE pr_color_rows (id BIGSERIAL PRIMARY KEY, c TEXT NOT NULL)`

	fixture.RunTrackedWith(t, ddl, func(t *testing.T, db *prColorDB) {
		ctx := t.Context()
		f := ant.Fields[prColorRow]()

		count := func() int64 {
			n, err := db.Rows.Count(ctx)
			require.NoError(t, err)

			return n
		}

		good := &prColorRow{C: 1}
		require.NoError(t, db.Rows.Insert(ctx, good))

		t.Run("Valuer error on Insert", func(t *testing.T) {
			probekit.KnownHere(t)

			a, b := &prColorRow{C: 2}, &prColorRow{C: 99}
			err := db.Rows.Insert(ctx, a, b)
			require.Error(t, err)
			require.ErrorContains(t, err, "out of range")

			if count() != 1 {
				t.Errorf("rows after a failed Insert: %d, want 1", count())
			}

			if a.ID != 0 {
				t.Errorf("rolled-back entity kept key %d", a.ID)
			}
		})

		t.Run("Valuer error on InsertMany", func(t *testing.T) {
			probekit.KnownHere(t)

			rows := []*prColorRow{{C: 0}, {C: 99}}
			_, err := db.Rows.InsertMany(ctx, rows)
			require.Error(t, err)

			if count() != 1 {
				t.Errorf("rows after a failed InsertMany: %d, want 1", count())
			}
		})

		t.Run("Valuer error in conditions and Set", func(t *testing.T) {
			probekit.KnownHere(t)

			_, err := db.Rows.Where(ant.Eq(&f.C, probekit.PrColor(99))).Count(ctx)
			require.Error(t, err)

			_, err = db.Rows.Where(ant.In(&f.C, 1, 99)).Count(ctx)
			require.Error(t, err)

			_, err = db.Rows.Where(ant.Eq(&f.ID, good.ID)).
				Update(ctx, ant.Set(&f.C, probekit.PrColor(99)))
			require.Error(t, err)

			_, err = db.Rows.Upsert(ctx, &prColorRow{ID: good.ID, C: 99}, ant.OnConflict(&f.ID))
			require.Error(t, err)
		})

		t.Run("Valuer error in SaveChanges keeps the entity dirty", func(t *testing.T) {
			probekit.KnownHere(t)

			tracked, err := db.Rows.Find(ctx, good.ID)
			require.NoError(t, err)

			tracked.C = 99

			require.Error(t, db.SaveChanges(ctx))

			if !fixture.HasChanges(t, db) {
				t.Errorf("a failed SaveChanges left the entity looking clean")
			}

			tracked.C = 2

			require.NoError(t, db.SaveChanges(ctx))

			got, err := db.Rows.NoTracking().Find(ctx, good.ID)
			require.NoError(t, err)
			require.Equal(t, probekit.PrColor(2), got.C)
		})

		t.Run("Scanner error on every read path", func(t *testing.T) {
			probekit.KnownHere(t)

			_, err := db.DB().ExecContext(ctx, `INSERT INTO pr_color_rows (c) VALUES ('purple')`)
			require.NoError(t, err)

			_, err = db.Rows.NoTracking().All(ctx)
			require.ErrorContains(t, err, "purple")

			_, err = db.Rows.Where(ant.Eq(&f.ID, good.ID+1)).First(ctx)
			require.ErrorContains(t, err, "purple")

			_, err = db.Rows.NoTracking().Find(ctx, good.ID+1)
			require.ErrorContains(t, err, "purple")

			var yielded int

			var each error

			for _, e := range db.Rows.Query().Each(ctx) {
				if e != nil {
					each = e

					break
				}

				yielded++
			}

			require.ErrorContains(t, each, "purple")

			_, err = ant.Pluck(ctx, db.Rows.Query(), &f.C)
			require.ErrorContains(t, err, "purple")

			_, err = ant.MaxOf(ctx, db.Rows.Query(), &f.ID)
			require.NoError(t, err)
		})
	})
}

// ---------------------------------------------------------------------------
// aggregates and Pluck over typed columns
// ---------------------------------------------------------------------------

func TestProbeAggregates(t *testing.T) {
	probekit.KnownHere(t)

	ddl := `CREATE TABLE ` + probekit.PrTable[int64](
		"row",
	) + ` (id BIGSERIAL PRIMARY KEY, v BIGINT NOT NULL);
		CREATE TABLE ` + probekit.PrTable[int64](
		"nrow",
	) + ` (id BIGSERIAL PRIMARY KEY, p BIGINT)`

	fixture.RunWith(t, ddl, func(t *testing.T, db *probekit.PrDB[int64]) {
		ctx := t.Context()
		f := ant.Fields[probekit.PrRow[int64]]()
		nf := ant.Fields[probekit.PrNRow[int64]]()

		sum, err := ant.SumOf(ctx, db.Rows.Query(), &f.V)
		require.NoError(t, err)
		require.Zero(t, sum)

		mx, err := ant.MaxOf(ctx, db.Rows.Query(), &f.V)
		require.NoError(t, err)
		require.Zero(t, mx)

		_, err = db.Rows.InsertMany(ctx, []*probekit.PrRow[int64]{{V: 1}, {V: 2}, {V: 3}, {V: -4}})
		require.NoError(t, err)

		sum, err = ant.SumOf(ctx, db.Rows.Query(), &f.V)
		require.NoError(t, err)
		require.EqualValues(t, 2, sum)

		avg, err := ant.AvgOf(ctx, db.Rows.Query(), &f.V)
		require.NoError(t, err)
		require.InDelta(t, 0.5, avg, 1e-9)

		mn, err := ant.MinOf(ctx, db.Rows.Query(), &f.V)
		require.NoError(t, err)
		require.EqualValues(t, -4, mn)

		// A sum beyond int64 is an error, not a wrapped number.
		_, err = db.Rows.InsertMany(
			ctx,
			[]*probekit.PrRow[int64]{{V: math.MaxInt64}, {V: math.MaxInt64}},
		)
		require.NoError(t, err)

		if got, err := ant.SumOf(ctx, db.Rows.Query(), &f.V); err == nil {
			t.Errorf("SumOf beyond int64 returned %d with no error", got)
		}

		one, two := int64(1), int64(2)
		require.NoError(
			t,
			db.NRows.Insert(
				ctx,
				&probekit.PrNRow[int64]{},
				&probekit.PrNRow[int64]{P: &one},
				&probekit.PrNRow[int64]{P: &two},
			),
		)

		plucked, err := ant.Pluck(ctx, db.NRows.Query().OrderBy(&nf.ID), &nf.P)
		require.NoError(t, err)
		require.Len(t, plucked, 3)
		require.Nil(t, plucked[0])
		require.EqualValues(t, 1, *plucked[1])
		require.EqualValues(t, 2, *plucked[2])
	})

	timeDDL := `CREATE TABLE ` + probekit.PrTable[time.Time](
		"row",
	) + ` (id BIGSERIAL PRIMARY KEY, v TIMESTAMPTZ NOT NULL)`
	fixture.RunWith(t, timeDDL, func(t *testing.T, db *probekit.PrDB[time.Time]) {
		ctx := t.Context()
		f := ant.Fields[probekit.PrRow[time.Time]]()
		lo := time.Date(1999, 1, 1, 0, 0, 0, 0, time.FixedZone("x", 3600))
		hi := time.Date(2030, 1, 1, 0, 0, 0, 5000, time.UTC)
		_, err := db.Rows.InsertMany(ctx, []*probekit.PrRow[time.Time]{{V: hi}, {V: lo}})
		require.NoError(t, err)

		mn, err := ant.MinOf(ctx, db.Rows.Query(), &f.V)
		require.NoError(t, err)
		require.True(t, mn.Equal(lo), "min %v", mn)

		mx, err := ant.MaxOf(ctx, db.Rows.Query(), &f.V)
		require.NoError(t, err)
		require.True(t, mx.Equal(hi), "max %v", mx)

		zero, err := ant.MaxOf(ctx, db.Rows.Where(ant.Gt(&f.V, hi)), &f.V)
		require.NoError(t, err)
		require.True(t, zero.IsZero(), "no rows gives the zero time, got %v", zero)
	})

	moneyDDL := `CREATE TABLE ` + probekit.PrTable[probekit.PrMoney](
		"row",
	) + ` (id BIGSERIAL PRIMARY KEY, v NUMERIC(12,2) NOT NULL)`
	fixture.RunWith(t, moneyDDL, func(t *testing.T, db *probekit.PrDB[probekit.PrMoney]) {
		ctx := t.Context()
		f := ant.Fields[probekit.PrRow[probekit.PrMoney]]()

		_, err := db.Rows.InsertMany(
			ctx,
			[]*probekit.PrRow[probekit.PrMoney]{
				{V: probekit.PrMoney{Cents: 150}},
				{V: probekit.PrMoney{Cents: -5}},
			},
		)
		require.NoError(t, err)

		got, err := ant.Pluck(ctx, db.Rows.Query().OrderBy(&f.ID), &f.V)
		require.NoError(t, err)
		require.Equal(t, []probekit.PrMoney{{Cents: 150}, {Cents: -5}}, got)
	})
}

// ---------------------------------------------------------------------------
// change tracking over many field types
// ---------------------------------------------------------------------------

type prTrackRow struct {
	ID    int64
	PI    *int64
	PF    *float64
	PI32  *int32
	PT    *time.Time
	PS    *string
	PB    *bool
	Raw   []byte
	Tags  probekit.PrTags
	J     ant.JSON[map[string]any]
	JS    ant.JSON[[]probekit.PrPair]
	N     sql.Null[int64]
	NT    sql.Null[time.Time]
	Arr   postgres.Array[int]
	Money probekit.PrMoney
	Opt   *probekit.PrMoney
	At    time.Time
	D     time.Duration
	F32   float32
	F     float64
	St    prStatus
	Un    ant.UnixTime
}

type prTrackDB struct {
	ant.Context
	Rows ant.Table[prTrackRow]
}

const prTrackDDL = `CREATE TABLE pr_track_rows (
	id BIGSERIAL PRIMARY KEY, pi BIGINT, pf DOUBLE PRECISION, pi32 INTEGER, pt TIMESTAMPTZ,
	ps TEXT, pb BOOLEAN, raw BYTEA, tags JSONB, j JSONB NOT NULL, js JSONB NOT NULL,
	n BIGINT, nt TIMESTAMPTZ, arr BIGINT[], money NUMERIC(12,2) NOT NULL, opt NUMERIC(12,2),
	at TIMESTAMPTZ NOT NULL, d BIGINT NOT NULL, f32 REAL NOT NULL, f DOUBLE PRECISION NOT NULL,
	st TEXT NOT NULL, un BIGINT NOT NULL)`

func prTrackSeed() *prTrackRow {
	return &prTrackRow{
		PI:   new(int64(1)),
		PF:   new(1.5),
		PI32: new(int32(2)),
		PT:   new(time.Date(2024, 1, 2, 3, 4, 5, 0, time.UTC)),
		PS:   new("s"),
		PB:   new(true),
		Raw:  []byte{1, 2, 3},
		Tags: probekit.PrTags{"a", "b"},
		J: ant.JSON[map[string]any]{
			V: map[string]any{"k": []any{1.0, "x"}, "m": map[string]any{"z": true}},
		},
		JS:    ant.JSON[[]probekit.PrPair]{V: []probekit.PrPair{{A: 1, B: "a"}, {A: 2, B: "b"}}},
		N:     sql.Null[int64]{V: 5, Valid: true},
		NT:    sql.Null[time.Time]{V: time.Date(2024, 1, 2, 3, 4, 5, 0, time.UTC), Valid: true},
		Arr:   postgres.Array[int]{1, 2, 3},
		Money: probekit.PrMoney{Cents: 100},
		Opt:   &probekit.PrMoney{Cents: 200},
		At:    time.Date(2024, 5, 6, 7, 8, 9, 0, time.UTC),
		D:     time.Second,
		F32:   1.5,
		F:     2.5,
		St:    "x",
		Un:    ant.UnixTime{Time: time.Unix(1_700_000_000, 0).UTC()},
	}
}

func TestProbeTrackingMutations(t *testing.T) {
	probekit.KnownHere(t)

	mutations := map[string]func(e *prTrackRow){
		"*int64 pointee":       func(e *prTrackRow) { *e.PI = 9 },
		"*float64 pointee":     func(e *prTrackRow) { *e.PF = 9.5 },
		"*int32 pointee":       func(e *prTrackRow) { *e.PI32 = 9 },
		"*time.Time pointee":   func(e *prTrackRow) { *e.PT = e.PT.Add(time.Hour) },
		"*string pointee":      func(e *prTrackRow) { *e.PS = "changed" },
		"*bool pointee":        func(e *prTrackRow) { *e.PB = false },
		"[]byte element":       func(e *prTrackRow) { e.Raw[1] = 99 },
		"[]byte append":        func(e *prTrackRow) { e.Raw = append(e.Raw, 4) },
		"[]byte to empty":      func(e *prTrackRow) { e.Raw = []byte{} },
		"[]byte to nil":        func(e *prTrackRow) { e.Raw = nil },
		"Tags element":         func(e *prTrackRow) { e.Tags[0] = "z" },
		"Tags append":          func(e *prTrackRow) { e.Tags = append(e.Tags, "c") },
		"Tags to nil":          func(e *prTrackRow) { e.Tags = nil },
		"JSON map nested":      func(e *prTrackRow) { e.J.V["m"].(map[string]any)["z"] = false },
		"JSON map slice elem":  func(e *prTrackRow) { e.J.V["k"].([]any)[0] = 2.0 },
		"JSON map new key":     func(e *prTrackRow) { e.J.V["new"] = nil },
		"JSON map to nil":      func(e *prTrackRow) { e.J.V = nil },
		"JSON slice of struct": func(e *prTrackRow) { e.JS.V[1].B = "changed" },
		"JSON slice append": func(e *prTrackRow) {
			e.JS.V = append(e.JS.V, probekit.PrPair{A: 3, B: "c"})
		},
		"Null value": func(e *prTrackRow) { e.N.V = 6 },
		"Null validity": func(e *prTrackRow) {
			e.N = sql.Null[int64]{}
		}, // a NULL keeps no value
		"Null time":      func(e *prTrackRow) { e.NT.V = e.NT.V.Add(time.Second) },
		"Array element":  func(e *prTrackRow) { e.Arr[0] = 100 },
		"Array append":   func(e *prTrackRow) { e.Arr = append(e.Arr, 4) },
		"Money value":    func(e *prTrackRow) { e.Money.Cents++ },
		"*Money pointee": func(e *prTrackRow) { e.Opt.Cents++ },
		"time.Time":      func(e *prTrackRow) { e.At = e.At.Add(time.Microsecond) },
		"Duration":       func(e *prTrackRow) { e.D++ },
		"float32":        func(e *prTrackRow) { e.F32 = 2.5 },
		"float64":        func(e *prTrackRow) { e.F = -0.5 },
		"named string":   func(e *prTrackRow) { e.St = "y" },
		"UnixTime":       func(e *prTrackRow) { e.Un.Time = e.Un.Time.Add(time.Second) },
	}

	noops := map[string]func(e *prTrackRow){
		"*int64 same value": func(e *prTrackRow) { e.PI = new(*e.PI) },
		"*time.Time other zone": func(e *prTrackRow) {
			e.PT = new(e.PT.In(time.FixedZone("z", 7200)))
		},
		"time.Time other zone": func(e *prTrackRow) {
			e.At = e.At.In(time.FixedZone("z", -3600))
		},
		"[]byte equal copy": func(e *prTrackRow) { e.Raw = slices.Clone(e.Raw) },
		"Tags equal copy":   func(e *prTrackRow) { e.Tags = slices.Clone(e.Tags) },
		"JSON equal rebuilt map": func(e *prTrackRow) {
			e.J.V = map[string]any{"k": []any{1.0, "x"}, "m": map[string]any{"z": true}}
		},
		"Null time other zone": func(e *prTrackRow) {
			e.NT.V = e.NT.V.In(time.FixedZone("z", 3600))
		},
		"Array equal copy": func(e *prTrackRow) { e.Arr = slices.Clone(e.Arr) },
		"*Money same":      func(e *prTrackRow) { e.Opt = &probekit.PrMoney{Cents: e.Opt.Cents} },
		"UnixTime other zone": func(e *prTrackRow) {
			e.Un.Time = e.Un.Time.In(time.FixedZone("z", 3600))
		},
	}

	fixture.RunTrackedWith(t, prTrackDDL, func(t *testing.T, db *prTrackDB) {
		ctx := t.Context()

		load := func(t *testing.T) *prTrackRow {
			t.Helper()

			seed := prTrackSeed()
			require.NoError(t, db.Rows.Insert(ctx, seed))
			require.NoError(t, db.ClearTracked(ctx))
			e, err := db.Rows.Find(ctx, seed.ID)
			require.NoError(t, err)

			require.False(
				t,
				fixture.HasChanges(t, db),
				"fresh load: %s",
				fixture.DebugTracked(t, db),
			)

			return e
		}

		for name, mutate := range mutations {
			t.Run("mutate "+name, func(t *testing.T) {
				probekit.KnownHere(t)

				e := load(t)
				mutate(e)

				if !fixture.HasChanges(t, db) {
					t.Fatalf("change not detected")
				}

				if err := db.SaveChanges(ctx); err != nil {
					t.Fatalf("SaveChanges: %v", err)
				}

				if fixture.HasChanges(t, db) {
					t.Errorf("still dirty after SaveChanges: %s", fixture.DebugTracked(t, db))
				}

				got, err := db.Rows.NoTracking().Find(ctx, e.ID)
				require.NoError(t, err)

				// Compare through JSON of the values that matter, and times by instant.
				want, _ := json.Marshal(e)
				have, _ := json.Marshal(got)

				var w, h map[string]any

				_ = json.Unmarshal(want, &w)
				_ = json.Unmarshal(have, &h)

				for k := range w {
					if !reflect.DeepEqual(w[k], h[k]) {
						if tw, ok := w[k].(string); ok {
							a, e1 := time.Parse(time.RFC3339Nano, tw)

							b, e2 := time.Parse(time.RFC3339Nano, fmt.Sprint(h[k]))
							if e1 == nil && e2 == nil && a.Equal(b) {
								continue
							}
						}

						t.Errorf("column %s: saved %v, read %v", k, w[k], h[k])
					}
				}
			})
		}

		for name, mutate := range noops {
			t.Run("no-op "+name, func(t *testing.T) {
				probekit.KnownHere(t)

				e := load(t)
				mutate(e)

				if fixture.HasChanges(t, db) {
					t.Errorf("an equal value counts as a change: %s", fixture.DebugTracked(t, db))
				}
			})
		}
	})
}

// ---------------------------------------------------------------------------
// secure.Value: encrypted columns
// ---------------------------------------------------------------------------

func TestProbeSecure(t *testing.T) {
	probekit.KnownHere(t)
	fixture.Serial(t) // secure.SetKeys is process-wide

	require.NoError(t, secure.SetKeys("probe", map[string][]byte{"probe": make([]byte, 32)}))

	sv := func(s string) secure.Value[string] { return secure.New(s) }

	probekit.Probe(t, probekit.Spec[secure.Value[string]]{
		NoEq: true,
		Name: "secure.Value[string]",
		SQL:  "TEXT",
		Vals: []secure.Value[string]{
			sv(""),
			sv("a"),
			sv("a\x00b"),
			sv("NULL"),
			sv("\U0001F600 \u65e5\u672c"),
			sv(`"'\`),
			sv(strings.Repeat("x", 500_000)),
		},
	})

	// JSON, which seals the value, would put U+FFFD in place of the bytes, so
	// such text is refused, as a TEXT column refuses it.
	probekit.RefusesInvalidUTF8(t, sv("ab\xffcd"), "TEXT")

	probekit.Probe(t, probekit.Spec[secure.Value[int64]]{
		NoEq: true,
		Name: "secure.Value[int64]",
		SQL:  "TEXT",
		Vals: []secure.Value[int64]{
			secure.New(
				int64(math.MinInt64),
			),
			secure.New(int64(0)),
			secure.New(int64(math.MaxInt64)),
		},
	})

	probekit.Probe(t, probekit.Spec[secure.Value[uint64]]{
		NoEq: true,
		Name: "secure.Value[uint64]",
		SQL:  "TEXT",
		Vals: []secure.Value[uint64]{secure.New(uint64(0)), secure.New(uint64(math.MaxUint64))},
	})

	probekit.Probe(t, probekit.Spec[secure.Value[float64]]{
		NoEq: true,
		Name: "secure.Value[float64]",
		SQL:  "TEXT",
		Vals: []secure.Value[float64]{
			secure.New(-math.MaxFloat64), secure.New(0.1), secure.New(math.SmallestNonzeroFloat64),
		},
	})

	probekit.Probe(t, probekit.Spec[secure.Value[[]byte]]{
		NoEq: true,
		Name: "secure.Value[[]byte] (nil vs empty)",
		SQL:  "TEXT",
		Vals: []secure.Value[[]byte]{
			secure.New([]byte(nil)), secure.New([]byte{}), secure.New([]byte{0, 255}),
		},
	})

	probekit.Probe(t, probekit.Spec[secure.Value[time.Time]]{
		NoEq: true,
		Name: "secure.Value[time.Time]",
		SQL:  "TEXT",
		Vals: []secure.Value[time.Time]{
			secure.New(time.Time{}),
			secure.New(time.Date(2024, 1, 2, 3, 4, 5, 123456789, time.UTC)),
			secure.New(time.Date(2024, 1, 2, 3, 4, 5, 0, time.FixedZone("", 3600))),
		},
		Same: func(a, b secure.Value[time.Time]) bool { return a.V.Equal(b.V) },
	})

	probekit.Probe(t, probekit.Spec[secure.Value[map[string]any]]{
		NoEq: true,
		Name: "secure.Value[map[string]any]",
		SQL:  "TEXT",
		Vals: []secure.Value[map[string]any]{
			secure.New(map[string]any(nil)),
			secure.New(map[string]any{}),
			secure.New(map[string]any{"a": 1.0, "b": []any{"x", nil}}),
		},
	})

	probekit.Probe(t, probekit.Spec[secure.Value[probekit.PrPair]]{
		NoEq: true,
		Name: "secure.Value[struct]",
		SQL:  "TEXT",
		Vals: []secure.Value[probekit.PrPair]{
			secure.New(probekit.PrPair{}),
			secure.New(probekit.PrPair{A: 1, B: "x"}),
		},
	})
}

// A secure column is nothing in the clear, whatever the value is.
func TestProbeSecureIsCiphertext(t *testing.T) {
	probekit.KnownHere(t)
	fixture.Serial(t) // secure.SetKeys is process-wide

	require.NoError(t, secure.SetKeys("probe", map[string][]byte{"probe": make([]byte, 32)}))

	ddl := `CREATE TABLE ` + probekit.PrTable[secure.Value[string]](
		"row",
	) + ` (id BIGSERIAL PRIMARY KEY, v TEXT NOT NULL)`
	fixture.RunWith(t, ddl, func(t *testing.T, db *probekit.PrDB[secure.Value[string]]) {
		ctx := t.Context()
		f := ant.Fields[probekit.PrRow[secure.Value[string]]]()
		e := &probekit.PrRow[secure.Value[string]]{V: secure.New("hunter2-secret")}
		require.NoError(t, db.Rows.Insert(ctx, e))

		raw, err := ant.Raw[string](
			db,
			"SELECT v FROM "+probekit.PrTable[secure.Value[string]]("row"),
		).First(ctx)
		require.NoError(t, err)
		require.NotContains(t, *raw, "hunter2")

		// Bulk update, upsert and InsertMany keep it encrypted too.
		_, err = db.Rows.Where(ant.Eq(&f.ID, e.ID)).
			Update(ctx, ant.Set(&f.V, secure.New("hunter3-secret")))
		require.NoError(t, err)

		_, err = db.Rows.Upsert(
			ctx,
			&probekit.PrRow[secure.Value[string]]{ID: e.ID, V: secure.New("hunter4-secret")},
			ant.OnConflict(&f.ID),
		)
		require.NoError(t, err)

		_, err = db.Rows.InsertMany(
			ctx,
			[]*probekit.PrRow[secure.Value[string]]{{V: secure.New("hunter5-secret")}},
		)
		require.NoError(t, err)

		all, err := ant.Pluck(ctx, db.Rows.Query(), &f.V)
		require.NoError(t, err)
		require.Len(t, all, 2)

		var texts []string

		rows, err := db.DB().
			QueryContext(ctx, "SELECT v FROM "+probekit.PrTable[secure.Value[string]]("row"))
		require.NoError(t, err)

		defer rows.Close()

		for rows.Next() {
			var s string
			require.NoError(t, rows.Scan(&s))

			texts = append(texts, s)
		}

		for _, text := range texts {
			require.NotContains(t, text, "hunter")
		}

		// The value does not leak through fmt or JSON.
		require.NotContains(t, fmt.Sprintf("%v %+v %#v", e.V, e, e.V.String()), "hunter")

		js, err := json.Marshal(e)
		require.NoError(t, err)
		require.NotContains(t, string(js), "hunter")
	})
}
