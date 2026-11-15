package model_definition

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/skudra-go/ant"
	"github.com/skudra-go/ant/dialect/postgres"
	"github.com/skudra-go/ant/tests/features/fixture"
	"github.com/skudra-go/ant/tests/features/model_definition/modules/auth"
	"github.com/skudra-go/ant/tests/features/model_definition/modules/billing"
)

// By convention a type maps to its plural snake_case name: OrderItem to
// order_items.
type OrderItem struct {
	ID  int64
	Sku string
}

// Configure overrides the convention. Category would otherwise map to
// categories.
type Category struct {
	ID   int64
	Name string
}

func (Category) Configure(b *ant.ModelBuilder[Category]) {
	b.Table("product_groups")
}

type ShopDB struct {
	ant.Context
	OrderItems ant.Table[OrderItem]
	Categories ant.Table[Category]
}

var Categories = ant.Fields[Category]()

// A condition built before the database opens still uses the configured table.
var named = ant.Eq(&Categories.Name, "Tools")

func TestTableName(t *testing.T) {
	ddl := `CREATE TABLE order_items (id BIGSERIAL PRIMARY KEY, sku TEXT NOT NULL);
	        CREATE TABLE product_groups (id BIGSERIAL PRIMARY KEY, name TEXT NOT NULL)`
	fixture.RunWith(t, ddl, func(t *testing.T, db *ShopDB) {
		ctx := t.Context()

		t.Run("convention", func(t *testing.T) {
			require.NoError(t, db.OrderItems.Insert(ctx, &OrderItem{Sku: "A1"}))
			items, err := db.OrderItems.All(ctx)
			require.NoError(t, err)
			require.Len(t, items, 1)
		})

		t.Run("override", func(t *testing.T) {
			require.NoError(t, db.Categories.Insert(ctx, &Category{Name: "Tools"}))
			found, err := db.Categories.Where(named).Single(ctx)
			require.NoError(t, err)
			require.Equal(t, "Tools", found.Name)
		})
	})
}

// Two modules can each have an Account. Both would map to accounts by
// convention, which is never meant, so opening the context refuses them and
// names both types. Naming one table with b.Table settles it.
type ModulesDB struct {
	ant.Context
	BillingAccounts ant.Table[billing.Account]
	AuthAccounts    ant.Table[auth.Account]
}

// AccountSecret names the accounts table itself, which splits the table between
// two models on purpose.
type AccountSecret struct {
	ID       int64
	Password string
}

func (AccountSecret) Configure(b *ant.ModelBuilder[AccountSecret]) {
	b.Table("accounts")
}

type SplitDB struct {
	ant.Context
	Accounts ant.Table[auth.Account]
	Secrets  ant.Table[AccountSecret]
}

// Models that share a table through b.Discriminator may share a type name too.
type PlansDB struct {
	ant.Context
	BillingPlans ant.Table[billing.Plan]
	AuthPlans    ant.Table[auth.Plan]
}

// A model reached only through a relation counts when the schema is built.
type Ledger struct {
	ID        int64
	AccountID int64
	Account   *billing.Account
}

type LedgersDB struct {
	ant.Context
	Ledgers  ant.Table[Ledger]
	Accounts ant.Table[auth.Account]
}

func TestSameTypeNameInTwoPackages(t *testing.T) {
	const refused = `models billing.Account and auth.Account both map to table "accounts"`

	t.Run("refused when the context opens", func(t *testing.T) {
		_, err := ant.Wrap[ModulesDB](postgres.Dialect, nil)
		require.ErrorContains(t, err, refused)
		require.ErrorContains(t, err, "b.Table")
	})

	t.Run("refused when the schema is built", func(t *testing.T) {
		_, err := ant.SchemaOf[ModulesDB](postgres.Dialect)
		require.ErrorContains(t, err, refused)

		_, err = ant.SchemaOf[LedgersDB](postgres.Dialect)
		require.ErrorContains(
			t,
			err,
			`auth.Account and billing.Account both map to table "accounts"`,
		)
	})

	t.Run("a table named with b.Table can be shared", func(t *testing.T) {
		_, err := ant.Wrap[SplitDB](postgres.Dialect, nil)
		require.NoError(t, err)

		_, err = ant.SchemaOf[SplitDB](postgres.Dialect)
		require.NoError(t, err)
	})

	t.Run("so can one shared with a discriminator", func(t *testing.T) {
		_, err := ant.Wrap[PlansDB](postgres.Dialect, nil)
		require.NoError(t, err)

		s, err := ant.SchemaOf[PlansDB](postgres.Dialect)
		require.NoError(t, err)
		require.Len(t, s.Tables, 1)
	})
}
