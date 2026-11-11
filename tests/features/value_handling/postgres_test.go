package value_handling

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/skudra-go/ant"
	"github.com/skudra-go/ant/dialect/postgres"
	"github.com/skudra-go/ant/tests/features/fixture"
)

// postgres.Array stores a slice in a native array column.
type Item struct {
	ID     int64
	Title  string
	Labels postgres.Array[string]
	Scores postgres.Array[int]
	Flags  postgres.Array[bool]
}

type ItemsDB struct {
	ant.Context
	Items ant.Table[Item]
}

var Items = ant.Fields[Item]()

func TestArrays(t *testing.T) {
	ddl := `CREATE TABLE items (id BIGSERIAL PRIMARY KEY, title TEXT NOT NULL, labels TEXT[], scores INT[], flags BOOL[])`
	fixture.RunTrackedWith(t, ddl, func(t *testing.T, db *ItemsDB) {
		ctx := t.Context()
		stored := func(id int64) *Item {
			got, err := db.Items.NoTracking().Find(ctx, id)
			require.NoError(t, err)

			return got
		}

		t.Run("values round trip", func(t *testing.T) {
			item := &Item{
				Title:  "a",
				Labels: []string{"x", "y"},
				Scores: []int{1, 2},
				Flags:  []bool{true, false},
			}
			require.NoError(t, db.Items.Insert(ctx, item))
			got := stored(item.ID)
			require.Equal(t, item.Labels, got.Labels)
			require.Equal(t, item.Scores, got.Scores)
			require.Equal(t, item.Flags, got.Flags)
		})

		t.Run("text that looks like syntax", func(t *testing.T) {
			hard := []string{"a,b", `quo"te`, `back\slash`, "{brace}", "", " space ", "NULL"}
			item := &Item{Title: "b", Labels: hard}
			require.NoError(t, db.Items.Insert(ctx, item))
			require.Equal(t, postgres.Array[string](hard), stored(item.ID).Labels)
		})

		t.Run("nil is NULL, and empty is empty", func(t *testing.T) {
			item := &Item{Title: "c", Scores: []int{}}
			require.NoError(t, db.Items.Insert(ctx, item))
			got := stored(item.ID)
			require.Nil(t, got.Labels)
			require.NotNil(t, got.Scores)
			require.Empty(t, got.Scores)
		})

		t.Run("an edit in place is tracked", func(t *testing.T) {
			item := &Item{Title: "d", Scores: []int{1}}
			require.NoError(t, db.Items.Insert(ctx, item))
			item.Scores[0] = 2

			require.NoError(t, db.SaveChanges(ctx))
			require.Equal(t, postgres.Array[int]{2}, stored(item.ID).Scores)
		})
	})
}

// Match runs a full-text search.
func TestFullText(t *testing.T) {
	fixture.Run(t, func(t *testing.T, db *fixture.AppDB) {
		ctx := t.Context()
		alice, _, _ := fixture.Seed(t, db)
		require.NoError(t, db.Posts.Insert(ctx,
			&fixture.Post{UserID: alice.ID, Title: "The quick brown fox"},
			&fixture.Post{UserID: alice.ID, Title: "Lazy dogs sleeping"}))

		search := func(query string) []string {
			got, err := ant.Pluck(
				ctx,
				db.Posts.Where(postgres.Match(&fixture.Posts.Title, "english", query)),
				&fixture.Posts.Title,
			)
			require.NoError(t, err)

			return got
		}

		require.Equal(t, []string{"The quick brown fox"}, search("quick fox"))
		require.Equal(t, []string{"Lazy dogs sleeping"}, search("dog"), "stemming applies")
		require.Empty(t, search("quick -fox"))
		require.Empty(t, search("unrelated"))
	})
}
