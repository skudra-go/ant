package named

import (
	"context"
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"math"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"

	"github.com/skudra-go/ant"
	"github.com/skudra-go/ant/tests/features/fixture"
	"github.com/skudra-go/ant/tests/features/value_handling/probekit"
)

// ---------------------------------------------------------------------------
// named types, custom Valuer / Scanner types
// ---------------------------------------------------------------------------

type (
	prStatus  string
	prLevel   int16
	prBits    uint8
	prBig     uint64
	prCelsius float32
	prFlag    bool
)

// prTemp: BOTH methods on the pointer receiver.
type prTemp struct{ Milli int64 }

func (t *prTemp) Value() (driver.Value, error) {
	if t == nil {
		return nil, nil
	}

	return t.Milli, nil
}

func (t *prTemp) Scan(src any) error {
	n, ok := src.(int64)
	if !ok {
		return fmt.Errorf("prTemp: unexpected %T", src)
	}

	t.Milli = n

	return nil
}

// prUpper: a string that is stored upper-cased (a documented conversion).
type prUpper string

func (u prUpper) Value() (driver.Value, error) { return strings.ToUpper(string(u)), nil }

func (u *prUpper) Scan(src any) error {
	switch s := src.(type) {
	case string:
		*u = prUpper(s)
	case []byte:
		*u = prUpper(s)
	default:
		return fmt.Errorf("prUpper: unexpected %T", src)
	}

	return nil
}

// prLine / prLines: a slice of STRUCTS that is its own JSON column.
type prLine struct {
	SKU string
	Qty int
}

type prLines []prLine

func (l prLines) Value() (driver.Value, error) {
	if l == nil {
		return nil, nil
	}

	b, err := json.Marshal([]prLine(l))

	return string(b), err
}

func (l *prLines) Scan(src any) error {
	var b []byte

	switch s := src.(type) {
	case nil:
		*l = nil

		return nil
	case string:
		b = []byte(s)
	case []byte:
		b = s
	default:
		return fmt.Errorf("prLines: unexpected %T", src)
	}

	return json.Unmarshal(b, (*[]prLine)(l))
}

func TestProbeNamedTypes(t *testing.T) {
	probekit.KnownHere(t)

	probekit.Probe(t, probekit.Ord("named string", "TEXT", prStatus(""), "active", "archived"))
	probekit.Probe(t, probekit.Ord("named int16", "SMALLINT", prLevel(-5), 0, 5))
	probekit.Probe(t, probekit.Ord("named uint8", "SMALLINT", prBits(0), 1, 255))

	probekit.Probe(
		t,
		probekit.Ord("named uint64", "NUMERIC(20,0)", prBig(0), 5, math.MaxInt64, math.MaxUint64),
	)

	probekit.Probe(t, probekit.Ord("named float32", "REAL", prCelsius(-40), 0, 36.6))

	probekit.Probe(t, probekit.Spec[prFlag]{
		Name: "named bool",
		SQL:  "BOOLEAN",
		Vals: []prFlag{false, true},
		Eq:   ant.Eq[prFlag],
		In:   ant.In[prFlag],
	})
}

