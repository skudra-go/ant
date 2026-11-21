package value_handling

import (
	"encoding/json"
	"fmt"
	"net"
	"net/netip"
	"strings"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"

	"github.com/skudra-go/ant"
	"github.com/skudra-go/ant/internal/testdb"
	"github.com/skudra-go/ant/tests/features/fixture"
)

// Reading has a field of each Go type that has a column type of PostgreSQL's
// own, which EnsureCreated and migrations give it.
type Reading struct {
	ID      int64
	Payload json.RawMessage // jsonb
	Took    time.Duration   // interval
	Timeout *time.Duration  // interval, NULL allowed
	Hop     net.IP          // inet
	Host    netip.Addr      // inet
	Gateway *netip.Addr     // inet, NULL allowed
	Network netip.Prefix    // cidr
	Price   decimal.Decimal // numeric
	Tags    []string        // text[]
	Scores  []int64         // bigint[]
	Waits   []time.Duration // interval[]
	Meta    map[string]any  // jsonb
}

// Probe sends readings, and IncludeJoined reads its last one on the same row.
type Probe struct {
	ID        int64
	ReadingID *int64
	Reading   *Reading
}

// ReadingDB holds Reading and Probe.
type ReadingDB struct {
	ant.Context
	Readings ant.Table[Reading]
	Probes   ant.Table[Probe]
}

var (
	Readings = ant.Fields[Reading]()
	Probes   = ant.Fields[Probe]()
)

func full() *Reading {
	timeout := 5 * time.Second
	gateway := netip.MustParseAddr("10.0.0.254")

	return &Reading{
		Payload: json.RawMessage(`{"a": [1, true]}`),
		Took:    90*time.Second + 1500*time.Microsecond,
		Timeout: &timeout,
		Hop:     net.IP{10, 0, 0, 1},
		Host:    netip.MustParseAddr("2001:db8::1"),
		Gateway: &gateway,
		Network: netip.MustParsePrefix("10.0.0.0/8"),
		Price:   decimal.RequireFromString("12.345"),
		Tags:    []string{"a", "b,c", `"q"`, ""},
		Scores:  []int64{1, -2},
		Waits:   []time.Duration{time.Second, -time.Hour},
		Meta:    map[string]any{"id": json.Number("9007199254740993")},
	}
}

func requireSame(t *testing.T, want, got *Reading) {
	t.Helper()

	require.JSONEq(t, string(want.Payload), string(got.Payload))
	require.True(t, want.Price.Equal(got.Price), got.Price)

	a, b := *want, *got
	a.Payload, b.Payload = nil, nil
	a.Price, b.Price = decimal.Decimal{}, decimal.Decimal{}

	require.Equal(t, a, b)
}

