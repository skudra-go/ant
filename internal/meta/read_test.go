package meta

import (
	"database/sql"
	"encoding/json"
	"math"
	"net"
	"reflect"
	"strings"
	"testing"
	"time"
	"unsafe"

	"github.com/stretchr/testify/require"
)

// Each IntervalStyle writes an interval its own way, and each reads back as the
// count of nanoseconds EXTRACT(EPOCH FROM ...) gives. TestIntervalStyles in
// tests/features/value_handling checks the same against the server.
func TestParseInterval(t *testing.T) {
	const (
		second = int64(time.Second)
		minute = 60 * second
		hour   = 60 * minute
		day    = 24 * hour
	)

	cases := []struct {
		text string
		want int64
	}{
		// postgres
		{"00:00:00", 0},
		{"36:00:00", 36 * hour},
		{"1 day 02:00:00", day + 2*hour},
		{"-1 days +02:00:00", -day + 2*hour},
		{"-1 days -02:00:00", -day - 2*hour},
		{"1 mon 2 days -03:04:05.000006", 30*day + 2*day - (3*hour + 4*minute + 5*second + 6000)},
		{"1 year", 365*day + 6*hour},
		{"1 year 2 mons", 365*day + 6*hour + 60*day},
		{"-1 years -2 mons", -(365*day + 6*hour + 60*day)},
		{"-00:00:00.5", -second / 2},
		{"00:00:00.000001", 1000},
		{"2562047:47:16.854775", 2562047*hour + 47*minute + 16*second + 854775000},
		// postgres_verbose
		{"@ 0", 0},
		{"@ 1 day 2 hours", day + 2*hour},
		{"@ 1 day -2 hours ago", -day + 2*hour},
		{
			"@ 1 mon 2 days -3 hours -4 mins -5.000006 secs",
			32*day - (3*hour + 4*minute + 5*second + 6000),
		},
		{"@ 0.5 secs ago", -second / 2},
		{"@ 1 sec", second},
		{"@ 1 hour 1 min", hour + minute},
		// sql_standard
		{"0", 0},
		{"1 2:00:00", day + 2*hour},
		{"-1 2:00:00", -day - 2*hour},
		{"+0-0 -1 +2:00:00", -day + 2*hour},
		{"+0-1 +2 -3:04:05.000006", 32*day - (3*hour + 4*minute + 5*second + 6000)},
		{"1-0", 365*day + 6*hour},
		{"-1-2", -(365*day + 6*hour + 60*day)},
		{"-0:00:00.5", -second / 2},
		{"4:05:06", 4*hour + 5*minute + 6*second},
		// iso_8601
		{"PT0S", 0},
		{"P1DT2H", day + 2*hour},
		{"P-1DT2H", -day + 2*hour},
		{"P1M2DT-3H-4M-5.000006S", 32*day - (3*hour + 4*minute + 5*second + 6000)},
		{"P1Y", 365*day + 6*hour},
		{"PT-0.5S", -second / 2},
		{"PT36H", 36 * hour},
		{"P1W", 7 * day},
		// a TIME as MySQL writes it
		{"838:59:59", 838*hour + 59*minute + 59*second},
	}

	for _, c := range cases {
		t.Run(c.text, func(t *testing.T) {
			i, err := parseInterval(c.text)
			require.NoError(t, err)

			got, err := i.duration()
			require.NoError(t, err)
			require.Equal(t, time.Duration(c.want), got)
		})
	}

	// 13 months is a year and a month, as PostgreSQL counts them, not 13 times
	// 30 days.
	i, err := parseInterval("13 mons")
	require.NoError(t, err)

	got, err := i.duration()
	require.NoError(t, err)
	require.Equal(t, time.Duration(395*day+6*hour), got)
}

