package model_definition

import (
	"database/sql/driver"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/skudra-go/ant"
	"github.com/skudra-go/ant/tests/features/fixture"
)

// Any type with sql.Scanner and driver.Valuer works as a column.
type Money struct {
	Cents int64
}

func (m Money) Value() (driver.Value, error) {
	return fmt.Sprintf("%d.%02d", m.Cents/100, m.Cents%100), nil
}

func (m *Money) Scan(src any) error {
	text, ok := src.(string)
	if !ok {
		return fmt.Errorf("money: unexpected %T", src)
	}

	whole, fraction, found := strings.Cut(text, ".")
	if !found {
		return errors.New("money: no decimal point")
	}

	dollars, err := strconv.ParseInt(whole, 10, 64)
	if err != nil {
		return err
	}

	cents, err := strconv.ParseInt(fraction, 10, 64)
	if err != nil {
		return err
	}

	m.Cents = dollars*100 + cents

	return nil
}

// A named basic type needs no methods.
type Status string

type Invoice struct {
	ID       int64
	Total    Money  // a struct, but one that is a single column
	Discount *Money // a pointer to it is nullable
	Status   Status
}

type InvoiceDB struct {
	ant.Context
	Invoices ant.Table[Invoice]
}

var Invoices = ant.Fields[Invoice]()

func TestCustomTypes(t *testing.T) {
	ddl := `CREATE TABLE invoices (
		id BIGSERIAL PRIMARY KEY,
		total NUMERIC(10, 2) NOT NULL,
		discount NUMERIC(10, 2),
		status TEXT NOT NULL)`
	fixture.RunWith(t, ddl, func(t *testing.T, db *InvoiceDB) {
		ctx := t.Context()

		t.Run("values round trip", func(t *testing.T) {
			invoice := &Invoice{Total: Money{1250}, Discount: &Money{100}, Status: "paid"}
			require.NoError(t, db.Invoices.Insert(ctx, invoice))

			found, err := db.Invoices.Find(ctx, invoice.ID)
			require.NoError(t, err)
			require.Equal(t, *invoice.Discount, *found.Discount)
			require.Equal(t, invoice.Total, found.Total)
			require.Equal(t, Status("paid"), found.Status)
		})

		t.Run("nil pointer is NULL", func(t *testing.T) {
			invoice := &Invoice{Total: Money{500}, Status: "open"}
			require.NoError(t, db.Invoices.Insert(ctx, invoice))

			found, err := db.Invoices.Find(ctx, invoice.ID)
			require.NoError(t, err)
			require.Nil(t, found.Discount)
		})

		t.Run("conditions use the converted value", func(t *testing.T) {
			found, err := db.Invoices.Where(ant.Eq(&Invoices.Total, Money{1250})).Single(ctx)
			require.NoError(t, err)
			require.Equal(t, Status("paid"), found.Status)
		})

		t.Run("named types work in conditions", func(t *testing.T) {
			found, err := db.Invoices.Where(ant.Eq(&Invoices.Status, "open")).Single(ctx)
			require.NoError(t, err)
			require.Equal(t, Money{500}, found.Total)

			_, err = db.Invoices.Where(ant.Like(&Invoices.Status, "pa%")).Single(ctx)
			require.NoError(t, err)
		})

		t.Run("a failed Scan is returned", func(t *testing.T) {
			_, err := ant.Raw[Invoice](db, `SELECT 'free' AS total`).First(ctx)
			require.ErrorContains(t, err, "no decimal point")
		})
	})
}