func TestNativeTypes(t *testing.T) {
	fixture.Each(t, func(t *testing.T, target testdb.Target) {
		db := fixture.Open[ReadingDB](t, target)
		ctx := t.Context()
		require.NoError(t, db.EnsureCreated(ctx))

		t.Run("columns take PostgreSQL's types", func(t *testing.T) {
			want := map[string]string{
				"payload": "jsonb",
				"took":    "interval",
				"timeout": "interval",
				"hop":     "inet",
				"host":    "inet",
				"gateway": "inet",
				"network": "cidr",
				"price":   "numeric",
				"tags":    "text[]",
				"scores":  "bigint[]",
				"waits":   "interval[]",
				"meta":    "jsonb",
			}

			for column, typ := range want {
				var got string

				err := target.DB.QueryRowContext(
					ctx,
					`SELECT format_type(atttypid, atttypmod) FROM pg_attribute
					 WHERE attrelid = 'readings'::regclass AND attname = $1`,
					column,
				).Scan(&got)
				require.NoError(t, err, column)
				require.Equal(t, typ, got, column)
			}
		})

		reading := full()
		require.NoError(t, db.Readings.Insert(ctx, reading))

		t.Run("values read back", func(t *testing.T) {
			got, err := db.Readings.Find(ctx, reading.ID)
			require.NoError(t, err)

			// An interval keeps microseconds, as does every value here.
			requireSame(t, reading, got)
		})

		t.Run("so does NULL", func(t *testing.T) {
			empty := &Reading{}
			require.NoError(t, db.Readings.Insert(ctx, empty))

			got, err := db.Readings.Find(ctx, empty.ID)
			require.NoError(t, err)
			require.Nil(t, got.Payload)
			require.Nil(t, got.Timeout)
			require.Nil(t, got.Hop)
			require.Nil(t, got.Gateway)
			require.False(t, got.Host.IsValid(), "the zero address is NULL, both ways")
			require.False(t, got.Network.IsValid())
			require.Nil(t, got.Tags)
			require.Nil(t, got.Waits)
			require.Nil(t, got.Meta)
			require.NoError(t, db.Readings.DeleteByID(ctx, empty.ID))
		})

		t.Run("conditions compare in the column's type", func(t *testing.T) {
			for _, cond := range []ant.Cond{
				ant.Eq(&Readings.Took, reading.Took),
				ant.Gt(&Readings.Took, time.Minute),
				ant.In(&Readings.Took, time.Hour, reading.Took),
				ant.Eq(&Readings.Host, reading.Host),
				ant.In(&Readings.Host, netip.MustParseAddr("::1"), reading.Host),
				ant.Eq(&Readings.Network, reading.Network),
				ant.Eq(&Readings.Price, decimal.RequireFromString("12.3450")),
				// A slice is not comparable in Go, so Eq does not take one.
				ant.SQLCond("? = ?", ant.Col(&Readings.Hop), net.IP{10, 0, 0, 1}),
				ant.SQLCond("? << ?", ant.Col(&Readings.Hop), reading.Network),
				ant.SQLCond("? = ?", ant.Col(&Readings.Tags), reading.Tags),
				ant.SQLCond("? @> ?", ant.Col(&Readings.Tags), []string{"b,c"}),
				ant.SQLCond("? = ?", ant.Col(&Readings.Payload), json.RawMessage(`{"a":[1,true]}`)),
			} {
				n, err := db.Readings.Where(cond).Count(ctx)
				require.NoError(t, err)
				require.Equal(t, int64(1), n, fmt.Sprint(cond))
			}
		})

		t.Run("one column and aggregates read back", func(t *testing.T) {
			took, err := ant.Pluck(ctx, db.Readings.Where(), &Readings.Took)
			require.NoError(t, err)
			require.Equal(t, []time.Duration{reading.Took}, took)

			hops, err := ant.Pluck(ctx, db.Readings.Where(), &Readings.Hop)
			require.NoError(t, err)
			require.Equal(t, []net.IP{reading.Hop}, hops)

			most, err := ant.MaxOf(ctx, db.Readings.Where(), &Readings.Took)
			require.NoError(t, err)
			require.Equal(t, reading.Took, most)

			sum, err := ant.SumOf(ctx, db.Readings.Where(), &Readings.Took)
			require.NoError(t, err)
			require.Equal(t, reading.Took, sum)

			average, err := ant.AvgOf(ctx, db.Readings.Where(), &Readings.Took)
			require.NoError(t, err)
			require.Equal(t, float64(reading.Took), average)

			type averages struct{ Took float64 }

			var Averages = ant.Fields[averages]()

			rows, err := ant.Project[averages](
				db.Readings.Where(),
				ant.IntoExpr(&Averages.Took, ant.Avg(&Readings.Took)),
			).All(ctx)
			require.NoError(t, err)

			require.Equal(
				t,
				[]averages{{float64(reading.Took)}},
				rows,
				"nanoseconds, from an interval",
			)

			none, err := ant.AvgOf(
				ctx,
				db.Readings.Where(ant.Lt(&Readings.Took, 0)),
				&Readings.Took,
			)
			require.NoError(t, err)
			require.Zero(t, none)
		})

		t.Run("as does the other side of an outer join", func(t *testing.T) {
			with := &Probe{ReadingID: &reading.ID}
			without := &Probe{}
			require.NoError(t, db.Probes.Insert(ctx, with, without))

			probes, err := db.Probes.IncludeJoined(&Probes.Reading).OrderBy(&Probes.ID).All(ctx)
			require.NoError(t, err)
			require.Len(t, probes, 2)
			requireSame(t, reading, probes[0].Reading)
			require.Nil(t, probes[1].Reading)

			type pair struct {
				Probe   int64
				Reading *Reading
			}

			pairs, err := db.Probes.LeftJoin(&Probes.Reading).
				OrderBy(&Probes.ID).
				Select(func(p *Probe, r *Reading) pair { return pair{p.ID, r} }).
				All(ctx)
			require.NoError(t, err)
			require.Len(t, pairs, 2)
			requireSame(t, reading, pairs[0].Reading)
			require.Nil(t, pairs[1].Reading)
		})

		t.Run("bulk writes and upserts", func(t *testing.T) {
			many := []*Reading{full(), full()}
			many[1].Took = time.Millisecond
			_, err := db.Readings.InsertMany(ctx, many)
			require.NoError(t, err)

			many[0].Took = 2 * time.Hour
			many[1].Hop = net.IP{192, 168, 0, 1}
			_, err = db.Readings.UpdateMany(ctx, many, ant.F(&Readings.Took), ant.F(&Readings.Hop))
			require.NoError(t, err)

			for _, want := range many {
				got, err := db.Readings.Find(ctx, want.ID)
				require.NoError(t, err)
				requireSame(t, want, got)
			}

			many[0].Waits = nil
			_, err = db.Readings.Upsert(ctx, many[0], ant.OnConflict(&Readings.ID))
			require.NoError(t, err)

			got, err := db.Readings.Find(ctx, many[0].ID)
			require.NoError(t, err)
			requireSame(t, many[0], got)
		})

		t.Run("a list longer than the parameters goes as one array", func(t *testing.T) {
			took := make([]time.Duration, 70000)
			hosts := make([]netip.Addr, len(took))

			for i := range took {
				took[i] = time.Duration(i) * time.Hour
				hosts[i] = netip.AddrFrom4([4]byte{192, 168, byte(i >> 8), byte(i)})
			}

			took[len(took)-1], hosts[len(hosts)-1] = reading.Took, reading.Host

			for _, cond := range []ant.Cond{ant.In(&Readings.Took, took...), ant.In(&Readings.Host, hosts...)} {
				n, err := db.Readings.Where(ant.Eq(&Readings.ID, reading.ID), cond).Count(ctx)
				require.NoError(t, err)
				require.Equal(t, int64(1), n)
			}
		})
	})
}