func TestProbeCustomValuers(t *testing.T) {
	probekit.KnownHere(t)

	probekit.Probe(t, probekit.Spec[probekit.PrMoney]{
		Name: "Money (value Valuer, pointer Scanner)",
		SQL:  "NUMERIC(12,2)",
		Vals: []probekit.PrMoney{
			{Cents: -100050},
			{Cents: -1},
			{Cents: 0},
			{Cents: 1},
			{Cents: 99},
			{Cents: 123456789},
		},
		Eq: ant.Eq[probekit.PrMoney],
		In: ant.In[probekit.PrMoney],
	})

	probekit.Probe(t, probekit.Spec[prTemp]{
		Name: "Temp (pointer Valuer and Scanner, non-pointer field)",
		SQL:  "BIGINT",
		Vals: []prTemp{{-5}, {0}, {7}},
		Eq:   ant.Eq[prTemp],
		In:   ant.In[prTemp],
	})

	probekit.Probe(t, probekit.Spec[probekit.PrOptional]{
		Name:     "Optional (Valuer returns nil for zero)",
		SQL:      "BIGINT",
		Nullable: true,
		Vals:     []probekit.PrOptional{{N: 0}, {N: 1}, {N: 9}},
		IsNull:   func(o probekit.PrOptional) bool { return o.N == 0 },
		Eq:       ant.Eq[probekit.PrOptional],
		In:       ant.In[probekit.PrOptional],
	})

	probekit.Probe(t, probekit.Spec[prUpper]{
		Name: "Upper (stored converted)",
		SQL:  "TEXT",
		Vals: []prUpper{"A", "b", "Cd"},
		Norm: func(u prUpper) prUpper { return prUpper(strings.ToUpper(string(u))) },
		Eq:   ant.Eq[prUpper],
		In:   ant.In[prUpper],
	})

	probekit.Probe(t, probekit.Spec[probekit.PrColor]{
		Name: "Color (int enum stored by name)",
		SQL:  "TEXT",
		Vals: []probekit.PrColor{0, 1, 2},
		Eq:   ant.Eq[probekit.PrColor],
		In:   ant.In[probekit.PrColor],
	})

	probekit.Probe(t, probekit.Spec[probekit.PrTags]{
		Name:     "Tags (slice with Valuer, nil is NULL)",
		SQL:      "JSONB",
		Nullable: true,
		Vals:     []probekit.PrTags{nil, {}, {"a"}, {"a", "b\"\\"}},
		IsNull:   func(t probekit.PrTags) bool { return t == nil },
	})

	probekit.Probe(t, probekit.Spec[prLines]{
		Name:     "Lines (slice of structs with Valuer)",
		SQL:      "JSONB",
		Nullable: true,
		Vals:     []prLines{nil, {}, {{"a", 1}}, {{"a", 1}, {"b", 2}}},
		IsNull:   func(l prLines) bool { return l == nil },
		NoPtr:    true,
	})
}

// ---------------------------------------------------------------------------
// bytes
// ---------------------------------------------------------------------------

type prBlob []byte

func TestProbeBytes(t *testing.T) {
	probekit.KnownHere(t)

	big := make([]byte, 1<<20)
	for i := range big {
		big[i] = byte(i * 7)
	}

	probekit.Probe(t, probekit.Spec[[]byte]{
		Name:     "[]byte (nil is NULL, empty is empty)",
		SQL:      "BYTEA",
		Nullable: true,
		Vals: [][]byte{
			nil, {}, {0}, {0, 0, 0}, {0xff, 0xfe, 'a'}, []byte("it's \\x00 \"q\""), big,
		},
		IsNull: func(b []byte) bool { return b == nil },
	})

	probekit.Probe(t, probekit.Spec[prBlob]{
		Name:  "named []byte",
		SQL:   "BYTEA",
		Vals:  []prBlob{{}, {1}, {0, 255}},
		NoPtr: true,
	})

	probekit.Probe(t, probekit.Spec[prBlob]{
		Name:     "named []byte with NULL",
		SQL:      "BYTEA",
		Nullable: true,
		Vals:     []prBlob{nil, {}, {1}, {0, 255}},
		IsNull:   func(b prBlob) bool { return b == nil },
		NoPtr:    true,
	})

	probekit.Probe(t, probekit.Spec[json.RawMessage]{
		Name: "json.RawMessage in JSONB",
		SQL:  "JSONB",
		Vals: []json.RawMessage{
			json.RawMessage(`{}`),
			json.RawMessage(`[1, 2]`),
			json.RawMessage(`"x"`),
			json.RawMessage(`{"a": [true, null]}`),
			json.RawMessage(`null`),
		},
		NoPtr: true,
	})

	probekit.Probe(t, probekit.Spec[json.RawMessage]{
		Name:     "json.RawMessage with NULL",
		SQL:      "JSONB",
		Nullable: true,
		Vals: []json.RawMessage{
			nil, json.RawMessage(`{}`), json.RawMessage(`[1, 2]`),
		},
		IsNull: func(b json.RawMessage) bool { return b == nil },
		NoPtr:  true,
	})

	probekit.Probe(t, probekit.Spec[string]{
		Name: "string raw JSON text in JSONB",
		SQL:  "JSONB",
		Vals: []string{`{}`, `[1, 2]`, `"x"`, `{"a": [true, null]}`, `null`, `1.5`},
		Same: func(a, b string) bool {
			var x, y any

			_ = json.Unmarshal([]byte(a), &x)
			_ = json.Unmarshal([]byte(b), &y)

			return reflect.DeepEqual(x, y)
		},
		NoPtr: true,
	})
}

