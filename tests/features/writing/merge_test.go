package writing

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/skudra-go/ant"
	"github.com/skudra-go/ant/dialect/postgres/pgxext"
	"github.com/skudra-go/ant/tests/features/fixture"
)

// Stock is a table that is synchronized from a feed.
type Stock struct {
	ID    int64
	SKU   string `ant:"unique"`
	Count int
}

// Staged is a feed waiting to be applied.
type Staged struct {
	ID    int64
	SKU   string
	Count int
}

// MergeDB holds them.
type MergeDB struct {
	ant.Context
	Stock  ant.Table[Stock]
	Staged ant.Table[Staged]
}

// Probes.
var (
	Stocks  = ant.Fields[Stock]()
	Stageds = ant.Fields[Staged]()
)

const mergeDDL = `
	CREATE TABLE stocks (id BIGSERIAL PRIMARY KEY, sku TEXT NOT NULL UNIQUE, count INT NOT NULL);
	CREATE TABLE stageds (id BIGSERIAL PRIMARY KEY, sku TEXT NOT NULL, count INT NOT NULL)`

// Merge decides per row what a batch of source rows does to a table: here an
// update, an insert, and a delete, in one statement. Upsert only does the first two.
func TestMerge(t *testing.T) {
	fixture.RunWith(t, mergeDDL, func(t *testing.T, db *MergeDB) {
		ctx := t.Context()
		version, err := ant.Raw[int](
			db,
			"SELECT current_setting('server_version_num')::int",
		).First(ctx)
		require.NoError(t, err)

		if *version < 150000 {
			t.Skip("MERGE needs PostgreSQL 15")
		}

		require.NoError(
			t,
			db.Stock.Insert(ctx, &Stock{SKU: "kept", Count: 1}, &Stock{SKU: "sold-out", Count: 5}),
		)

		require.NoError(t, db.Staged.Insert(
			ctx,
			&Staged{SKU: "kept", Count: 9},
			&Staged{SKU: "sold-out", Count: 0},
			&Staged{SKU: "new", Count: 3},
		))

		changed, err := ant.Merge(&db.Stock, db.Staged.Query(), ant.EqCol(&Stocks.SKU, &Stageds.SKU)).
			WhenMatchedIf(ant.Eq(&Stageds.Count, 0)).
			Delete().
			WhenMatched(ant.SetExpr(&Stocks.Count, ant.Col(&Stageds.Count))).
			WhenNotMatched(ant.Into(&Stocks.SKU, &Stageds.SKU), ant.Into(&Stocks.Count, &Stageds.Count)).
			Exec(ctx)
		require.NoError(t, err)
		require.EqualValues(t, 3, changed)

		rows, err := db.Stock.OrderBy(&Stocks.SKU).All(ctx)
		require.NoError(t, err)
		require.Len(t, rows, 2)
		require.Equal(t, "kept", rows[0].SKU)
		require.Equal(t, 9, rows[0].Count)
		require.Equal(t, "new", rows[1].SKU)

		t.Run("with no clause there is nothing to run", func(t *testing.T) {
			_, err := ant.Merge(&db.Stock, db.Staged.Query()).Exec(ctx)
			require.ErrorContains(t, err, "no WHEN clause")
		})

		t.Run("Delete only follows a matched clause with no assignments", func(t *testing.T) {
			_, err := ant.Merge(&db.Stock, db.Staged.Query()).
				WhenMatched(ant.Set(&Stocks.Count, 1)).
				Delete().
				Exec(ctx)
			require.ErrorContains(t, err, "Delete follows WhenMatched")
		})
	})
}

// Copy is the fast path for loading many rows: timestamps are filled in, and keys
// from the database do not come back.
func TestCopy(t *testing.T) {
	fixture.Run(t, func(t *testing.T, db *fixture.AppDB) {
		ctx := t.Context()
		users := make([]*fixture.User, 0, 1000)

		for i := range 1000 {
			users = append(
				users,
				&fixture.User{Name: "copied", Email: fmt.Sprintf("u%d@example.com", i), Age: i},
			)
		}

		copied, err := pgxext.Copy(ctx, db, users)
		require.NoError(t, err)
		require.EqualValues(t, 1000, copied)

		loaded, err := db.Users.Where(ant.Eq(&Users.Age, 7)).First(ctx)
		require.NoError(t, err)
		require.False(t, loaded.CreatedAt.IsZero())
		require.NotZero(t, loaded.ID, "the database assigned it")

		t.Run("no rows is an error", func(t *testing.T) {
			_, err := pgxext.Copy[fixture.User](ctx, db, nil)
			require.ErrorIs(t, err, ant.ErrNoRows)
		})
	})
}

// A Batch sends statements in one round trip, as one transaction.
func TestBatch(t *testing.T) {
	fixture.Run(t, func(t *testing.T, db *fixture.AppDB) {
		ctx := t.Context()
		alice, bob, _ := fixture.Seed(t, db)

		var batch pgxext.Batch
		batch.Add(
			db.Users.Where(ant.Eq(&Users.ID, alice.ID)).UpdateSQL(ctx, ant.Set(&Users.Age, 41)),
		)

		batch.Add(db.Users.Where(ant.Eq(&Users.ID, bob.ID)).DeleteSQL(ctx))
		batch.Exec("UPDATE users SET age = age + 1 WHERE id = $1", alice.ID)

		affected, err := batch.Send(ctx, db)
		require.NoError(t, err)
		require.Equal(t, []int64{1, 1, 1}, affected)

		stored, err := db.Users.Find(ctx, alice.ID)
		require.NoError(t, err)
		require.Equal(t, 42, stored.Age)

		t.Run("a failure undoes the whole batch", func(t *testing.T) {
			var batch pgxext.Batch
			batch.Exec("UPDATE users SET age = 1").Exec("SELECT 1 FROM nowhere")
			_, err := batch.Send(ctx, db)
			require.Error(t, err)
			stored, err := db.Users.Find(ctx, alice.ID)
			require.NoError(t, err)
			require.Equal(t, 42, stored.Age)
		})
	})
}