// Pinned keeps the column types ant gave these fields before it mapped them to
// PostgreSQL's own: a model names the type to keep a column that predates the
// change, and the values read back from it as they did.
type Pinned struct {
	ID      int64
	Took    time.Duration
	Waits   []time.Duration
	Hop     net.IP
	Payload json.RawMessage
}

func (Pinned) Configure(b *ant.ModelBuilder[Pinned]) {
	b.Column(&Pinneds.Took).Type("bigint")
	b.Column(&Pinneds.Waits).Type("bigint[]")
	b.Column(&Pinneds.Hop).Type("bytea")
	b.Column(&Pinneds.Payload).Type("bytea")
}

// PinnedDB holds Pinned.
type PinnedDB struct {
	ant.Context
	Pinneds ant.Table[Pinned]
}

var Pinneds = ant.Fields[Pinned]()

func TestNativeTypesPinned(t *testing.T) {
	fixture.Each(t, func(t *testing.T, target testdb.Target) {
		db := fixture.Open[PinnedDB](t, target)
		ctx := t.Context()
		require.NoError(t, db.EnsureCreated(ctx))

		p := &Pinned{
			Took:    90 * time.Second,
			Waits:   []time.Duration{time.Nanosecond, time.Hour},
			Hop:     net.IP{10, 0, 0, 1},
			Payload: json.RawMessage(`{"a":1}`),
		}
		require.NoError(t, db.Pinneds.Insert(ctx, p))

		var (
			took  int64
			waits string
			hop   []byte
		)

		err := target.DB.QueryRowContext(ctx, "SELECT took, waits::text, hop FROM pinneds").
			Scan(&took, &waits, &hop)
		require.NoError(t, err)
		require.Equal(t, int64(90*time.Second), took, "nanoseconds, as before")
		require.Equal(t, "{1,3600000000000}", waits)
		require.Equal(t, []byte{10, 0, 0, 1}, hop)

		got, err := db.Pinneds.Find(ctx, p.ID)
		require.NoError(t, err)
		require.Equal(t, p, got)

		long := make([]time.Duration, 70000)
		long[0] = p.Took

		for _, cond := range []ant.Cond{ant.Eq(&Pinneds.Took, p.Took), ant.In(&Pinneds.Took, long...)} {
			n, err := db.Pinneds.Where(cond).Count(ctx)
			require.NoError(t, err)
			require.Equal(t, int64(1), n)
		}

		average, err := ant.AvgOf(ctx, db.Pinneds.Where(), &Pinneds.Took)
		require.NoError(t, err)
		require.Equal(t, float64(p.Took), average)
	})
}

