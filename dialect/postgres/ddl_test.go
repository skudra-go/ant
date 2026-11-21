package postgres

import (
	"database/sql/driver"
	"encoding/json"
	"net"
	"net/netip"
	"reflect"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"

	"github.com/skudra-go/ant/schema"
)

type status string

type tags []string

// stringsAsJSON is a slice that stores itself, as JSON text, so it is no array.
type stringsAsJSON []string

func (stringsAsJSON) Value() (driver.Value, error) { return "[]", nil }

type scannedInts []int64

func (*scannedInts) Scan(any) error { return nil }

// A type with a column type of PostgreSQL's own takes it.
func TestColumnTypeForNativeTypes(t *testing.T) {
	cases := map[string]struct {
		t    reflect.Type
		f    schema.Facets
		want string
	}{
		"time.Time":       {t: reflect.TypeFor[time.Time](), want: "timestamptz"},
		"time.Duration":   {t: reflect.TypeFor[time.Duration](), want: "interval"},
		"json.RawMessage": {t: reflect.TypeFor[json.RawMessage](), want: "jsonb"},
		"net.IP":          {t: reflect.TypeFor[net.IP](), want: "inet"},
		"netip.Addr":      {t: reflect.TypeFor[netip.Addr](), want: "inet"},
		"netip.Prefix":    {t: reflect.TypeFor[netip.Prefix](), want: "cidr"},
		"decimal.Decimal": {t: reflect.TypeFor[decimal.Decimal](), want: "numeric"},
		"decimal.Decimal with a precision": {
			t:    reflect.TypeFor[decimal.Decimal](),
			f:    schema.Facets{Precision: 10, Scale: 2},
			want: "numeric(10,2)",
		},
		"uuid.UUID": {t: reflect.TypeFor[uuid.UUID](), want: "uuid"},
		"[]byte":    {t: reflect.TypeFor[[]byte](), want: "bytea"},
		"[]string":  {t: reflect.TypeFor[[]string](), want: "text[]"},
		"[]string with a length": {
			t:    reflect.TypeFor[[]string](),
			f:    schema.Facets{Length: 20},
			want: "varchar(20)[]",
		},
		"[]float64 with a precision": {
			t:    reflect.TypeFor[[]float64](),
			f:    schema.Facets{Precision: 8, Scale: 3},
			want: "numeric(8,3)[]",
		},
		"a named slice":       {t: reflect.TypeFor[tags](), want: "text[]"},
		"[]status":            {t: reflect.TypeFor[[]status](), want: "text[]"},
		"[]*string":           {t: reflect.TypeFor[[]*string](), want: "text[]"},
		"[]int64":             {t: reflect.TypeFor[[]int64](), want: "bigint[]"},
		"[]int":               {t: reflect.TypeFor[[]int](), want: "bigint[]"},
		"[]int32":             {t: reflect.TypeFor[[]int32](), want: "integer[]"},
		"[]int16":             {t: reflect.TypeFor[[]int16](), want: "smallint[]"},
		"[]bool":              {t: reflect.TypeFor[[]bool](), want: "boolean[]"},
		"[]float32":           {t: reflect.TypeFor[[]float32](), want: "real[]"},
		"[]time.Time":         {t: reflect.TypeFor[[]time.Time](), want: "timestamptz[]"},
		"[]time.Duration":     {t: reflect.TypeFor[[]time.Duration](), want: "interval[]"},
		"[]uuid.UUID":         {t: reflect.TypeFor[[]uuid.UUID](), want: "uuid[]"},
		"[]netip.Addr":        {t: reflect.TypeFor[[]netip.Addr](), want: "inet[]"},
		"[]net.IP":            {t: reflect.TypeFor[[]net.IP](), want: "inet[]"},
		"[]netip.Prefix":      {t: reflect.TypeFor[[]netip.Prefix](), want: "cidr[]"},
		"[]json.RawMessage":   {t: reflect.TypeFor[[]json.RawMessage](), want: "jsonb[]"},
		"[][]byte":            {t: reflect.TypeFor[[][]byte](), want: "bytea[]"},
		"[]decimal.Decimal":   {t: reflect.TypeFor[[]decimal.Decimal](), want: "numeric[]"},
		"[]*decimal.Decimal":  {t: reflect.TypeFor[[]*decimal.Decimal](), want: "numeric[]"},
		"a named string":      {t: reflect.TypeFor[status](), want: "text"},
		"map[string]any":      {t: reflect.TypeFor[map[string]any](), want: "jsonb"},
		"int64, as it always": {t: reflect.TypeFor[int64](), want: "bigint"},
		"uint64":              {t: reflect.TypeFor[uint64](), want: "numeric(20,0)"},
		"uint32":              {t: reflect.TypeFor[uint32](), want: "bigint"},
	}

	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			got, ok := impl{}.ColumnType(c.t, c.f)
			require.True(t, ok)
			require.Equal(t, c.want, got)
		})
	}
}

