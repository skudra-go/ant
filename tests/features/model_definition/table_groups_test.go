package model_definition

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/skudra-go/ant"
	"github.com/skudra-go/ant/dialect/postgres"
	"github.com/skudra-go/ant/tests/features/fixture"
)

// A large application can group each domain's tables in a struct of its own,
// embedded in the context or as a named field.
type Bill struct {
	ID    int64
	Total int64
}

type Cust struct {
	ID   int64
	Name string
}

type BillingTables struct {
	Bills ant.Table[Bill]
}

type SalesTables struct {
	Custs ant.Table[Cust]
}

type GroupedDB struct {
	ant.Context
	BillingTables
	Sales SalesTables
}

func TestTableGroups(t *testing.T) {
	const ddl = `
		CREATE TABLE bills (id BIGSERIAL PRIMARY KEY, total BIGINT NOT NULL);
		CREATE TABLE custs (id BIGSERIAL PRIMARY KEY, name TEXT NOT NULL)`

	fixture.RunWith(t, ddl, func(t *testing.T, db *GroupedDB) {
		ctx := t.Context()

		require.NoError(t, db.Bills.Insert(ctx, &Bill{Total: 10}))
		require.NoError(t, db.Sales.Custs.Insert(ctx, &Cust{Name: "Ada"}))

		n, err := db.Bills.Count(ctx)
		require.NoError(t, err)
		require.EqualValues(t, 1, n, "an embedded group")

		n, err = db.Sales.Custs.Count(ctx)
		require.NoError(t, err)
		require.EqualValues(t, 1, n, "a named group")

		t.Run("in a transaction and a session", func(t *testing.T) {
			require.NoError(t, db.Tx(ctx, func(tx *GroupedDB) error {
				_, err := tx.Sales.Custs.Count(ctx)

				return err
			}))

			_, err := ant.Session(db).Bills.Count(ctx)
			require.NoError(t, err)
		})
	})

	t.Run("the schema has every group's tables", func(t *testing.T) {
		s, err := ant.SchemaOf[GroupedDB](postgres.Dialect)
		require.NoError(t, err)

		var names []string

		for _, table := range s.Tables {
			names = append(names, table.Name)
		}

		require.ElementsMatch(t, []string{"bills", "custs"}, names)
	})
}

// An unexported Table would be left unbound, and fail at first use with an
// error about something else.
type hiddenTables struct {
	Custs ant.Table[Cust]
}

type HiddenFieldDB struct {
	ant.Context
	custs ant.Table[Cust]
}

type HiddenGroupDB struct {
	ant.Context
	sales hiddenTables
}

func TestUnexportedTablesAreRefused(t *testing.T) {
	_, err := ant.Wrap[HiddenFieldDB](postgres.Dialect, nil)
	require.ErrorIs(t, err, ant.ErrBadSchema)
	require.ErrorContains(t, err, "custs")

	_, err = ant.Wrap[HiddenGroupDB](postgres.Dialect, nil)
	require.ErrorIs(t, err, ant.ErrBadSchema)
	require.ErrorContains(t, err, "sales.Custs")

	_, err = ant.SchemaOf[HiddenFieldDB](postgres.Dialect)
	require.ErrorIs(t, err, ant.ErrBadSchema)

	var (
		_ = HiddenFieldDB{}.custs
		_ = HiddenGroupDB{}.sales
	)
}