// A tracked change to any of these is saved, and a read is no change.
func TestNativeTypesTracked(t *testing.T) {
	fixture.Each(t, func(t *testing.T, target testdb.Target) {
		db := fixture.Open[ReadingDB](t, target, ant.WithTracking(true))
		ctx := t.Context()
		require.NoError(t, db.EnsureCreated(ctx))
		require.NoError(t, db.Readings.Insert(ctx, full()))

		session := ant.Session(db)

		got, err := session.Readings.First(ctx)
		require.NoError(t, err)
		require.NoError(t, session.SaveChanges(ctx), "nothing changed")

		got.Took = time.Minute
		got.Tags = append(got.Tags, "d")

		require.NoError(t, session.SaveChanges(ctx))

		stored, err := db.Readings.NoTracking().Find(ctx, got.ID)
		require.NoError(t, err)
		require.Equal(t, time.Minute, stored.Took)
		require.Equal(t, got.Tags, stored.Tags)
	})
}

// PostgreSQL writes an interval as the session's IntervalStyle says, and every
// one reads back as the duration EXTRACT(EPOCH FROM ...) counts. pgx writes one
// itself where it reads the value in binary, as it does a query with arguments.
func TestIntervalStyles(t *testing.T) {
	fixture.Each(t, func(t *testing.T, target testdb.Target) {
		var values []string

		for _, years := range []int{0, 1, -2} {
			for _, months := range []int{0, 5, -13} {
				for _, days := range []int{0, 1, -40} {
					for _, micros := range []int64{0, 1, -500000, 3723000006, -90061000001} {
						values = append(
							values,
							fmt.Sprintf(
								"(make_interval(years => %d, months => %d, days => %d) + %d * interval '1 microsecond')",
								years,
								months,
								days,
								micros,
							),
						)
					}
				}
			}
		}

		query := `SELECT v AS took, (EXTRACT(EPOCH FROM v) * 1000000)::bigint AS micros
			FROM (VALUES ` + strings.Join(values, ", ") + `) AS t(v) WHERE $1`

		type row struct {
			Took   time.Duration
			Micros int64
		}

		for _, style := range []string{"postgres", "postgres_verbose", "sql_standard", "iso_8601"} {
			t.Run(style, func(t *testing.T) {
				db := fixture.Open[ReadingDB](
					t,
					testdb.Target{
						Name:    target.Name,
						Dialect: target.Dialect,
						DSN:     target.DSN + "&intervalstyle=" + style,
					},
				)

				for _, args := range [][]any{nil, {true}} {
					q := query

					if args == nil {
						q = strings.Replace(query, "$1", "true", 1)
					}

					rows, err := ant.Raw[row](db, q, args...).All(t.Context())
					require.NoError(t, err)
					require.Len(t, rows, len(values))

					for _, r := range rows {
						require.Equal(t, time.Duration(r.Micros*1000), r.Took)
					}
				}

				_, err := ant.Raw[time.Duration](db, "SELECT 'infinity'::interval").All(t.Context())
				require.ErrorContains(t, err, "does not fit a time.Duration")
			})
		}
	})
}
