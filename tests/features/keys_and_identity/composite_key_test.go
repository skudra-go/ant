package keys_and_identity

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/skudra-go/ant"
	"github.com/skudra-go/ant/tests/features/fixture"
)

// A composite key spans several columns. Use FindKey with its parts in order.
type Grant struct {
	UserID int64
	Scope  string
	Level  int
}

func (Grant) Configure(b *ant.ModelBuilder[Grant]) {
	b.CompositeKey(ant.F(&Gr.UserID), ant.F(&Gr.Scope))
}

type GrantsDB struct {
	ant.Context
	Grants ant.Table[Grant]
}

var Gr = ant.Fields[Grant]()

func TestCompositeKey(t *testing.T) {
	ddl := `CREATE TABLE grants (user_id BIGINT NOT NULL, scope TEXT NOT NULL, level INT NOT NULL, PRIMARY KEY (user_id, scope))`
	fixture.RunTrackedWith(t, ddl, func(t *testing.T, db *GrantsDB) {
		ctx := t.Context()

		read, write := &Grant{
			UserID: 1,
			Scope:  "read",
			Level:  1,
		}, &Grant{
			UserID: 1,
			Scope:  "write",
			Level:  2,
		}
		require.NoError(t, db.Grants.Insert(ctx, read, write))

		t.Run("FindKey", func(t *testing.T) {
			got, err := ant.Session(db).Grants.FindKey(ctx, int64(1), "write")
			require.NoError(t, err)
			require.Equal(t, 2, got.Level)
		})

		t.Run("not found", func(t *testing.T) {
			_, err := db.Grants.FindKey(ctx, int64(1), "none")
			require.ErrorIs(t, err, ant.ErrNotFound)
		})

		t.Run("the wrong number of parts", func(t *testing.T) {
			_, err := db.Grants.FindKey(ctx, int64(1))
			require.ErrorIs(t, err, ant.ErrKeyType)
		})

		t.Run("Find needs a single key", func(t *testing.T) {
			_, err := db.Grants.Find(ctx, 1)
			require.ErrorIs(t, err, ant.ErrNoKey)
		})

		t.Run("identity spans the whole key", func(t *testing.T) {
			session := ant.Session(db)
			rows, err := session.Grants.OrderBy(&Gr.Scope).All(ctx)
			require.NoError(t, err)
			require.Len(t, rows, 2)
			require.NotSame(t, rows[0], rows[1])

			again, err := session.Grants.FindKey(ctx, int64(1), "read")
			require.NoError(t, err)
			require.Same(t, rows[0], again)
		})

		t.Run("SaveChanges updates the right row", func(t *testing.T) {
			read.Level = 9

			require.NoError(t, db.SaveChanges(ctx))
			got, err := db.Grants.NoTracking().Where(ant.Eq(&Gr.Scope, "read")).Single(ctx)
			require.NoError(t, err)
			require.Equal(t, 9, got.Level)

			other, err := db.Grants.NoTracking().Where(ant.Eq(&Gr.Scope, "write")).Single(ctx)
			require.NoError(t, err)
			require.Equal(t, 2, other.Level)
		})

		t.Run("Remove deletes the right row", func(t *testing.T) {
			require.NoError(t, ant.Remove(ctx, db, read))
			require.NoError(t, db.SaveChanges(ctx))
			n, err := db.Grants.Count(ctx)
			require.NoError(t, err)
			require.EqualValues(t, 1, n)
		})

		t.Run("changing part of the key is refused", func(t *testing.T) {
			write.Scope = "other"

			require.ErrorIs(t, db.SaveChanges(ctx), ant.ErrKeyChanged)

			write.Scope = "write"
		})

		t.Run("Reload", func(t *testing.T) {
			_, err := db.Grants.Where(ant.Eq(&Gr.Scope, "write")).Update(ctx, ant.Set(&Gr.Level, 7))
			require.NoError(t, err)
			require.NoError(t, db.Grants.Reload(ctx, write))
			require.Equal(t, 7, write.Level)
		})
	})
}

// GenerateKey fills a key in Go, such as a UUID, before the insert.
type Order struct {
	Code  string
	Total int
}

var nextCode = 0

func (Order) Configure(b *ant.ModelBuilder[Order]) {
	ant.GenerateKey(b, &Or.Code, func() string {
		nextCode++

		return "order-" + string(rune('0'+nextCode))
	})
}

type OrdersDB struct {
	ant.Context
	Orders ant.Table[Order]
}

var Or = ant.Fields[Order]()

func TestGenerateKey(t *testing.T) {
	ddl := `CREATE TABLE orders (code TEXT PRIMARY KEY, total INT NOT NULL)`
	fixture.RunWith(t, ddl, func(t *testing.T, db *OrdersDB) {
		ctx := t.Context()

		t.Run("a zero key is generated", func(t *testing.T) {
			order := &Order{Total: 5}
			require.NoError(t, db.Orders.Insert(ctx, order))
			require.Contains(t, order.Code, "order-")
		})

		t.Run("a supplied key is kept", func(t *testing.T) {
			order := &Order{Code: "mine"}
			require.NoError(t, db.Orders.Insert(ctx, order))
			require.Equal(t, "mine", order.Code)
		})

		t.Run("a failed insert clears the generated key", func(t *testing.T) {
			first := &Order{Code: "taken"}
			require.NoError(t, db.Orders.Insert(ctx, first))

			fresh, clash := &Order{}, &Order{Code: "taken"}
			require.Error(t, db.Orders.Insert(ctx, fresh, clash))
			require.Empty(t, fresh.Code)
		})

		t.Run("InsertMany generates too", func(t *testing.T) {
			order := &Order{}
			_, err := db.Orders.InsertMany(ctx, []*Order{order})
			require.NoError(t, err)
			require.NotEmpty(t, order.Code)
		})
	})
}
