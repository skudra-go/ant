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
		"net.IP":          {reflect.TypeFor[net.IP](), "inet"},
		"netip.Addr":      {reflect.TypeFor[netip.Addr](), "inet"},
		"netip.Prefix":    {reflect.TypeFor[netip.Prefix](), "cidr"},
		"time.Duration":   {reflect.TypeFor[time.Duration](), "interval"},
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
