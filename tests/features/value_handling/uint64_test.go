package value_handling

import (
	"math"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/skudra-go/ant"
	"github.com/skudra-go/ant/internal/testdb"
	"github.com/skudra-go/ant/tests/features/fixture"
)

// Wide holds values above int64's maximum, which only a uint64 field admits.
type Wide struct {
	ID      int64
	Mask    uint64  // numeric(20,0), past bigint
	Narrow  uint32  // bigint
	Pointer *uint64 // numeric(20,0), NULL allowed
}

type WidesDB struct {
	ant.Context
	Wides ant.Table[Wide]
}

var Wides = ant.Fields[Wide]()

func TestUint64KeepsEveryValue(t *testing.T) {
	fixture.Each(t, func(t *testing.T, target testdb.Target) {
		// bigint stops at MaxInt64, so the column type only matters where a value
		// can exceed it; the other dialects store uint64 in an integer column and
		// are out of scope here.
		if target.Dialect.Name() != "postgres" {
			t.Skip("numeric(20,0) is PostgreSQL's")
		}

		db := fixture.Open[WidesDB](t, target)
		ctx := t.Context()
		require.NoError(t, db.EnsureCreated(ctx))

		t.Run("the column holds the whole range", func(t *testing.T) {
			var got string

			err := target.DB.QueryRowContext(
				ctx,
				`SELECT format_type(atttypid, atttypmod) FROM pg_attribute
				 WHERE attrelid = 'wides'::regclass AND attname = $1`,
				"mask",
			).Scan(&got)
			require.NoError(t, err)
			require.Equal(t, "numeric(20,0)", got)
		})

		max := uint64(math.MaxUint64)
		row := &Wide{
			ID:      1,
			Mask:    max,
			Narrow:  math.MaxUint32,
			Pointer: &max,
		}
		require.NoError(t, db.Wides.Insert(ctx, row))

		got, err := db.Wides.Find(ctx, row.ID)
		require.NoError(t, err)
		require.Equal(t, max, got.Mask, "the largest uint64 must survive the round trip")
		require.Equal(t, uint64(math.MaxUint32), uint64(got.Narrow))
		require.NotNil(t, got.Pointer)
		require.Equal(t, max, *got.Pointer)

		t.Run("and a value just past int64", func(t *testing.T) {
			over := uint64(math.MaxInt64) + 1
			edge := &Wide{ID: 2, Mask: over}
			require.NoError(t, db.Wides.Insert(ctx, edge))

			back, err := db.Wides.Find(ctx, edge.ID)
			require.NoError(t, err)
			require.Equal(t, over, back.Mask)
		})
	})
}
