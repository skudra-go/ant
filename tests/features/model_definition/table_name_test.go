package model_definition

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/skudra-go/ant"
	"github.com/skudra-go/ant/tests/features/fixture"
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