func TestParseIntervalRefuses(t *testing.T) {
	for _, text := range []string{
		"",
		"@",
		"ago",
		"abc",
		"1 fortnight",
		"1:2:3",
		"01:60:00",
		"01:00:60",
		"1.5 days",
		"P",
		"PT",
		"P1H",
		"PT1D",
		"P1.5D",
		"1 day x",
		"--1 day",
		"1--2",
		"00:00:00.0000000001",
	} {
		_, err := parseInterval(text)
		require.ErrorIs(t, err, errSyntax, text)
		require.ErrorContains(t, err, `"`+text+`"`)
	}

	for _, text := range []string{
		"infinity",
		"-infinity",
		"2562048:00:00",
		"106752 days",
		"300 years",
		"99999999999999999999:00:00",
		"-2562048:00:00",
	} {
		i, err := parseInterval(text)
		if err == nil {
			_, err = i.duration()
		}

		require.ErrorIs(t, err, errDuration, text)
	}

	// The extremes that fit.
	for _, d := range []time.Duration{math.MaxInt64 / 1000 * 1000, math.MinInt64 / 1000 * 1000} {
		text := strings.TrimSuffix(d.Truncate(time.Hour).String(), "0m0s")
		i, err := parseInterval(strings.TrimSuffix(text, "h") + ":00:00")
		require.NoError(t, err, text)

		got, err := i.duration()
		require.NoError(t, err)
		require.Equal(t, d.Truncate(time.Hour), got)
	}
}

func scan[T any](t *testing.T, src any) (T, error) {
	t.Helper()

	var v T

	read := ReaderFor(reflect.TypeFor[T]())
	require.NotNil(t, read)

	err := read(unsafe.Pointer(&v)).(sql.Scanner).Scan(src)

	return v, err
}

// A time.Duration reads from the integer ant wrote before, and from an interval.
func TestReadDuration(t *testing.T) {
	got, err := scan[time.Duration](t, int64(90))
	require.NoError(t, err)
	require.Equal(t, 90*time.Nanosecond, got)

	got, err = scan[time.Duration](t, "90")
	require.NoError(t, err)
	require.Equal(t, 90*time.Nanosecond, got, "a whole number is nanoseconds, as before")

	got, err = scan[time.Duration](t, "00:01:30")
	require.NoError(t, err)
	require.Equal(t, 90*time.Second, got)

	got, err = scan[time.Duration](t, []byte("P1DT1S"))
	require.NoError(t, err)
	require.Equal(t, 24*time.Hour+time.Second, got)

	_, err = scan[time.Duration](t, nil)
	require.ErrorContains(t, err, "NULL", "NULL is database/sql's error, as before")

	_, err = scan[time.Duration](t, 1.5)
	require.Error(t, err)

	_, err = scan[time.Duration](t, "infinity")
	require.ErrorContains(t, err, "does not fit a time.Duration")

	p, err := scan[*time.Duration](t, "01:00:00")
	require.NoError(t, err)
	require.Equal(t, time.Hour, *p)

	p, err = scan[*time.Duration](t, nil)
	require.NoError(t, err)
	require.Nil(t, p)

	n, err := scan[sql.Null[time.Duration]](t, "-00:00:01")
	require.NoError(t, err)
	require.Equal(t, sql.Null[time.Duration]{V: -time.Second, Valid: true}, n)

	n, err = scan[sql.Null[time.Duration]](t, nil)
	require.NoError(t, err)
	require.Equal(t, sql.Null[time.Duration]{}, n)
}

// A net.IP reads from bytes as a bytea held them, and from the text of an inet.
func TestReadIP(t *testing.T) {
	got, err := scan[net.IP](t, []byte{10, 0, 0, 1})
	require.NoError(t, err)
	require.Equal(t, net.IP{10, 0, 0, 1}, got)

	got, err = scan[net.IP](t, "10.0.0.1")
	require.NoError(t, err)
	require.Equal(t, net.IP{10, 0, 0, 1}, got, "IPv4 is four bytes, as To4 gives")

	got, err = scan[net.IP](t, "::ffff:10.0.0.1")
	require.NoError(t, err)
	require.Len(t, got, 16)
	require.True(t, got.Equal(net.IPv4(10, 0, 0, 1)))

	got, err = scan[net.IP](t, "2001:db8::1")
	require.NoError(t, err)
	require.Equal(t, net.ParseIP("2001:db8::1"), got)

	got, err = scan[net.IP](t, "10.0.0.1/32")
	require.NoError(t, err, "a cidr writes a host's mask")
	require.Equal(t, net.IP{10, 0, 0, 1}, got)

	for _, text := range []string{"10.0.0.1/24", "10.0.0.0/8", "nonsense"} {
		_, err = scan[net.IP](t, text)
		require.ErrorContains(t, err, "netip.Prefix", text)
	}

	got, err = scan[net.IP](t, nil)
	require.NoError(t, err)
	require.Nil(t, got, "NULL is nil, as for a []byte")

	p, err := scan[*net.IP](t, "::1")
	require.NoError(t, err)
	require.Equal(t, net.IPv6loopback, *p)

	p, err = scan[*net.IP](t, nil)
	require.NoError(t, err)
	require.Nil(t, p)

	n, err := scan[sql.Null[net.IP]](t, "10.0.0.1")
	require.NoError(t, err)
	require.Equal(t, sql.Null[net.IP]{V: net.IP{10, 0, 0, 1}, Valid: true}, n)

	n, err = scan[sql.Null[net.IP]](t, nil)
	require.NoError(t, err)
	require.False(t, n.Valid)
}

