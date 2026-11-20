package value_handling

import (
	"encoding/json"
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/skudra-go/ant"
	"github.com/skudra-go/ant/internal/testdb"
	"github.com/skudra-go/ant/tests/features/fixture"
)

// Reading has one field of each type whose column type ant maps.
type Reading struct {
	ID      int64
	Payload json.RawMessage
	Took    time.Duration
	Hop     net.IP
}

// ReadingDB holds Reading.
type ReadingDB struct {
	ant.Context
	Readings ant.Table[Reading]
}

// The table EnsureCreated makes from the model has the column types ant maps,
// and the values come back as they went in.
func TestNativeTypesFromModels(t *testing.T) {
	fixture.Each(t, func(t *testing.T, target testdb.Target) {
		db := fixture.Open[ReadingDB](t, target)
		ctx := t.Context()
		require.NoError(t, db.EnsureCreated(ctx))

		t.Run("columns take the types ant maps", func(t *testing.T) {
			want := map[string]string{
				"payload": "jsonb",
				"took":    "bigint",
				"hop":     "bytea",
			}

			for column, typ := range want {
				var got string

				err := target.DB.QueryRowContext(
					ctx,
					`SELECT data_type FROM information_schema.columns
					 WHERE table_schema = current_schema()
					   AND table_name = 'readings'
					   AND column_name = $1`,
					column,
				).Scan(&got)
				require.NoError(t, err, column)
				require.Equal(t, typ, got, column)
			}
		})

		t.Run("values read back", func(t *testing.T) {
			hop := net.ParseIP("10.0.0.1").To4()
			reading := &Reading{
				Payload: json.RawMessage(`{"a": 1}`),
				Took:    90 * time.Second,
				Hop:     hop,
			}
			require.NoError(t, db.Readings.Insert(ctx, reading))

			got, err := db.Readings.NoTracking().Find(ctx, reading.ID)
			require.NoError(t, err)
			require.JSONEq(t, `{"a": 1}`, string(got.Payload))
			require.Equal(t, 90*time.Second, got.Took)
			require.True(t, got.Hop.Equal(hop))
		})
	})
}
