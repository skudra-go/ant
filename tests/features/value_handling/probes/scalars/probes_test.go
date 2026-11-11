package scalars

import (
	"database/sql"
	"math"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/skudra-go/ant"
	"github.com/skudra-go/ant/tests/features/fixture"
	"github.com/skudra-go/ant/tests/features/value_handling/probekit"
)

// ---------------------------------------------------------------------------
// the scalar matrix
// ---------------------------------------------------------------------------

func TestProbeScalars(t *testing.T) {
	probekit.KnownHere(t)

	probekit.Probe(t, probekit.Spec[bool]{
		Name: "bool",
		SQL:  "BOOLEAN",
		Vals: []bool{false, true},
		Eq:   ant.Eq[bool],
		In:   ant.In[bool],
	})

	probekit.Probe(t, probekit.Ord("int8", "SMALLINT", int8(math.MinInt8), -1, 0, 1, math.MaxInt8))

	probekit.Probe(
		t,
		probekit.Ord("int16", "SMALLINT", int16(math.MinInt16), -1, 0, 1, math.MaxInt16),
	)

	probekit.Probe(
		t,
		probekit.Ord("int32", "INTEGER", int32(math.MinInt32), -1, 0, 1, math.MaxInt32),
	)

	probekit.Probe(
		t,
		probekit.Ord("int64", "BIGINT", int64(math.MinInt64), -1, 0, 1, math.MaxInt64),
	)

	probekit.Probe(t, probekit.Ord("int", "BIGINT", math.MinInt, -1, 0, 1, math.MaxInt))
	probekit.Probe(t, probekit.Ord("uint8", "SMALLINT", uint8(0), 1, 127, 128, math.MaxUint8))
	probekit.Probe(t, probekit.Ord("uint16", "INTEGER", uint16(0), 1, 32768, math.MaxUint16))

	probekit.Probe(
		t,
		probekit.Ord("uint32", "BIGINT", uint32(0), 1, math.MaxInt32+1, math.MaxUint32),
	)

	probekit.Probe(t, probekit.Ord(
		"uint64 numeric",
		"NUMERIC(20,0)",
		uint64(0),
		1,
		math.MaxInt64,
		math.MaxInt64+1,
		math.MaxUint64,
	))

	probekit.Probe(t, probekit.Ord(
		"uint numeric",
		"NUMERIC(20,0)",
		uint(0),
		1,
		math.MaxInt64,
		math.MaxInt64+1,
		math.MaxUint64,
	))

	s := probekit.Ord(
		"float64",
		"DOUBLE PRECISION",
		math.Inf(-1),
		-math.MaxFloat64,
		-1.5,
		-math.SmallestNonzeroFloat64,
		0,
		math.SmallestNonzeroFloat64,
		0.1,
		1.5,
		math.MaxFloat64,
		math.Inf(1),
		math.NaN(),
	)
	s.Same = probekit.FloatSame[float64]
	probekit.Probe(t, s)

	s32 := probekit.Ord(
		"float32",
		"REAL",
		float32(math.Inf(-1)),
		-math.MaxFloat32,
		-1.5,
		-math.SmallestNonzeroFloat32,
		0,
		math.SmallestNonzeroFloat32,
		0.1,
		1.5,
		math.MaxFloat32,
		float32(math.Inf(1)),
		float32(math.NaN()),
	)
	s32.Same = probekit.FloatSame[float32]
	probekit.Probe(t, s32)

	str := probekit.Ord(
		"string",
		`TEXT COLLATE "C"`,
		"",
		" ",
		"a",
		"A",
		"a b",
		"NULL",
		"null",
		"'",
		`"`,
		`\`,
		`\\n`,
		"%_",
		"\n\t\r",
		"\u65e5\u672c\u8a9e",
		"emoji \U0001F600\U0001F389\U0001F468\u200d\U0001F469\u200d\U0001F467",
		"e\u0301",
		"\u00e9",
		"\u00a0",
		"\ufeffbom",
		"\u202eRTL",
		strings.Repeat("x", 1<<20),
		strings.Repeat("ab", 70_000)+"\U0001F600",
	)
	slices.Sort(str.Vals)
	probekit.Probe(t, str)

	probekit.Probe(t, probekit.Spec[time.Time]{
		Name: "time.Time timestamptz",
		SQL:  "TIMESTAMPTZ",
		Vals: []time.Time{
			time.Time{},
			time.Date(1900, 1, 1, 0, 0, 0, 0, time.UTC),
			time.Date(1969, 12, 31, 23, 59, 59, 999_999_000, time.UTC),
			time.Unix(0, 0),
			time.Date(2024, 2, 29, 12, 0, 0, 123_456_000, time.FixedZone("x", 5*3600+1800)),
			time.Date(2024, 2, 29, 12, 0, 1, 0, time.FixedZone("x", -8*3600)),
			time.Date(2038, 1, 19, 3, 14, 8, 0, time.UTC),
			time.Date(9999, 12, 31, 23, 59, 59, 999_999_000, time.UTC),
			time.Date(100000, 1, 1, 0, 0, 0, 0, time.UTC),
		},
		Same: probekit.TimeSame,
		Eq:   ant.Eq[time.Time],
		In:   ant.In[time.Time],
		Gt:   ant.Gt[time.Time],
	})

	probekit.Probe(t, probekit.Spec[time.Time]{
		Name: "time.Time with nanoseconds",
		SQL:  "TIMESTAMPTZ",
		Vals: []time.Time{
			time.Date(2024, 1, 1, 0, 0, 0, 123_456_000, time.UTC),
			time.Date(2024, 1, 1, 0, 0, 0, 123_457_000, time.UTC),
		},
		Same: probekit.TimeSame,
		Norm: func(v time.Time) time.Time { return v.Truncate(time.Microsecond) },
		Eq:   ant.Eq[time.Time],
		In:   ant.In[time.Time],
		Gt:   ant.Gt[time.Time],
	})

	probekit.Probe(t, probekit.Spec[time.Time]{
		Name: "time.Time nanoseconds truncate",
		SQL:  "TIMESTAMPTZ",
		Vals: []time.Time{
			time.Date(1969, 12, 31, 23, 59, 59, 999_999_999, time.UTC),
			time.Date(2024, 1, 1, 0, 0, 0, 123_456_789, time.UTC),
			time.Date(2024, 1, 1, 0, 0, 0, 999_999_999, time.UTC),
		},
		Same:  probekit.TimeSame,
		Norm:  func(v time.Time) time.Time { return v.Truncate(time.Microsecond) },
		Eq:    ant.Eq[time.Time],
		In:    ant.In[time.Time],
		Gt:    ant.Gt[time.Time],
		NoPtr: true,
	})

	probekit.Probe(t, probekit.Spec[time.Time]{
		Name: "time.Time timestamp without zone (UTC)",
		SQL:  "TIMESTAMP",
		Vals: []time.Time{
			time.Date(1900, 1, 1, 0, 0, 0, 0, time.UTC),
			time.Date(2024, 2, 29, 12, 0, 0, 123_456_000, time.UTC),
			time.Date(9999, 12, 31, 23, 59, 59, 0, time.UTC),
		},
		Same: probekit.TimeSame,
		Eq:   ant.Eq[time.Time],
		In:   ant.In[time.Time],
		Gt:   ant.Gt[time.Time],
	})

	probekit.Probe(t, probekit.Spec[time.Duration]{
		Name: "time.Duration bigint",
		SQL:  "BIGINT",
		Vals: []time.Duration{math.MinInt64, -1, 0, 1, time.Hour, math.MaxInt64},
		Eq:   ant.Eq[time.Duration],
		In:   ant.In[time.Duration],
		Gt:   ant.Gt[time.Duration],
	})

	probekit.Probe(t, probekit.Spec[ant.UnixTime]{
		Name: "ant.UnixTime",
		SQL:  "BIGINT",
		Vals: []ant.UnixTime{
			{Time: time.Unix(-1, 0).UTC()},
			{Time: time.Unix(1, 0).UTC()},
			{Time: time.Unix(1_700_000_000, 0).UTC()},
		},
		Same: func(a, b ant.UnixTime) bool { return a.Time.Equal(b.Time) },
		Eq:   ant.Eq[ant.UnixTime],
		In:   ant.In[ant.UnixTime],
	})

	// 0 stands for the zero time, as legacy schemas use it for no time, so the
	// Unix epoch itself, which is 0 too, reads back as the zero time.
	probekit.Probe(t, probekit.Spec[ant.UnixTime]{
		Name:  "ant.UnixTime epoch",
		SQL:   "BIGINT",
		Vals:  []ant.UnixTime{{Time: time.Unix(0, 0).UTC()}},
		Same:  func(a, b ant.UnixTime) bool { return a.Time.Equal(b.Time) },
		Norm:  func(ant.UnixTime) ant.UnixTime { return ant.UnixTime{} },
		Eq:    ant.Eq[ant.UnixTime],
		In:    ant.In[ant.UnixTime],
		NoPtr: true,
	})
}

// ---------------------------------------------------------------------------
// sql.Null[T] and the legacy Null types
// ---------------------------------------------------------------------------

func nullSpec[V comparable](name, sqlType string, valid func(V) bool, vals ...V) probekit.Spec[V] {
	return probekit.Spec[V]{
		Name:     name,
		SQL:      sqlType,
		Vals:     vals,
		Nullable: true,
		IsNull:   func(v V) bool { return !valid(v) },
		Eq:       ant.Eq[V],
		In:       ant.In[V],
	}
}

func TestProbeNullTypes(t *testing.T) {
	probekit.KnownHere(t)

	ts := time.Date(2024, 5, 6, 7, 8, 9, 0, time.UTC)

	probekit.Probe(t, nullSpec(
		"sql.Null[int64]",
		"BIGINT",
		func(v sql.Null[int64]) bool { return v.Valid },
		sql.Null[int64]{},
		sql.Null[int64]{V: 0, Valid: true},
		sql.Null[int64]{V: math.MaxInt64, Valid: true},
	))

	probekit.Probe(t, nullSpec(
		"sql.Null[int32]",
		"INTEGER",
		func(v sql.Null[int32]) bool { return v.Valid },
		sql.Null[int32]{},
		sql.Null[int32]{V: math.MinInt32, Valid: true},
		sql.Null[int32]{V: 0, Valid: true},
	))

	probekit.Probe(t, nullSpec(
		"sql.Null[string]",
		"TEXT",
		func(v sql.Null[string]) bool { return v.Valid },
		sql.Null[string]{},
		sql.Null[string]{V: "", Valid: true},
		sql.Null[string]{V: "NULL", Valid: true},
		sql.Null[string]{V: "x", Valid: true},
	))

	probekit.Probe(t, nullSpec(
		"sql.Null[bool]",
		"BOOLEAN",
		func(v sql.Null[bool]) bool { return v.Valid },
		sql.Null[bool]{},
		sql.Null[bool]{V: false, Valid: true},
		sql.Null[bool]{V: true, Valid: true},
	))

	probekit.Probe(t, nullSpec(
		"sql.Null[float64]",
		"DOUBLE PRECISION",
		func(v sql.Null[float64]) bool { return v.Valid },
		sql.Null[float64]{},
		sql.Null[float64]{V: 0, Valid: true},
		sql.Null[float64]{V: math.Inf(1), Valid: true},
	))

	probekit.Probe(t, nullSpec(
		"sql.Null[uint8]",
		"SMALLINT",
		func(v sql.Null[uint8]) bool { return v.Valid },
		sql.Null[uint8]{},
		sql.Null[uint8]{V: 0, Valid: true},
		sql.Null[uint8]{V: 255, Valid: true},
	))

	probekit.Probe(t, nullSpec(
		"sql.Null[uint64]",
		"NUMERIC(20,0)",
		func(v sql.Null[uint64]) bool { return v.Valid },
		sql.Null[uint64]{},
		sql.Null[uint64]{V: 0, Valid: true},
		sql.Null[uint64]{V: math.MaxInt64, Valid: true},
	))
	// database/sql's own Null[T].Value refuses uint64 with the high bit set, though a
	// bare uint64 field is accepted above.
	probekit.Probe(t, nullSpec(
		"sql.Null[uint64] high bit",
		"NUMERIC(20,0)",
		func(v sql.Null[uint64]) bool { return v.Valid },
		sql.Null[uint64]{V: math.MaxUint64, Valid: true},
	))

	tn := nullSpec(
		"sql.Null[time.Time]",
		"TIMESTAMPTZ",
		func(v sql.Null[time.Time]) bool { return v.Valid },
		sql.Null[time.Time]{},
		sql.Null[time.Time]{V: time.Time{}, Valid: true},
		sql.Null[time.Time]{V: ts, Valid: true},
	)
	tn.Same = func(a, b sql.Null[time.Time]) bool { return a.Valid == b.Valid && a.V.Equal(b.V) }
	probekit.Probe(t, tn)

	probekit.Probe(t, nullSpec(
		"sql.NullString",
		"TEXT",
		func(v sql.NullString) bool { return v.Valid },
		sql.NullString{},
		sql.NullString{Valid: true},
		sql.NullString{String: "x", Valid: true},
	))

	probekit.Probe(t, nullSpec(
		"sql.NullInt64",
		"BIGINT",
		func(v sql.NullInt64) bool { return v.Valid },
		sql.NullInt64{},
		sql.NullInt64{Valid: true},
		sql.NullInt64{Int64: -5, Valid: true},
	))

	probekit.Probe(t, nullSpec(
		"sql.NullInt32",
		"INTEGER",
		func(v sql.NullInt32) bool { return v.Valid },
		sql.NullInt32{},
		sql.NullInt32{Valid: true},
		sql.NullInt32{Int32: 5, Valid: true},
	))

	probekit.Probe(t, nullSpec(
		"sql.NullInt16",
		"SMALLINT",
		func(v sql.NullInt16) bool { return v.Valid },
		sql.NullInt16{},
		sql.NullInt16{Valid: true},
		sql.NullInt16{Int16: 5, Valid: true},
	))

	probekit.Probe(t, nullSpec(
		"sql.NullByte",
		"SMALLINT",
		func(v sql.NullByte) bool { return v.Valid },
		sql.NullByte{},
		sql.NullByte{Valid: true},
		sql.NullByte{Byte: 255, Valid: true},
	))

	probekit.Probe(t, nullSpec(
		"sql.NullBool",
		"BOOLEAN",
		func(v sql.NullBool) bool { return v.Valid },
		sql.NullBool{},
		sql.NullBool{Valid: true},
		sql.NullBool{Bool: true, Valid: true},
	))

	probekit.Probe(t, nullSpec(
		"sql.NullFloat64",
		"DOUBLE PRECISION",
		func(v sql.NullFloat64) bool { return v.Valid },
		sql.NullFloat64{},
		sql.NullFloat64{Valid: true},
		sql.NullFloat64{Float64: 1.5, Valid: true},
	))

	nt := nullSpec(
		"sql.NullTime",
		"TIMESTAMPTZ",
		func(v sql.NullTime) bool { return v.Valid },
		sql.NullTime{},
		sql.NullTime{Valid: true},
		sql.NullTime{Time: ts, Valid: true},
	)
	nt.Same = func(a, b sql.NullTime) bool { return a.Valid == b.Valid && a.Time.Equal(b.Time) }
	probekit.Probe(t, nt)
}

// An invalid Null value that still holds data is stored as NULL.
func TestProbeInvalidNullWithData(t *testing.T) {
	probekit.KnownHere(t)

	ddl := `CREATE TABLE ` + probekit.PrTable[sql.Null[int64]](
		"row",
	) + ` (id BIGSERIAL PRIMARY KEY, v BIGINT)`
	fixture.RunWith(t, ddl, func(t *testing.T, db *probekit.PrDB[sql.Null[int64]]) {
		ctx := t.Context()
		f := ant.Fields[probekit.PrRow[sql.Null[int64]]]()
		e := &probekit.PrRow[sql.Null[int64]]{V: sql.Null[int64]{V: 7, Valid: false}}
		require.NoError(t, db.Rows.Insert(ctx, e))

		n, err := db.Rows.Where(ant.IsNull(&f.V)).Count(ctx)
		require.NoError(t, err)
		require.EqualValues(t, 1, n)

		n, err = db.Rows.Where(ant.Eq(&f.V, sql.Null[int64]{V: 99})).Count(ctx)
		require.NoError(t, err)
		require.EqualValues(t, 1, n, "Eq(invalid Null) is Eq(NULL)")

		n, err = db.Rows.Where(ant.Ne(&f.V, sql.Null[int64]{})).Count(ctx)
		require.NoError(t, err)
		require.Zero(t, n)

		n, err = db.Rows.Where(ant.Eq(&f.V, sql.Null[int64]{V: 7, Valid: true})).Count(ctx)
		require.NoError(t, err)
		require.Zero(t, n)
	})
}

// In and NotIn with NULL members: Eq(nil) and Ne(nil) turn into IS [NOT] NULL.
// In/NotIn do not, which is the SQL meaning.
func TestProbeInWithNulls(t *testing.T) {
	probekit.KnownHere(t)

	ddl := `CREATE TABLE ` + probekit.PrTable[int64](
		"nrow",
	) + ` (id BIGSERIAL PRIMARY KEY, p BIGINT)`
	fixture.RunWith(t, ddl, func(t *testing.T, db *probekit.PrDB[int64]) {
		ctx := t.Context()
		f := ant.Fields[probekit.PrNRow[int64]]()
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

		count := func(c ant.Cond) int64 {
			n, err := db.NRows.Where(c).Count(ctx)
			require.NoError(t, err)

			return n
		}

		if n := count(ant.In(&f.P, nil, &one)); n != 2 {
			t.Errorf(
				"In(P, nil, &1) matches %d rows; Eq(P, nil) matches NULL, so want 2 (NULL row and 1)",
				n,
			)
		}

		if n := count(ant.NotIn(&f.P, nil, &one)); n != 1 {
			t.Errorf("NotIn(P, nil, &1) matches %d rows; want 1 (only 2) like Ne(nil)", n)
		}

		if n := count(ant.NotIn(&f.P, &one)); n != 1 {
			t.Errorf(
				"NotIn(P, &1) matches %d rows; plain SQL gives 1 (the NULL row is not 'not in')",
				n,
			)
		}
	})
}