type blob []byte

type scannedBlob []byte

func (b *scannedBlob) Scan(any) error { return nil }

// database/sql refuses NULL into a named byte slice, though it gives a []byte
// nil, so ant reads one as nil, and text as its bytes.
func TestReadNamedBytes(t *testing.T) {
	raw, err := scan[json.RawMessage](t, nil)
	require.NoError(t, err)
	require.Nil(t, raw)

	source := []byte(`{"a":1}`)

	raw, err = scan[json.RawMessage](t, source)
	require.NoError(t, err)
	require.Equal(t, json.RawMessage(`{"a":1}`), raw)

	source[0] = 'x'

	require.Equal(t, json.RawMessage(`{"a":1}`), raw, "a copy, as the driver reuses its buffer")

	raw, err = scan[json.RawMessage](t, `[1]`)
	require.NoError(t, err)
	require.Equal(t, json.RawMessage(`[1]`), raw)

	_, err = scan[json.RawMessage](t, int64(1))
	require.ErrorContains(t, err, "json", "anything else is database/sql's error")

	b, err := scan[blob](t, nil)
	require.NoError(t, err)
	require.Nil(t, b)

	pb, err := scan[*blob](t, []byte{1})
	require.NoError(t, err)
	require.Equal(t, blob{1}, *pb)

	pb, err = scan[*blob](t, nil)
	require.NoError(t, err)
	require.Nil(t, pb)

	for _, typ := range []reflect.Type{
		reflect.TypeFor[[]byte](),
		reflect.TypeFor[sql.RawBytes](),
		reflect.TypeFor[scannedBlob](),
		reflect.TypeFor[string](),
		reflect.TypeFor[time.Time](),
		reflect.TypeFor[int64](),
	} {
		require.Nil(t, ReaderFor(typ), typ)
	}
}

// A float64 reads an interval as its nanoseconds, as AVG of one gives, and a
// map reads JSON with json.Number.
func TestReadFloatsAndMaps(t *testing.T) {
	f, err := scan[float64](t, "00:00:01.5")
	require.NoError(t, err)
	require.Equal(t, 1.5e9, f)

	f, err = scan[float64](t, "2.5")
	require.NoError(t, err)
	require.Equal(t, 2.5, f)

	_, err = scan[float64](t, "abc")
	require.ErrorContains(t, err, "converting", "database/sql's error, as before")

	n, err := scan[sql.Null[float64]](t, nil)
	require.NoError(t, err)
	require.False(t, n.Valid)

	m, err := scan[map[string]any](t, []byte(`{"id": 9007199254740993}`))
	require.NoError(t, err)
	require.Equal(t, map[string]any{"id": json.Number("9007199254740993")}, m)

	m, err = scan[map[string]any](t, nil)
	require.NoError(t, err)
	require.Nil(t, m)
}

// What only a driver converts is scanned into a pointer on the side of an
// outer join, so database/sql must not be thought to handle it.
func TestSQLConverts(t *testing.T) {
	for _, typ := range []reflect.Type{
		reflect.TypeFor[int](),
		reflect.TypeFor[*string](),
		reflect.TypeFor[[]byte](),
		reflect.TypeFor[json.RawMessage](),
		reflect.TypeFor[time.Time](),
		reflect.TypeFor[**time.Time](),
		reflect.TypeFor[DeletedAt](),
		reflect.TypeFor[sql.NullString](),
		reflect.TypeFor[any](),
		reflect.TypeFor[UnixTime](),
	} {
		require.True(t, sqlConverts(typ), typ)
	}

	for _, typ := range []reflect.Type{
		reflect.TypeFor[[]string](),
		reflect.TypeFor[[16]byte](),
		reflect.TypeFor[map[int]any](),
		reflect.TypeFor[struct{ A int }](),
		reflect.TypeFor[*[]int64](),
	} {
		require.False(t, sqlConverts(typ), typ)
	}
}
