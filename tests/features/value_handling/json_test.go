package value_handling

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/skudra-go/ant"
	"github.com/skudra-go/ant/dialect/postgres"
	"github.com/skudra-go/ant/tests/features/fixture"
)

// ant.JSON stores a Go value in a JSON column.
type Prefs struct {
	Theme string   `json:"theme"`
	Size  int      `json:"size"`
	Tags  []string `json:"tags"`
}

type Device struct {
	ID    int64
	Name  string
	Prefs ant.JSON[Prefs]
	Extra *ant.JSON[map[string]int]
}

type DevicesDB struct {
	ant.Context
	Devices ant.Table[Device]
}

var Devices = ant.Fields[Device]()

func TestJSON(t *testing.T) {
	ddl := `CREATE TABLE devices (id BIGSERIAL PRIMARY KEY, name TEXT NOT NULL, prefs JSONB NOT NULL, extra JSONB)`
	fixture.RunTrackedWith(t, ddl, func(t *testing.T, db *DevicesDB) {
		ctx := t.Context()
		dark := Prefs{"dark", 12, []string{"a", "b"}}
		phone := &Device{Name: "phone", Prefs: ant.JSON[Prefs]{V: dark}}
		tablet := &Device{Name: "tablet", Prefs: ant.JSON[Prefs]{V: Prefs{Theme: "light"}}}
		require.NoError(t, db.Devices.Insert(ctx, phone, tablet))

		stored := func(id int64) *Device {
			got, err := db.Devices.NoTracking().Find(ctx, id)
			require.NoError(t, err)

			return got
		}

		t.Run("values round trip", func(t *testing.T) {
			require.Equal(t, dark, stored(phone.ID).Prefs.V)
		})

		t.Run("a nil pointer is NULL", func(t *testing.T) {
			require.Nil(t, stored(phone.ID).Extra)
		})

		t.Run("a change inside the value is tracked", func(t *testing.T) {
			phone.Prefs.V.Tags[0] = "changed"

			require.True(t, fixture.HasChanges(t, db))
			require.NoError(t, db.SaveChanges(ctx))
			require.Equal(t, "changed", stored(phone.ID).Prefs.V.Tags[0])
		})

		t.Run("JSONText reads inside the column", func(t *testing.T) {
			got, err := db.Devices.NoTracking().
				Where(postgres.JSONText(&Devices.Prefs, "theme").Eq("dark")).
				Single(ctx)
			require.NoError(t, err)
			require.Equal(t, "phone", got.Name)
		})

		t.Run("JSONText with a deeper path", func(t *testing.T) {
			first, err := ant.Pluck(
				ctx,
				db.Devices.Where(ant.Eq(&Devices.Name, "phone")),
				&Devices.Name,
			)
			require.NoError(t, err)
			require.Len(t, first, 1)

			n, err := db.Devices.Where(postgres.JSONText(&Devices.Prefs, "tags", "1").Eq("b")).
				Count(ctx)
			require.NoError(t, err)
			require.EqualValues(t, 1, n)
		})

		t.Run("JSONContains", func(t *testing.T) {
			n, err := db.Devices.Where(postgres.JSONContains(
				&Devices.Prefs,
				map[string]any{"theme": "light"},
			)).
				Count(ctx)
			require.NoError(t, err)
			require.EqualValues(t, 1, n)
		})

		t.Run("a value that cannot be JSON is an error", func(t *testing.T) {
			_, err := db.Devices.Where(postgres.JSONContains(&Devices.Prefs, make(chan int))).
				All(ctx)
			require.Error(t, err)
		})

		t.Run("JSONHasKey", func(t *testing.T) {
			n, err := db.Devices.Where(postgres.JSONHasKey(&Devices.Prefs, "size")).Count(ctx)
			require.NoError(t, err)
			require.EqualValues(t, 2, n)
			n, err = db.Devices.Where(postgres.JSONHasKey(&Devices.Prefs, "missing")).Count(ctx)
			require.NoError(t, err)
			require.Zero(t, n)
		})

		t.Run("JSONLength", func(t *testing.T) {
			n, err := db.Devices.Where(postgres.JSONLength(&Devices.Extra).Gt(1)).Count(ctx)
			require.NoError(t, err, "NULL has no length, so no row matches")
			require.Zero(t, n)
		})
	})
}