// A slice ant can't know the storage of has no column type, so EnsureCreated and
// migrations ask for one, as they do for any type ant does not know.
func TestColumnTypeForSlicesItCannotKnow(t *testing.T) {
	for name, typ := range map[string]reflect.Type{
		"a slice that is a Valuer":     reflect.TypeFor[stringsAsJSON](),
		"a slice that is a Scanner":    reflect.TypeFor[scannedInts](),
		"a slice of slices":            reflect.TypeFor[[][]string](),
		"a slice of an unmapped type":  reflect.TypeFor[[]struct{ A int }](),
		"a slice of interfaces":        reflect.TypeFor[[]any](),
		"a slice of maps":              reflect.TypeFor[[]map[string]any](),
		"a map with int keys":          reflect.TypeFor[map[int]string](),
		"decimal.NullDecimal":          reflect.TypeFor[decimal.NullDecimal](),
		"a type named Decimal, not it": reflect.TypeFor[struct{ Decimal int }](),
	} {
		t.Run(name, func(t *testing.T) {
			got, ok := impl{}.ColumnType(typ, schema.Facets{})
			require.False(t, ok, got)
		})
	}
}

// A column that ant made for a type's earlier default is converted as ant stored
// it, where PostgreSQL has no cast. TestUpgradeConvertsWhatAntStored runs these.
func TestAlterColumnConvertsEarlierDefaults(t *testing.T) {
	alter := func(from, to string) string {
		out, err := impl{}.Statements(schema.AlterColumn{
			Table: schema.Ref{Name: "hooks"},
			From:  &schema.Column{Name: "v", Type: from},
			To:    &schema.Column{Name: "v", Type: to},
		})
		require.NoError(t, err)
		require.Len(t, out, 1)

		return out[0]
	}

	require.Equal(
		t,
		`ALTER TABLE "hooks" ALTER COLUMN "v" TYPE interval USING ("v" / 1000) * interval '1 microsecond'`,
		alter("bigint", "interval"),
	)

	require.Equal(
		t,
		`ALTER TABLE "hooks" ALTER COLUMN "v" TYPE inet USING CASE length("v")`+
			` WHEN 4 THEN array_to_string(ARRAY[get_byte("v", 0), get_byte("v", 1), get_byte("v", 2), get_byte("v", 3)], '.')`+
			` WHEN 16 THEN regexp_replace(encode("v", 'hex'), '(.{4})(?=.)', '\1:', 'g') END::inet`,
		alter("bytea", "inet"),
	)

	require.Equal(
		t,
		`ALTER TABLE "hooks" ALTER COLUMN "v" TYPE jsonb USING convert_from("v", 'UTF8')::jsonb`,
		alter("bytea", "jsonb"),
	)

	require.Equal(
		t,
		`ALTER TABLE "hooks" ALTER COLUMN "v" TYPE json USING convert_from("v", 'UTF8')::json`,
		alter("BYTEA", "json"),
	)

	// Anything else keeps its cast.
	require.Equal(
		t,
		`ALTER TABLE "hooks" ALTER COLUMN "v" TYPE interval USING "v"::interval`,
		alter("integer", "interval"),
	)

	require.Equal(t, `ALTER TABLE "hooks" ALTER COLUMN "v" TYPE text`, alter("bytea", "text"))
}