// ---------------------------------------------------------------------------
// ant.JSON and ant.Gob
// ---------------------------------------------------------------------------

type prDeep struct {
	Name   string              `json:"name"`
	Tags   []string            `json:"tags"`
	Inner  *probekit.PrPair    `json:"inner"`
	Attrs  map[string]any      `json:"attrs,omitempty"`
	Counts map[string]int      `json:"counts"`
	Times  []time.Time         `json:"times"`
	Raw    json.RawMessage     `json:"raw"`
	Any    any                 `json:"any"`
	Nested map[string][]prDeep `json:"nested,omitempty"`
}

func TestProbeJSON(t *testing.T) {
	probekit.KnownHere(t)

	probekit.Probe(t, probekit.Spec[ant.JSON[probekit.PrPair]]{
		Name: "ant.JSON[comparable struct]",
		SQL:  "JSONB",
		Vals: []ant.JSON[probekit.PrPair]{
			{},
			{V: probekit.PrPair{A: 1}},
			{V: probekit.PrPair{A: 1, B: "x"}},
			{V: probekit.PrPair{A: -1, B: "\u65e5\u672c \"q\" \\ \n <&>"}},
		},
		Eq: ant.Eq[ant.JSON[probekit.PrPair]],
		In: ant.In[ant.JSON[probekit.PrPair]],
	})

	probekit.Probe(t, probekit.Spec[ant.JSON[map[string]any]]{
		Name: "ant.JSON[map[string]any]",
		SQL:  "JSONB",
		Vals: []ant.JSON[map[string]any]{
			{},
			{V: map[string]any{}},
			{V: map[string]any{"a": nil}},
			{V: map[string]any{"": 1.0, "k": "v"}},
			{
				V: map[string]any{
					"a": 1.5,
					"b": []any{1.0, "x", nil, true, map[string]any{}},
					"c": map[string]any{"d": map[string]any{"e": []any{}}},
				},
			},
		},
	})

	probekit.Probe(t, probekit.Spec[ant.JSON[[]int]]{
		Name: "ant.JSON[[]int] (nil vs empty)",
		SQL:  "JSONB",
		Vals: []ant.JSON[[]int]{{}, {V: []int{}}, {V: []int{1, 2}}},
	})

	probekit.Probe(t, probekit.Spec[ant.JSON[*probekit.PrPair]]{
		Name: "ant.JSON[*struct]",
		SQL:  "JSONB",
		Vals: []ant.JSON[*probekit.PrPair]{
			{},
			{V: &probekit.PrPair{}},
			{V: &probekit.PrPair{A: 3, B: "z"}},
		},
	})

	probekit.Probe(t, probekit.Spec[ant.JSON[any]]{
		Name: "ant.JSON[any]",
		SQL:  "JSONB",
		Vals: []ant.JSON[any]{
			{}, {V: 1.0}, {V: "s"}, {V: true}, {V: []any{}}, {V: map[string]any{"k": []any{1.0}}},
		},
	})

	probekit.Probe(t, probekit.Spec[ant.JSON[time.Time]]{
		Name: "ant.JSON[time.Time]",
		SQL:  "JSONB",
		Vals: []ant.JSON[time.Time]{
			{},
			{V: time.Date(2024, 1, 2, 3, 4, 5, 123456789, time.UTC)},
			{V: time.Date(2024, 1, 2, 3, 4, 5, 0, time.FixedZone("", 3600))},
		},
		Same: func(a, b ant.JSON[time.Time]) bool { return a.V.Equal(b.V) },
	})

	probekit.Probe(t, probekit.Spec[ant.JSON[int64]]{
		Name: "ant.JSON[int64] extremes",
		SQL:  "JSONB",
		Vals: []ant.JSON[int64]{{V: math.MinInt64}, {V: 0}, {V: math.MaxInt64}},
		Eq:   ant.Eq[ant.JSON[int64]],
		In:   ant.In[ant.JSON[int64]],
	})

	probekit.Probe(t, probekit.Spec[ant.JSON[prDeep]]{
		Name: "ant.JSON[deep struct]",
		SQL:  "JSONB",
		Vals: []ant.JSON[prDeep]{
			{},
			{V: prDeep{Name: "n", Tags: []string{}, Counts: map[string]int{}}},
			{
				V: prDeep{
					Name:   "full",
					Tags:   []string{"a", "b"},
					Inner:  &probekit.PrPair{A: 1, B: "x"},
					Attrs:  map[string]any{"x": 1.0, "y": []any{"z"}},
					Counts: map[string]int{"a": 1, "b": 2},
					Times:  []time.Time{time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)},
					Raw:    json.RawMessage(`{"r":1}`),
					Any:    "s",
					Nested: map[string][]prDeep{"k": {{Name: "child", Counts: map[string]int{}}}},
				},
			},
		},
		Same: func(a, b ant.JSON[prDeep]) bool {
			x, _ := json.Marshal(a.V)
			y, _ := json.Marshal(b.V)

			var xv, yv any

			_ = json.Unmarshal(x, &xv)
			_ = json.Unmarshal(y, &yv)

			return reflect.DeepEqual(xv, yv)
		},
	})

	probekit.Probe(t, probekit.Spec[ant.Gob[map[string]int]]{
		Name: "ant.Gob[map]",
		SQL:  "BYTEA",
		Vals: []ant.Gob[map[string]int]{
			{V: map[string]int{}}, {V: map[string]int{"a": 1}}, {V: map[string]int{"": -1}},
		},
	})

	probekit.Probe(t, probekit.Spec[ant.Gob[probekit.PrPair]]{
		Name: "ant.Gob[struct]",
		SQL:  "BYTEA",
		Vals: []ant.Gob[probekit.PrPair]{
			{},
			{V: probekit.PrPair{A: 1}},
			{V: probekit.PrPair{A: 2, B: "q"}},
		},
		Eq: ant.Eq[ant.Gob[probekit.PrPair]],
	})

	probekit.Probe(t, probekit.Spec[ant.Gob[[]string]]{
		Name: "ant.Gob[[]string]",
		SQL:  "BYTEA",
		Vals: []ant.Gob[[]string]{{V: []string{"a"}}, {V: []string{"", "b"}}},
	})
}

