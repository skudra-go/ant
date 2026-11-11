package relations

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/skudra-go/ant"
	"github.com/skudra-go/ant/tests/features/fixture"
)

// A model can relate to itself. The parent is found through ParentID, and the
// children through the matching back-reference.
type Folder struct {
	ID       int64
	ParentID *int64
	Name     string
	Parent   *Folder
	Children []Folder
}

type DriveDB struct {
	ant.Context
	Folders ant.Table[Folder]
}

var Folders = ant.Fields[Folder]()

func TestSelfReference(t *testing.T) {
	ddl := `CREATE TABLE folders (id BIGSERIAL PRIMARY KEY, parent_id BIGINT REFERENCES folders, name TEXT NOT NULL)`
	fixture.RunWith(t, ddl, func(t *testing.T, db *DriveDB) {
		ctx := t.Context()
		root := &Folder{Name: "root"}
		require.NoError(t, db.Folders.Insert(ctx, root))
		docs := &Folder{Name: "docs", ParentID: &root.ID}
		require.NoError(t, db.Folders.Insert(ctx, docs))
		require.NoError(t, db.Folders.Insert(ctx, &Folder{Name: "2026", ParentID: &docs.ID}))

		t.Run("children", func(t *testing.T) {
			found, err := db.Folders.Where(ant.Eq(&Folders.Name, "root")).
				Include(&Folders.Children).
				Single(ctx)
			require.NoError(t, err)
			require.Len(t, found.Children, 1)
			require.Equal(t, "docs", found.Children[0].Name)
		})

		t.Run("parent", func(t *testing.T) {
			found, err := db.Folders.Where(ant.Eq(&Folders.Name, "docs")).
				IncludeOne(&Folders.Parent).
				Single(ctx)
			require.NoError(t, err)
			require.Equal(t, "root", found.Parent.Name)
		})

		t.Run("a NULL foreign key has no parent", func(t *testing.T) {
			found, err := db.Folders.Where(ant.Eq(&Folders.Name, "root")).
				IncludeOne(&Folders.Parent).
				Single(ctx)
			require.NoError(t, err)
			require.Nil(t, found.Parent)
		})

		t.Run("levels nest", func(t *testing.T) {
			found, err := db.Folders.Where(ant.Eq(&Folders.Name, "root")).
				Include(&Folders.Children, func(q *ant.Query[Folder]) *ant.Query[Folder] {
					return q.Include(&Folders.Children)
				}).Single(ctx)
			require.NoError(t, err)
			require.Equal(t, "2026", found.Children[0].Children[0].Name)
		})

		t.Run("Has refers to the related row, not the outer row", func(t *testing.T) {
			got, err := db.Folders.Where(ant.Has(&Folders.Children, ant.Eq(&Folders.Name, "2026"))).
				Single(ctx)
			require.NoError(t, err)
			require.Equal(t, "docs", got.Name)
		})

		t.Run("a self-join gives each side its own row", func(t *testing.T) {
			type pair struct{ Child, Parent string }

			got, err := db.Folders.Join(&Folders.Parent).
				Where(ant.Eq(&Folders.Name, "docs")).
				Select(func(child, parent *Folder) pair { return pair{child.Name, parent.Name} }).
				All(ctx)
			require.NoError(t, err)
			require.Equal(t, []pair{{"docs", "root"}}, got)
		})
	})
}
