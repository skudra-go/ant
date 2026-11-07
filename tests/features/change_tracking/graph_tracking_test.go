package change_tracking

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/skudra-go/ant"
	"github.com/skudra-go/ant/tests/features/fixture"
)

// TrackGraph saves a tree that came from outside, such as a request body. Nothing
// says what it held before, so each entity's state is decided here.
func TestTrackGraph(t *testing.T) {
	fixture.RunTracked(t, func(t *testing.T, db *AppDB) {
		ctx := t.Context()
		alice, _, _ := fixture.Seed(t, db)
		existing := &fixture.Post{UserID: alice.ID, Title: "old"}
		require.NoError(t, db.Posts.Insert(ctx, existing))
		require.NoError(t, db.ClearTracked(ctx))
		// A body with a changed user, a changed post, and a new post.
		incoming := &fixture.User{
			Name:  "Alice Updated",
			Email: alice.Email,
			Age:   31,
			Posts: []fixture.Post{
				{Title: "edited"},
				{Title: "brand new"},
			},
		}
		incoming.ID, incoming.CreatedAt = alice.ID, alice.CreatedAt
		incoming.Posts[0].ID, incoming.Posts[0].CreatedAt = existing.ID, existing.CreatedAt

		var visited []string

		err := ant.TrackGraph(ctx, db, incoming, func(n ant.GraphNode) ant.State {
			visited = append(visited, n.Model)

			if n.HasKey {
				return ant.Modified
			}

			return ant.Added
		})
		require.NoError(t, err)
		require.Equal(t, []string{"User", "Post", "Post"}, visited)
		require.NoError(t, db.SaveChanges(ctx))

		loaded, err := db.Users.NoTracking().
			Include(&U.Posts, func(q *ant.Query[fixture.Post]) *ant.Query[fixture.Post] {
				return q.OrderBy(&P.ID)
			}).
			Find(ctx, alice.ID)
		require.NoError(t, err)
		require.Equal(t, "Alice Updated", loaded.Name)
		require.Len(t, loaded.Posts, 2)
		require.Equal(t, "edited", loaded.Posts[0].Title)
		require.Equal(t, "brand new", loaded.Posts[1].Title)
		require.Equal(t, alice.ID, loaded.Posts[1].UserID, "the foreign key was filled in")

		t.Run("Detached skips an entity and everything below it", func(t *testing.T) {
			require.NoError(t, db.ClearTracked(ctx))

			skipped := &fixture.User{
				Name:  "Nobody",
				Email: "nobody@example.com",
				Posts: []fixture.Post{{Title: "x"}},
			}

			count := 0

			require.NoError(t, ant.TrackGraph(ctx, db, skipped, func(ant.GraphNode) ant.State {
				count++

				return ant.Detached
			}))

			require.Equal(t, 1, count)
			require.False(t, fixture.HasChanges(t, db))
		})

		t.Run("a nil root is an error", func(t *testing.T) {
			require.ErrorIs(t, ant.TrackGraph[fixture.User](ctx, db, nil, nil), ant.ErrNilEntity)
		})
	})
}

// Tagged is a model whose slice ignores order, with a comparer to say so.
type Tagged struct {
	ID    int64
	Name  string
	Items ant.JSON[[]string]
}

func (Tagged) Configure(b *ant.ModelBuilder[Tagged]) {
	b.Comparer(&TG.Items, func(a, b ant.JSON[[]string]) bool {
		if len(a.V) != len(b.V) {
			return false
		}

		seen := map[string]int{}

		for _, item := range a.V {
			seen[item]++
		}

		for _, item := range b.V {
			seen[item]--

			if seen[item] < 0 {
				return false
			}
		}

		return true
	})
}

// TG is Tagged's probe.
var TG = ant.Fields[Tagged]()

// TaggedDB holds Tagged.
type TaggedDB struct {
	ant.Context
	Rows ant.Table[Tagged]
}

// A comparer decides what counts as a change, for values that deep equality gets
// wrong, such as a list whose order means nothing.
func TestComparer(t *testing.T) {
	const ddl = `CREATE TABLE taggeds (id BIGSERIAL PRIMARY KEY, name TEXT NOT NULL, items JSONB NOT NULL)`
	fixture.RunTrackedWith(t, ddl, func(t *testing.T, db *TaggedDB) {
		ctx := t.Context()
		row := &Tagged{Name: "a", Items: ant.JSON[[]string]{V: []string{"x", "y"}}}
		require.NoError(t, db.Rows.Insert(ctx, row))

		row.Items.V = []string{"y", "x"}

		require.False(t, fixture.HasChanges(t, db), "the same items in another order")

		row.Items.V = []string{"y", "z"}

		require.True(t, fixture.HasChanges(t, db))
		require.NoError(t, db.SaveChanges(ctx))
	})
}

// New rows of one model share one INSERT, so saving a thousand costs one round
// trip per two hundred, and generated keys still land on the right structs.
func TestInsertsAreBatched(t *testing.T) {
	fixture.Each(t, func(t *testing.T, target fixture.Target) {
		ctx := t.Context()
		rec := &counter{}
		db := fixture.Open[AppDB](t, target, ant.WithTracking(true), ant.WithInterceptor(rec))
		user := &fixture.User{Name: "Many", Email: "many@example.com"}

		for i := range 450 {
			user.Posts = append(user.Posts, fixture.Post{Title: fmt.Sprint("post ", i)})
		}

		require.NoError(t, db.Users.Insert(ctx, user))
		require.Equal(t, 4, rec.inserts(), "one user, and three batches of posts")

		rows, err := db.Posts.NoTracking().OrderBy(&P.ID).All(ctx)
		require.NoError(t, err)
		require.Len(t, rows, 450)

		for i, row := range rows {
			require.Equal(t, fmt.Sprint("post ", i), row.Title)
			require.Equal(t, row.ID, user.Posts[i].ID, "keys match rows")
			require.Equal(t, user.ID, row.UserID)
		}
	})
}

// A failure in a batch leaves nothing behind, and keys are cleared, as ever.
func TestFailedBatch(t *testing.T) {
	fixture.RunTracked(t, func(t *testing.T, db *AppDB) {
		ctx := t.Context()
		first := &fixture.User{Name: "A", Email: "same@example.com"}
		second := &fixture.User{Name: "B", Email: "same@example.com"}
		err := db.Users.Insert(ctx, first, second)
		require.True(t, ant.IsUniqueViolation(err))
		require.Zero(t, first.ID)

		count, err := db.Users.Count(ctx)
		require.NoError(t, err)
		require.Zero(t, count)
	})
}

type counter struct {
	mu     sync.Mutex
	writes int
}

func (c *counter) Before(ctx context.Context, s *ant.Statement) (context.Context, error) {
	if strings.HasPrefix(s.SQL, "INSERT") {
		c.mu.Lock()
		c.writes++
		c.mu.Unlock()
	}

	return ctx, nil
}

func (c *counter) After(context.Context, *ant.Statement, ant.Outcome) {}

func (c *counter) inserts() int {
	c.mu.Lock()
	defer c.mu.Unlock()

	return c.writes
}
