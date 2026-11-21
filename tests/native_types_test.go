package tests

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/skudra-go/ant"
	"github.com/skudra-go/ant/internal/testdb"
)

// Span keeps a time.Duration in an integer column, and a json.RawMessage and a
// net.IP in bytes, as every database can, and as ant made them on PostgreSQL
// before it mapped them to interval, jsonb and inet.
type Span struct {
	ID      int64
	Took    time.Duration
	Maybe   *time.Duration
	Payload json.RawMessage
	Hop     net.IP
}

func (Span) Configure(b *ant.ModelBuilder[Span]) { b.Table("spans") }

// Tracer has a span or none, for the outer join.
type Tracer struct {
	ID     int64
	SpanID *int64
	Span   *Span
}

func (Tracer) Configure(b *ant.ModelBuilder[Tracer]) { b.Table("tracers") }

// SpanDB holds Span and Tracer.
type SpanDB struct {
	ant.Context
	Spans   ant.Table[Span]
	Tracers ant.Table[Tracer]
}

var (
	Spans   = ant.Fields[Span]()
	Tracers = ant.Fields[Tracer]()
)

var blobs = map[string]string{
	"postgres":  "BYTEA",
	"mysql":     "VARBINARY(64)",
	"mariadb":   "VARBINARY(64)",
	"sqlite":    "BLOB",
	"sqlserver": "VARBINARY(64)",
}

// The values ant reads itself read as they did from integers and bytes on every
// database, and a NULL reads into a json.RawMessage or net.IP as nil.
func TestConformanceDurationsAndBytes(t *testing.T) {
	testdb.ForEach(t, func(t *testing.T, target testdb.Target) {
		ctx := t.Context()

		for _, ddl := range []string{
			fmt.Sprintf(
				`CREATE TABLE spans (id BIGINT PRIMARY KEY, took BIGINT NOT NULL, maybe BIGINT NULL, payload %[1]s NULL, hop %[1]s NULL)`,
				blobs[target.Name],
			),
			`CREATE TABLE tracers (id BIGINT PRIMARY KEY, span_id BIGINT NULL)`,
		} {
			_, err := target.DB.ExecContext(ctx, ddl)
			require.NoError(t, err)
		}

		t.Cleanup(func() {
			_, _ = target.DB.ExecContext(context.Background(), "DROP TABLE tracers")
			_, _ = target.DB.ExecContext(context.Background(), "DROP TABLE spans")
		})

		db, err := ant.Open[SpanDB](ctx, target.Dialect, target.DSN, ant.WithTracking(false))
		require.NoError(t, err)
		t.Cleanup(func() { db.Close() })

		maybe := -time.Nanosecond
		full := &Span{
			ID:      1,
			Took:    90*time.Second + 7,
			Maybe:   &maybe,
			Payload: json.RawMessage(`{"a":1}`),
			Hop:     net.IP{10, 0, 0, 1},
		}

		empty := &Span{ID: 2, Took: 10}
		require.NoError(t, db.Spans.Insert(ctx, full, empty))

		got, err := db.Spans.Find(ctx, full.ID)
		require.NoError(t, err)
		require.Equal(t, full, got, "to the nanosecond")

		got, err = db.Spans.Find(ctx, empty.ID)
		require.NoError(t, err)
		require.Equal(t, empty, got, "NULL is nil")

		took, err := ant.Pluck(ctx, db.Spans.OrderBy(&Spans.ID), &Spans.Took)
		require.NoError(t, err)
		require.Equal(t, []time.Duration{full.Took, empty.Took}, took)

		sum, err := ant.SumOf(ctx, db.Spans.Where(), &Spans.Took)
		require.NoError(t, err)
		require.Equal(t, full.Took+empty.Took, sum)

		average, err := ant.AvgOf(ctx, db.Spans.Where(), &Spans.Took)
		require.NoError(t, err)
		require.InDelta(t, float64(full.Took+empty.Took)/2, average, 1)

		n, err := db.Spans.Where(ant.In(&Spans.Took, full.Took, time.Hour)).Count(ctx)
		require.NoError(t, err)
		require.Equal(t, int64(1), n)

		require.NoError(t, db.Tracers.Insert(ctx, &Tracer{ID: 1, SpanID: &full.ID}, &Tracer{ID: 2}))

		tracers, err := db.Tracers.IncludeJoined(&Tracers.Span).OrderBy(&Tracers.ID).All(ctx)
		require.NoError(t, err)
		require.Len(t, tracers, 2)
		require.Equal(t, full, tracers[0].Span)
		require.Nil(t, tracers[1].Span)
	})
}
