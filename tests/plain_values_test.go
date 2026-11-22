package tests

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/skudra-go/ant"
	"github.com/skudra-go/ant/internal/testdb"
)

// Street is a plain struct, with no driver.Valuer and no sql.Scanner.
type Street struct{ City string }

// MovedAt is a time.Time by another name, which database/sql does not take as one.
type MovedAt time.Time

// Resident has a field of each type only pgx converts.
type Resident struct {
	ID    int64
	Home  Street
	Tags  []string
	Attrs map[string]string
	Moved MovedAt
}

type ResidentDB struct {
	ant.Context
	Residents ant.Table[Resident]
}

// Reader only reads its map, which ant reads from JSON on every database.
type Reader struct {
	ID    int64
	Attrs map[string]any `ant:"readonly"`
	Home  ant.JSON[Street]
}

type ReaderDB struct {
	ant.Context
	Readers ant.Table[Reader]
}

// PointerDB declares its table as a pointer, which ant does not bind.
type PointerDB struct {
	ant.Context
	Residents *ant.Table[Reader]
}

// Open refuses a model it can map but whose values the driver can't take, so a
// mistake shows at startup with the field named, rather than as the driver's
// "unsupported type" at the first write. pgx converts structs, slices and maps
// itself, so PostgreSQL leaves them to it (#300).
func TestConformanceOpenRefusesValuesTheDriverCannotTake(t *testing.T) {
	testdb.ForEach(t, func(t *testing.T, target testdb.Target) {
		ctx := t.Context()

		t.Run("a struct, a slice or a map", func(t *testing.T) {
			db, err := ant.Open[ResidentDB](ctx, target.Dialect, target.DSN)

			if target.Name == "postgres" {
				require.NoError(t, err)
				require.NoError(t, db.Close())

				return
			}

			require.Error(t, err)

			for _, field := range []string{"Resident.Home", "Resident.Tags", "Resident.Attrs", "Resident.Moved"} {
				require.ErrorContains(t, err, field)
			}

			require.ErrorContains(t, err, "ant.JSON")
		})

		t.Run("a map that is only read, and ant.JSON", func(t *testing.T) {
			db, err := ant.Open[ReaderDB](ctx, target.Dialect, target.DSN)
			require.NoError(t, err)
			require.NoError(t, db.Close())
		})

		t.Run("a table declared as a pointer", func(t *testing.T) {
			_, err := ant.Open[PointerDB](ctx, target.Dialect, target.DSN)
			require.ErrorContains(t, err, "Residents")
			require.ErrorContains(t, err, "ant.Table[")
		})
	})
}
