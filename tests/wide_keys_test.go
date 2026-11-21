package tests

import (
	"context"
	"math"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/skudra-go/ant"
	"github.com/skudra-go/ant/internal/testdb"
)

// WideOwner has a uint64 key the application gives, such as a hash, which can
// pass math.MaxInt64.
type WideOwner struct {
	ID    uint64
	Name  string
	Items []WideItem
	Tags  []WideTag
}

func (WideOwner) Configure(b *ant.ModelBuilder[WideOwner]) {
	b.Table("wide_owners")
	b.Key(&WideOwners.ID).NotGenerated()
	b.ManyToMany(&WideOwners.Tags).Through("wide_owner_tags")
}

type WideItem struct {
	ID          int64
	WideOwnerID uint64
	WideOwner   *WideOwner
}

func (WideItem) Configure(b *ant.ModelBuilder[WideItem]) {
	b.Table("wide_items")
	b.Key(&WideItems.ID).NotGenerated()
}

type WideTag struct {
	ID   uint64
	Name string
}

func (WideTag) Configure(b *ant.ModelBuilder[WideTag]) {
	b.Table("wide_tags")
	b.Key(&WideTags.ID).NotGenerated()
}

var (
	WideOwners = ant.Fields[WideOwner]()
	WideItems  = ant.Fields[WideItem]()
	WideTags   = ant.Fields[WideTag]()
)

type WideDB struct {
	ant.Context
	WideOwners ant.Table[WideOwner]
	WideItems  ant.Table[WideItem]
	WideTags   ant.Table[WideTag]
}

// A uint64 key past math.MaxInt64 reaches every place ant reads a key: the
// foreign keys of a graph it inserts, Include in both directions, a
// many-to-many link, the tracker, a conflict's error and DeleteByID. ant read
// it as an int64, which wrapped to a negative number, so inserting an owner
// with its items failed to set their foreign key (#206).
func TestWideUnsignedKeys(t *testing.T) {
	testdb.ForEach(t, func(t *testing.T, target testdb.Target) {
		wide := map[string]string{
			"postgres": "NUMERIC(20,0)",
			"mysql":    "BIGINT UNSIGNED",
			"mariadb":  "BIGINT UNSIGNED",
		}[target.Name]

		if wide == "" {
			// database/sql refuses the value for these drivers: "uint64 values with
			// high bit set are not supported".
			t.Skip("the " + target.Name + " driver takes no uint64 past math.MaxInt64")
		}

		ctx := t.Context()

		for _, ddl := range []string{
			"CREATE TABLE wide_owners (id " + wide + " PRIMARY KEY, name VARCHAR(20) NOT NULL)",
			"CREATE TABLE wide_items (id BIGINT PRIMARY KEY, wide_owner_id " + wide + " NOT NULL)",
			"CREATE TABLE wide_tags (id " + wide + " PRIMARY KEY, name VARCHAR(20) NOT NULL)",
			"CREATE TABLE wide_owner_tags (wide_owner_id " + wide + " NOT NULL, wide_tag_id " + wide + " NOT NULL)",
		} {
			_, err := target.DB.ExecContext(ctx, ddl)
			require.NoError(t, err)
		}

		t.Cleanup(func() {
			for _, table := range []string{"wide_owner_tags", "wide_tags", "wide_items", "wide_owners"} {
				_, _ = target.DB.ExecContext(context.Background(), "DROP TABLE "+table)
			}
		})

		db, err := ant.Open[WideDB](ctx, target.Dialect, target.DSN)
		require.NoError(t, err)

		t.Cleanup(func() { _ = db.Close() })

		big := uint64(math.MaxInt64) + 7
		tag := big + 1

		owner := &WideOwner{
			ID:    big,
			Name:  "owner",
			Items: []WideItem{{ID: 1}, {ID: 2}},
		}

		s := ant.Session(db)
		require.NoError(t, s.WideOwners.Insert(ctx, owner))

		label := &WideTag{ID: tag, Name: "tag"}
		require.NoError(t, db.WideTags.Insert(ctx, label))
		require.NoError(t, ant.Link(ctx, db, owner, &WideOwners.Tags, label))

		owners, err := db.WideOwners.
			Include(&WideOwners.Items).
			Include(&WideOwners.Tags).
			All(ctx)
		require.NoError(t, err)
		require.Len(t, owners, 1)
		require.Equal(t, big, owners[0].ID)
		require.Len(t, owners[0].Items, 2)
		require.Equal(t, big, owners[0].Items[0].WideOwnerID)
		require.Len(t, owners[0].Tags, 1)
		require.Equal(t, tag, owners[0].Tags[0].ID)

		items, err := db.WideItems.IncludeOne(&WideItems.WideOwner).All(ctx)
		require.NoError(t, err)
		require.Len(t, items, 2)
		require.NotNil(t, items[0].WideOwner)
		require.Equal(t, big, items[0].WideOwner.ID)

		t.Run("the tracker finds the row by its key", func(t *testing.T) {
			s := ant.Session(db)
			first, err := s.WideOwners.Find(ctx, big)
			require.NoError(t, err)

			again, err := s.WideOwners.Find(ctx, big)
			require.NoError(t, err)
			require.Same(t, first, again)

			first.Name = "renamed"

			require.NoError(t, s.SaveChanges(ctx))
		})

		t.Run("Toggle finds the link it removes", func(t *testing.T) {
			tags := func() int {
				got, err := db.WideOwners.Include(&WideOwners.Tags).All(ctx)
				require.NoError(t, err)

				return len(got[0].Tags)
			}

			require.NoError(t, ant.Toggle(ctx, db, owner, &WideOwners.Tags, label))
			require.Zero(t, tags(), "the link is gone, not added again")

			require.NoError(t, ant.Toggle(ctx, db, owner, &WideOwners.Tags, label))
			require.Equal(t, 1, tags())
		})

		t.Run("a conflict names the key as it is", func(t *testing.T) {
			s := ant.Session(db)
			loaded, err := s.WideOwners.Find(ctx, big)
			require.NoError(t, err)

			_, err = db.WideOwners.Where(ant.Eq(&WideOwners.ID, big)).Delete(ctx)
			require.NoError(t, err)

			loaded.Name = "gone"

			var conflict *ant.ConcurrencyError
			require.ErrorAs(t, s.SaveChanges(ctx), &conflict)
			require.Equal(t, big, conflict.Key)
		})

		t.Run("DeleteByID finds it", func(t *testing.T) {
			require.NoError(t, db.WideTags.DeleteByID(ctx, tag))
			require.ErrorIs(t, db.WideTags.DeleteByID(ctx, tag), ant.ErrNotFound)
		})
	})
}