// A value JSON cannot encode, or that PostgreSQL's jsonb cannot hold, must be an
// error and leave nothing behind.
func TestProbeUnstorableValues(t *testing.T) {
	probekit.KnownHere(t)

	check := func(t *testing.T, name string, insert func(ctx context.Context, db *probekit.PrDB[ant.JSON[any]]) error) {
		ddl := `CREATE TABLE ` + probekit.PrTable[ant.JSON[any]](
			"row",
		) + ` (id BIGSERIAL PRIMARY KEY, v JSONB NOT NULL)`

		t.Run(name, func(t *testing.T) {
			probekit.KnownHere(t)

			fixture.RunWith(t, ddl, func(t *testing.T, db *probekit.PrDB[ant.JSON[any]]) {
				ctx := t.Context()

				err := insert(ctx, db)
				if err == nil {
					t.Errorf("no error for a value that cannot be stored")
				}

				n, cerr := db.Rows.Count(ctx)
				require.NoError(t, cerr)
				require.Zero(t, n, "a failed insert left rows behind")
			})
		})
	}

	one := func(v any) func(context.Context, *probekit.PrDB[ant.JSON[any]]) error {
		return func(ctx context.Context, db *probekit.PrDB[ant.JSON[any]]) error {
			return db.Rows.Insert(ctx, &probekit.PrRow[ant.JSON[any]]{V: ant.JSON[any]{V: v}})
		}
	}

	check(t, "NaN in JSON", one(math.NaN()))
	check(t, "Inf in JSON", one(math.Inf(1)))
	check(t, "channel in JSON", one(make(chan int)))
	check(t, "NUL in JSON string (jsonb rejects \\u0000)", one("a\x00b"))
}

