package lifecycle

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/skudra-go/ant"
	"github.com/skudra-go/ant/tests/features/fixture"
)

// An ant:"version" field detects conflicting writes: a save only succeeds if the
// row still has the version that was loaded.
type Page struct {
	ID      int64
	Title   string
	Version int64 `ant:"version"`
}

// An ant:"token" field does the same with a value the application manages.
type Stock struct {
	ID       int64
	Quantity int
	ETag     *string `ant:"token"`
}

type WikiDB struct {
	ant.Context
	Pages  ant.Table[Page]
	Stocks ant.Table[Stock]
}

var (
	Pg = ant.Fields[Page]()
	Sk = ant.Fields[Stock]()
)

const wikiDDL = `
	CREATE TABLE pages (id BIGSERIAL PRIMARY KEY, title TEXT NOT NULL, version BIGINT NOT NULL);
	CREATE TABLE stocks (id BIGSERIAL PRIMARY KEY, quantity INT NOT NULL, e_tag TEXT)`

func TestOptimisticConcurrency(t *testing.T) {
	fixture.RunTrackedWith(t, wikiDDL, func(t *testing.T, db *WikiDB) {
		ctx := t.Context()

		t.Run("insert starts at 1, and a save moves it", func(t *testing.T) {
			page := &Page{Title: "a"}
			require.NoError(t, db.Pages.Insert(ctx, page))
			require.EqualValues(t, 1, page.Version)
			page.Title = "b"

			require.NoError(t, db.SaveChanges(ctx))
			require.EqualValues(t, 2, page.Version)
		})

		t.Run("no change keeps it", func(t *testing.T) {
			page := &Page{Title: "c"}
			require.NoError(t, db.Pages.Insert(ctx, page))
			require.NoError(t, db.SaveChanges(ctx))
			require.EqualValues(t, 1, page.Version)
		})

		t.Run("the second writer loses", func(t *testing.T) {
			page := &Page{Title: "race"}
			require.NoError(t, db.Pages.Insert(ctx, page))
			one, two := ant.Session(db), ant.Session(db)
			a, err := one.Pages.Find(ctx, page.ID)
			require.NoError(t, err)
			b, err := two.Pages.Find(ctx, page.ID)
			require.NoError(t, err)

			a.Title = "first"

			require.NoError(t, one.SaveChanges(ctx))

			b.Title = "second"
			err = two.SaveChanges(ctx)
			require.ErrorIs(t, err, ant.ErrConcurrency)

			var conflict *ant.ConcurrencyError
			require.True(t, errors.As(err, &conflict))
			require.Equal(t, "Page", conflict.Model)
			require.Equal(t, page.ID, conflict.Key)

			require.EqualValues(t, 1, b.Version, "the failed save leaves the version alone")

			require.NoError(t, two.Pages.Reload(ctx, b))
			require.Equal(t, "first", b.Title)
			b.Title = "second"

			require.NoError(t, two.SaveChanges(ctx), "after a reload it can win")
		})

		t.Run("a bulk update counts as a write", func(t *testing.T) {
			page := &Page{Title: "bulk"}
			session := ant.Session(db)
			require.NoError(t, session.Pages.Insert(ctx, page))

			_, err := db.Pages.Where(ant.Eq(&Pg.ID, page.ID)).
				Update(ctx, ant.Set(&Pg.Title, "other"))
			require.NoError(t, err)

			page.Title = "mine"

			require.ErrorIs(t, session.SaveChanges(ctx), ant.ErrConcurrency)
		})

		t.Run("a delete checks it too", func(t *testing.T) {
			page := &Page{Title: "gone"}
			session := ant.Session(db)
			require.NoError(t, session.Pages.Insert(ctx, page))
			_, err := db.Pages.Where(ant.Eq(&Pg.ID, page.ID)).Update(ctx, ant.Set(&Pg.Title, "x"))
			require.NoError(t, err)
			require.NoError(t, ant.Remove(ctx, session, page))
			require.ErrorIs(t, session.SaveChanges(ctx), ant.ErrConcurrency)
		})

		t.Run("a token the application sets", func(t *testing.T) {
			stock := &Stock{Quantity: 5, ETag: new("v1")}
			session := ant.Session(db)
			require.NoError(t, session.Stocks.Insert(ctx, stock))

			_, err := db.Stocks.Where(ant.Eq(&Sk.ID, stock.ID)).
				Update(ctx, ant.Set(&Sk.ETag, new("v2")))
			require.NoError(t, err)

			stock.Quantity = 4

			require.ErrorIs(t, session.SaveChanges(ctx), ant.ErrConcurrency)
		})

		t.Run("a NULL token compares", func(t *testing.T) {
			stock := &Stock{Quantity: 5}
			require.NoError(t, db.Stocks.Insert(ctx, stock))
			stock.Quantity = 4

			require.NoError(t, db.SaveChanges(ctx))
		})
	})
}
