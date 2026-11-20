package postgres

import (
	"encoding/json"
	"net"
	"net/netip"
	"reflect"
	"testing"
	"time"

	"github.com/skudra-go/ant/schema"
	"github.com/stretchr/testify/require"
)

func TestColumnTypeForPostgresNativeTypes(t *testing.T) {
	cases := map[string]struct {
		t    reflect.Type
		want string
	}{
		"json.RawMessage": {reflect.TypeFor[json.RawMessage](), "jsonb"},
		"[]byte":          {reflect.TypeFor[[]byte](), "bytea"},
	}

	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			got, ok := impl{}.ColumnType(c.t, schema.Facets{})
			require.True(t, ok)
			require.Equal(t, c.want, got)
		})
	}
}

// Types whose values come back from PostgreSQL as text keep the mapping they
// had, so a model that reads them still works. A type with no mapping is an
// error until the model names its column type.
func TestColumnTypeKeepsTypesThatDoNotReadBack(t *testing.T) {
	cases := map[string]struct {
		t    reflect.Type
		want string
	}{
		"time.Duration": {reflect.TypeFor[time.Duration](), "bigint"},
		"net.IP":        {reflect.TypeFor[net.IP](), "bytea"},
	}

	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			got, ok := impl{}.ColumnType(c.t, schema.Facets{})
			require.True(t, ok)
			require.Equal(t, c.want, got)
		})
	}

	for name, typ := range map[string]reflect.Type{
		"netip.Addr":   reflect.TypeFor[netip.Addr](),
		"netip.Prefix": reflect.TypeFor[netip.Prefix](),
	} {
		t.Run(name, func(t *testing.T) {
			_, ok := impl{}.ColumnType(typ, schema.Facets{})
			require.False(t, ok)
		})
	}
}