// ---------------------------------------------------------------------------
// third-party types: uuid, decimal, native enum
// ---------------------------------------------------------------------------

func TestProbeThirdParty(t *testing.T) {
	probekit.KnownHere(t)

	u := func(s string) uuid.UUID { return uuid.MustParse(s) }

	probekit.Probe(t, probekit.Spec[uuid.UUID]{
		Name: "uuid.UUID",
		SQL:  "UUID",
		Vals: []uuid.UUID{
			{},
			u("00000000-0000-0000-0000-000000000001"),
			u("7f000000-0000-4000-8000-000000000000"),
			u("ffffffff-ffff-ffff-ffff-ffffffffffff"),
		},
		Eq: ant.Eq[uuid.UUID],
		In: ant.In[uuid.UUID],
	})

	probekit.Probe(t, probekit.Spec[uuid.NullUUID]{
		Name:     "uuid.NullUUID",
		SQL:      "UUID",
		Nullable: true,
		Vals: []uuid.NullUUID{
			{},
			{UUID: uuid.UUID{}, Valid: true},
			{UUID: u("7f000000-0000-4000-8000-000000000000"), Valid: true},
		},
		IsNull: func(n uuid.NullUUID) bool { return !n.Valid },
		Eq:     ant.Eq[uuid.NullUUID],
		In:     ant.In[uuid.NullUUID],
	})

	probekit.Probe(t, probekit.Ord(
		"string in a uuid column",
		"UUID",
		"00000000-0000-0000-0000-000000000000",
		"00000000-0000-0000-0000-000000000001",
		"7f000000-0000-4000-8000-000000000000",
		"ffffffff-ffff-ffff-ffff-ffffffffffff",
	))

	d := func(s string) decimal.Decimal { return decimal.RequireFromString(s) }

	probekit.Probe(t, probekit.Spec[decimal.Decimal]{
		Name: "decimal.Decimal",
		SQL:  "NUMERIC(40,10)",
		Vals: []decimal.Decimal{
			d("-12345678901234567890.0123456789"),
			d("-0.0000000001"),
			d("0"),
			d("0.1"),
			d("1.5"),
			d("99999999999999999999.9999999999"),
		},
		Same: func(a, b decimal.Decimal) bool { return a.Equal(b) },
		Eq:   ant.Eq[decimal.Decimal],
		In:   ant.In[decimal.Decimal],
	})

	probekit.Probe(t, probekit.Spec[decimal.NullDecimal]{
		Name:     "decimal.NullDecimal",
		SQL:      "NUMERIC(20,4)",
		Nullable: true,
		Vals: []decimal.NullDecimal{
			{}, {Decimal: d("0"), Valid: true}, {Decimal: d("1.25"), Valid: true},
		},
		IsNull: func(n decimal.NullDecimal) bool { return !n.Valid },
		Same: func(a, b decimal.NullDecimal) bool {
			return a.Valid == b.Valid && a.Decimal.Equal(b.Decimal)
		},
		Eq: ant.Eq[decimal.NullDecimal],
		In: ant.In[decimal.NullDecimal],
	})

	probekit.Probe(t, probekit.Spec[string]{
		Name: "string in NUMERIC(10,2) (reads back as PG formats it)",
		SQL:  "NUMERIC(10,2)",
		Vals: []string{"-1.50", "0.00", "0.10", "3.14"},
		Eq:   ant.Eq[string],
		In:   ant.In[string],
	})

	probekit.Probe(t, probekit.Spec[float64]{
		Name: "float64 in NUMERIC(10,2)",
		SQL:  "NUMERIC(10,2)",
		Vals: []float64{-1.5, 0, 0.1, 3.14},
		Eq:   ant.Eq[float64],
		In:   ant.In[float64],
		Gt:   ant.Gt[float64],
	})

	probekit.Probe(t, probekit.Spec[int64]{
		Name: "int64 in NUMERIC(30,0)",
		SQL:  "NUMERIC(30,0)",
		Vals: []int64{math.MinInt64, 0, math.MaxInt64},
		Eq:   ant.Eq[int64],
		In:   ant.In[int64],
		Gt:   ant.Gt[int64],
	})
}

