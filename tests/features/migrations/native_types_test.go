package migrations

import (
	"encoding/json"
	"net"
	"net/netip"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"

	"github.com/skudra-go/ant"
)

// HookV1 has the columns ant made for a time.Duration, a net.IP and a
// json.RawMessage before it mapped them to interval, inet and jsonb: bigint and
// bytea, which these fields of int64 and []byte make too.
type HookV1 struct {
	ID      int64
	Timeout int64
	FromIP  []byte
	Payload []byte
}

func (HookV1) Configure(b *ant.ModelBuilder[HookV1]) { b.Table("hooks") }

// HookV1DB holds HookV1.
type HookV1DB struct {
	ant.Context
	Hooks ant.Table[HookV1]
}

// Hook is HookV1 with the types it stood for.
type Hook struct {
	ID      int64
	Timeout time.Duration
	FromIP  net.IP
	Payload json.RawMessage
}

// HookDB holds Hook.
type HookDB struct {
	ant.Context
	Hooks ant.Table[Hook]
}

// PinnedHook keeps the columns HookV1 made.
type PinnedHook struct {
	ID      int64
	Timeout time.Duration
	FromIP  net.IP
	Payload json.RawMessage
}

func (PinnedHook) Configure(b *ant.ModelBuilder[PinnedHook]) {
	b.Table("hooks")
	b.Column(&PinnedHooks.Timeout).Type("bigint")
	b.Column(&PinnedHooks.FromIP).Type("bytea")
	b.Column(&PinnedHooks.Payload).Type("bytea")
}

// PinnedHookDB holds PinnedHook.
type PinnedHookDB struct {
	ant.Context
	Hooks ant.Table[PinnedHook]
}

var (
	Hooks       = ant.Fields[Hook]()
	PinnedHooks = ant.Fields[PinnedHook]()
)

// The values ant stored for these types before, by how it stored them.
func storedHooks(t *testing.T, p *project) []*HookV1 {
	t.Helper()

	db := open[HookV1DB](p)

	hooks := []*HookV1{
		{
			Timeout: int64(90*time.Second + 1500*time.Microsecond + 999),
			FromIP:  net.ParseIP("10.0.0.1"),
			Payload: []byte(`{"a": [1, "é"]}`),
		},
		{Timeout: -1500, FromIP: net.ParseIP("10.0.0.2").To4()},
		{FromIP: net.ParseIP("2001:db8::1")},
	}
	require.NoError(t, db.Hooks.Insert(t.Context(), hooks...))

	return hooks
}

// A column ant made for the type's earlier default is converted by the next
// diff as ant stored it, where PostgreSQL has no cast: bigint nanoseconds to
// interval, the bytes of a net.IP to inet, and JSON text to jsonb.
func TestUpgradeConvertsWhatAntStored(t *testing.T) {
	each(t, func(t *testing.T, p *project) {
		must[HookV1DB](p, "diff", "start")
		must[HookV1DB](p, "up")

		before := storedHooks(t, p)

		out := must[HookDB](p, "diff", "native types")
		require.Contains(t, out, "reads each value as a time.Duration's nanoseconds")
		require.Contains(t, out, "reads each value as a net.IP's bytes")
		require.Contains(t, out, "reads each value as a json.RawMessage's UTF-8 text")

		file := p.read("versions/0002_native_types.up.sql")
		require.Contains(
			t,
			file,
			`ALTER COLUMN "timeout" TYPE interval USING ("timeout" / 1000) * interval '1 microsecond'`,
		)

		require.Contains(t, file, `ALTER COLUMN "from_ip" TYPE inet USING CASE length("from_ip")`)

		require.Contains(
			t,
			file,
			`ALTER COLUMN "payload" TYPE jsonb USING convert_from("payload", 'UTF8')::jsonb`,
		)

		must[HookDB](p, "up")
		require.Contains(t, must[HookDB](p, "drift"), "No drift")

		db := open[HookDB](p)

		for _, b := range before {
			got, err := db.Hooks.Find(t.Context(), b.ID)
			require.NoError(t, err)

			// An interval keeps microseconds, cut toward zero as pgx cuts them.
			require.Equal(t, time.Duration(b.Timeout/1000*1000), got.Timeout)
			require.True(t, net.IP(b.FromIP).Equal(got.FromIP), got.FromIP)
			require.Len(t, got.FromIP, len(b.FromIP), "a 16-byte IPv4 stays the IPv6 form it was")

			if b.Payload == nil {
				require.Nil(t, got.Payload)
			} else {
				require.JSONEq(t, string(b.Payload), string(got.Payload))
			}
		}

		// A value bound now finds the row converted from what was bound then.
		for _, cond := range []ant.Cond{
			ant.Eq(&Hooks.Timeout, 90*time.Second+1500*time.Microsecond),
			ant.SQLCond("? = ?", ant.Col(&Hooks.FromIP), net.ParseIP("10.0.0.1")),
			ant.SQLCond("? = ?", ant.Col(&Hooks.FromIP), net.ParseIP("10.0.0.2").To4()),
		} {
			n, err := db.Hooks.Where(cond).Count(t.Context())
			require.NoError(t, err)
			require.Equal(t, int64(1), n)
		}
	})
}

// A model that names the earlier type keeps the column, and reads it as before.
func TestUpgradeKeepsAPinnedColumn(t *testing.T) {
	each(t, func(t *testing.T, p *project) {
		must[HookV1DB](p, "diff", "start")
		must[HookV1DB](p, "up")

		before := storedHooks(t, p)

		require.Contains(t, must[PinnedHookDB](p, "diff", "pinned"), "No changes")

		db := open[PinnedHookDB](p)

		for _, b := range before {
			got, err := db.Hooks.Find(t.Context(), b.ID)
			require.NoError(t, err)
			require.Equal(t, time.Duration(b.Timeout), got.Timeout, "nanoseconds, to the last")
			require.Equal(t, net.IP(b.FromIP), got.FromIP)
			require.Equal(t, json.RawMessage(b.Payload), got.Payload)
		}
	})
}

// Natives has a field of each type that maps to a column type of PostgreSQL's
// own.
type Natives struct {
	ID      int64
	Took    time.Duration
	Hop     net.IP
	Host    netip.Addr
	Network netip.Prefix
	Payload json.RawMessage
	Price   decimal.Decimal
	Exact   decimal.Decimal
	Tags    []string
	Codes   []string
	Scores  []int64
	Waits   []time.Duration
	Hosts   []netip.Addr
	Seen    []time.Time
	Meta    map[string]any
}

func (Natives) Configure(b *ant.ModelBuilder[Natives]) {
	b.Column(&NativesFields.Exact).Precision(10, 2)
	b.Column(&NativesFields.Codes).Length(3)
}

// NativesDB holds Natives.
type NativesDB struct {
	ant.Context
	Natives ant.Table[Natives]
}

var NativesFields = ant.Fields[Natives]()

// The types ant maps are what the server stores, so a table made from them is
// no drift, and the next diff finds nothing.
func TestNativeTypesHaveNoDrift(t *testing.T) {
	each(t, func(t *testing.T, p *project) {
		file := must[NativesDB](p, "diff", "start")
		require.NotContains(t, file, "Warning")
		must[NativesDB](p, "up")
		require.Contains(t, must[NativesDB](p, "drift"), "No drift")
		require.Contains(t, must[NativesDB](p, "diff", "again"), "No changes")

		require.Contains(
			t,
			p.read("versions/0001_start.up.sql"),
			`"codes" varchar(3)[]`,
		)
	})
}
