package tests

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/skudra-go/ant"
	"github.com/skudra-go/ant/internal/testdb"
)

// Doc has a uuid key that nothing generates.
type Doc struct {
	ID    uuid.UUID
	Title string
}

// Slug has a text key that nothing generates.
type Slug struct {
	ID    string
	Title string
}

// Memo has a uuid key that ant generates.
type Memo struct {
	ID    uuid.UUID
	Title string
}

func (Memo) Configure(b *ant.ModelBuilder[Memo]) { ant.GenerateKey(b, &Memos.ID, uuid.New) }

// Seat has an integer key that the application gives, where zero is a key.
type Seat struct {
	ID    int64
	Title string
}

func (Seat) Configure(b *ant.ModelBuilder[Seat]) { b.Key(&Seats.ID).NotGenerated() }

// KeyDB holds a model of each kind of key.
type KeyDB struct {
	ant.Context
	Docs  ant.Table[Doc]
	Slugs ant.Table[Slug]
	Memos ant.Table[Memo]
	Seats ant.Table[Seat]
}

var (
	Docs  = ant.Fields[Doc]()
	Slugs = ant.Fields[Slug]()
	Memos = ant.Fields[Memo]()
	Seats = ant.Fields[Seat]()
)

func openKeys(t *testing.T, target testdb.Target) *KeyDB {
	t.Helper()
	ctx := t.Context()

	for _, ddl := range []string{
		`CREATE TABLE docs (id ` + uuids[target.Name] + ` PRIMARY KEY, title VARCHAR(40) NOT NULL)`,
		`CREATE TABLE slugs (id VARCHAR(40) PRIMARY KEY, title VARCHAR(40) NOT NULL)`,
		`CREATE TABLE memos (id ` + uuids[target.Name] + ` PRIMARY KEY, title VARCHAR(40) NOT NULL)`,
		`CREATE TABLE seats (id BIGINT PRIMARY KEY, title VARCHAR(40) NOT NULL)`,
	} {
		_, err := target.DB.ExecContext(ctx, ddl)
		require.NoError(t, err)
	}

	t.Cleanup(func() {
		for _, table := range []string{"docs", "slugs", "memos", "seats"} {
			_, _ = target.DB.ExecContext(context.Background(), "DROP TABLE "+table)
		}
	})

	db, err := ant.Open[KeyDB](ctx, target.Dialect, target.DSN)
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })

	return db
}

// A key that is its type's zero value, and that neither ant nor the database
// fills, is refused before the insert, naming the field and how to generate it.
// It was written as it was, and the next insert failed with a bare duplicate
// key (#297).
func TestConformanceZeroKeyIsRefused(t *testing.T) {
	testdb.ForEach(t, func(t *testing.T, target testdb.Target) {
		db := openKeys(t, target)
		ctx := t.Context()

		refused := func(t *testing.T, err error, field string) {
			t.Helper()
			require.ErrorContains(t, err, field)
			require.ErrorContains(t, err, "ant.GenerateKey")
		}

		count := func(t *testing.T) int64 {
			t.Helper()

			n, err := db.Docs.Count(ctx)
			require.NoError(t, err)

			return n
		}

		t.Run("Insert", func(t *testing.T) {
			refused(t, db.Docs.Insert(ctx, &Doc{Title: "a"}), "Doc.ID")
			refused(t, db.Slugs.Insert(ctx, &Slug{Title: "a"}), "Slug.ID")
			require.Zero(t, count(t))
		})

		t.Run("InsertMany", func(t *testing.T) {
			_, err := db.Docs.InsertMany(ctx, []*Doc{{ID: uuid.New(), Title: "a"}, {Title: "b"}})
			refused(t, err, "Doc.ID")
			require.Zero(t, count(t))
		})

		t.Run("Upsert", func(t *testing.T) {
			_, err := db.Docs.Upsert(ctx, &Doc{Title: "a"}, ant.OnConflict(&Docs.ID))
			refused(t, err, "Doc.ID")
		})

		t.Run("FirstOrCreate", func(t *testing.T) {
			_, _, err := db.Docs.FirstOrCreate(ctx, &Doc{Title: "a"}, ant.Eq(&Docs.Title, "a"))
			refused(t, err, "Doc.ID")
		})

		t.Run("SaveChanges", func(t *testing.T) {
			s := ant.Session(db)
			require.NoError(t, ant.Add(ctx, s, &Doc{Title: "a"}))
			refused(t, s.SaveChanges(ctx), "Doc.ID")
			require.Zero(t, count(t))
		})

		t.Run("SaveChanges of many", func(t *testing.T) {
			s := ant.Session(db)
			require.NoError(t, ant.Add(ctx, s, &Doc{ID: uuid.New(), Title: "a"}))
			require.NoError(t, ant.Add(ctx, s, &Doc{Title: "b"}))
			refused(t, s.SaveChanges(ctx), "Doc.ID")
			require.Zero(t, count(t))
		})

		t.Run("PrepareBulk", func(t *testing.T) {
			_, err := ant.PrepareBulk(db, []*Doc{{Title: "a"}})
			refused(t, err, "Doc.ID")
		})

		t.Run("a key given, generated or where zero is a key is written", func(t *testing.T) {
			require.NoError(t, db.Docs.Insert(ctx, &Doc{ID: uuid.New(), Title: "a"}))
			require.NoError(t, db.Slugs.Insert(ctx, &Slug{ID: "a", Title: "a"}))

			memo := &Memo{Title: "a"}
			require.NoError(t, db.Memos.Insert(ctx, memo))
			require.NotEqual(t, uuid.Nil, memo.ID)

			require.NoError(t, db.Seats.Insert(ctx, &Seat{Title: "zero"}))
		})
	})
}