const prEnumDDL = `CREATE TYPE pr_mood AS ENUM ('sad', 'ok', 'happy');
	CREATE TABLE pr_moods (id BIGSERIAL PRIMARY KEY, v pr_mood NOT NULL, p pr_mood)`

type prMood struct {
	ID int64
	V  prStatus
	P  *prStatus
}

type prMoodDB struct {
	ant.Context
	Moods ant.Table[prMood]
}

func TestProbeNativeEnum(t *testing.T) {
	probekit.KnownHere(t)

	fixture.RunWith(t, prEnumDDL, func(t *testing.T, db *prMoodDB) {
		ctx := t.Context()
		f := ant.Fields[prMood]()
		ok := prStatus("ok")

		a, b, c := &prMood{V: "sad"}, &prMood{V: "ok", P: &ok}, &prMood{V: "happy", P: &ok}
		require.NoError(t, db.Moods.Insert(ctx, a, b, c))

		got, err := db.Moods.NoTracking().OrderBy(&f.ID).All(ctx)
		require.NoError(t, err)
		require.Len(t, got, 3)
		require.Equal(t, prStatus("happy"), got[2].V)
		require.Nil(t, got[0].P)
		require.Equal(t, ok, *got[1].P)

		n, err := db.Moods.Where(ant.Eq(&f.V, "ok")).Count(ctx)
		require.NoError(t, err)
		require.EqualValues(t, 1, n)

		n, err = db.Moods.Where(ant.In(&f.V, "sad", "happy")).Count(ctx)
		require.NoError(t, err)
		require.EqualValues(t, 2, n)

		n, err = db.Moods.Where(ant.Eq(&f.P, &ok)).Count(ctx)
		require.NoError(t, err)
		require.EqualValues(t, 2, n)

		n, err = db.Moods.Where(ant.IsNull(&f.P)).Count(ctx)
		require.NoError(t, err)
		require.EqualValues(t, 1, n)

		n, err = db.Moods.Where(ant.Gt(&f.V, "ok")).Count(ctx)
		require.NoError(t, err)
		require.EqualValues(t, 1, n, "enum order, not text order")

		cnt, err := db.Moods.Where(ant.Eq(&f.ID, a.ID)).
			Update(ctx, ant.Set(&f.V, "happy"), ant.Set(&f.P, &ok))
		require.NoError(t, err)
		require.EqualValues(t, 1, cnt)

		ok2, err := db.Moods.Upsert(ctx, &prMood{ID: a.ID, V: "ok"}, ant.OnConflict(&f.ID))
		require.NoError(t, err)
		require.True(t, ok2)

		n, err = db.Moods.Where(ant.Eq(&f.V, "ok"), ant.IsNull(&f.P)).Count(ctx)
		require.NoError(t, err)
		require.EqualValues(t, 1, n)
	})
}
