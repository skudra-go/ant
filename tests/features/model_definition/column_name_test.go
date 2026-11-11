package model_definition

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/skudra-go/ant"
	"github.com/skudra-go/ant/tests/features/fixture"
)

// Field names map to snake_case, and initialisms stay together. The column tag
// overrides this.
type Device struct {
	ID       int64
	OwnerID  int64  // owner_id
	HTTPPort int    // http_port
	Name     string `ant:"column=display_name"`
}

type DeviceDB struct {
	ant.Context
	Devices ant.Table[Device]
}

var Devices = ant.Fields[Device]()

func TestColumnName(t *testing.T) {
	ddl := `CREATE TABLE devices (
		id BIGSERIAL PRIMARY KEY,
		owner_id BIGINT NOT NULL,
		http_port INT NOT NULL,
		display_name TEXT NOT NULL)`
	fixture.RunWith(t, ddl, func(t *testing.T, db *DeviceDB) {
		ctx := t.Context()
		require.NoError(
			t,
			db.Devices.Insert(ctx, &Device{OwnerID: 7, HTTPPort: 8080, Name: "Router"}),
		)

		t.Run("queries use the mapped column", func(t *testing.T) {
			found, err := db.Devices.Where(ant.Eq(&Devices.Name, "Router"), ant.Eq(&Devices.HTTPPort, 8080)).
				Single(ctx)
			require.NoError(t, err)
			require.EqualValues(t, 7, found.OwnerID)
		})

		t.Run("raw SQL sees the database name", func(t *testing.T) {
			name, err := ant.Raw[string](db, `SELECT display_name FROM devices`).First(ctx)
			require.NoError(t, err)
			require.Equal(t, "Router", *name)
		})

		t.Run("raw rows scan by mapped column", func(t *testing.T) {
			found, err := ant.Raw[Device](db, `SELECT display_name FROM devices`).First(ctx)
			require.NoError(t, err)
			require.Equal(t, "Router", found.Name)
		})
	})
}
