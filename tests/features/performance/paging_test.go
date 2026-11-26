package performance

import (
	"database/sql"
	"encoding/base64"
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/skudra-go/ant"
	"github.com/skudra-go/ant/tests/features/fixture"
)

// Page returns a numbered page with the totals for paging links.
func TestPage(t *testing.T) {
	fixture.Run(t, func(t *testing.T, db *fixture.AppDB) {
		seed(t, db, 25)
		ctx := t.Context()
		q := db.Users.OrderBy(&Users.Name)

		t.Run("a middle page", func(t *testing.T) {
			page, err := q.Page(ctx, 2, 10)
			require.NoError(t, err)
			require.EqualValues(t, 25, page.Total)
			require.Equal(t, 3, page.Pages())
			require.True(t, page.HasNext())
			require.Equal(t, "u010", page.Items[0].Name)
		})

		t.Run("the last page", func(t *testing.T) {
			page, err := q.Page(ctx, 3, 10)
			require.NoError(t, err)
			require.Len(t, page.Items, 5)
			require.False(t, page.HasNext())
		})

		t.Run("past the end", func(t *testing.T) {
			page, err := q.Page(ctx, 9, 10)
			require.NoError(t, err)
			require.Empty(t, page.Items)
			require.EqualValues(t, 25, page.Total)
		})

		t.Run("an exact multiple", func(t *testing.T) {
			page, err := q.Page(ctx, 1, 25)
			require.NoError(t, err)
			require.Equal(t, 1, page.Pages())
		})

		t.Run("no rows", func(t *testing.T) {
			page, err := db.Users.Where(ant.Eq(&Users.Name, "none")).Page(ctx, 1, 10)
			require.NoError(t, err)
			require.Zero(t, page.Pages())
			require.False(t, page.HasNext())
		})

		// Total already counted within the query's Limit, and the items agree.
		t.Run("the query's own Limit and Offset are the rows to page", func(t *testing.T) {
			top := q.Limit(12).Offset(10)

			page, err := top.Page(ctx, 1, 5)
			require.NoError(t, err)
			require.EqualValues(t, 12, page.Total)
			require.Equal(t, 3, page.Pages())
			require.Equal(t, "u010", page.Items[0].Name)
			require.Len(t, page.Items, 5)

			page, err = top.Page(ctx, 3, 5)
			require.NoError(t, err)
			require.Equal(t, []string{"u020", "u021"}, fixture.Names(page.Items))
			require.False(t, page.HasNext())

			page, err = top.Page(ctx, 4, 5)
			require.NoError(t, err)
			require.NotNil(t, page.Items)
			require.Empty(t, page.Items)
			require.EqualValues(t, 12, page.Total)
		})

		t.Run("a bad page", func(t *testing.T) {
			// A page number from a request, such as ?page=0, is the client's
			// mistake, which ErrPaging tells from a database failure.
			for _, bad := range [][2]int{{0, 10}, {-1, 10}, {1, 0}, {1, -5}} {
				_, err := q.Page(ctx, bad[0], bad[1])
				require.ErrorIs(t, err, ant.ErrPaging, "page %d of size %d", bad[0], bad[1])
			}
		})
	})
}

// After pages by position in the ordering, so deep pages cost the same as the
// first, and rows added meanwhile do not shift them.
func TestAfter(t *testing.T) {
	fixture.Run(t, func(t *testing.T, db *fixture.AppDB) {
		seed(t, db, 25)
		ctx := t.Context()
		walk := func(t *testing.T, q *ant.Query[User], size int) []string {
			t.Helper()

			var names []string

			cursor := ""

			for {
				page, err := q.After(ctx, cursor, size)
				require.NoError(t, err)

				names = append(names, fixture.Names(page.Items)...)
				require.LessOrEqual(t, len(names), 100, "the pages never end")

				if page.Next == "" {
					return names
				}

				cursor = page.Next
			}
		}

		t.Run("visits every row once", func(t *testing.T) {
			names := walk(t, db.Users.OrderBy(&Users.Name), 10)
			require.Len(t, names, 25)
			require.Equal(t, "u000", names[0])
			require.Equal(t, "u024", names[24])
		})

		// The cursor is made of the ordering and the key, so both are read even
		// when Column or Omit left them out.
		for name, narrowed := range map[string]*ant.Query[User]{
			"Column without the key":      db.Users.Column(&Users.Name).OrderBy(&Users.Name),
			"Column without the ordering": db.Users.Column(&Users.Name).OrderBy(&Users.Email),
			"the key omitted":             db.Users.Omit(&Users.ID).OrderBy(&Users.Age),
			"the ordering omitted":        db.Users.OrderBy(&Users.Age).Omit(&Users.Age),
		} {
			t.Run(name, func(t *testing.T) {
				names := walk(t, narrowed, 4)
				require.Len(t, names, 25)

				slices.Sort(names)
				require.Len(t, slices.Compact(names), 25, "no row twice")
			})
		}

		t.Run("descending", func(t *testing.T) {
			names := walk(t, db.Users.OrderByDesc(&Users.Name), 7)
			require.Equal(t, "u024", names[0])
			require.Len(t, names, 25)
		})

		// Age has many ties. The key breaks them, so no row is lost at a boundary.
		t.Run("ties", func(t *testing.T) {
			names := walk(t, db.Users.OrderBy(&Users.Age), 4)
			require.Len(t, names, 25)

			seen := map[string]bool{}

			for _, n := range names {
				require.False(t, seen[n], "duplicate %s", n)
				seen[n] = true
			}
		})

		t.Run("two columns in opposite directions", func(t *testing.T) {
			require.Len(t, walk(t, db.Users.OrderBy(&Users.Age).ThenByDesc(&Users.Name), 3), 25)
		})

		t.Run("no ordering uses the key", func(t *testing.T) {
			require.Len(t, walk(t, db.Users.Where(), 10), 25)
		})

		t.Run("with a filter", func(t *testing.T) {
			require.Len(
				t,
				walk(t, db.Users.Where(ant.Eq(&Users.Age, 0)).OrderBy(&Users.Name), 2),
				9,
			)
		})

		t.Run("a row added behind the cursor shifts nothing", func(t *testing.T) {
			first, err := db.Users.OrderBy(&Users.Name).After(ctx, "", 10)
			require.NoError(t, err)
			require.NoError(t, db.Users.Insert(ctx, &User{Name: "a-early", Email: "a-early"}))
			second, err := db.Users.OrderBy(&Users.Name).After(ctx, first.Next, 10)
			require.NoError(t, err)
			require.Equal(t, "u010", second.Items[0].Name)
		})

		t.Run("an exact last page has no next cursor", func(t *testing.T) {
			page, err := db.Users.OrderBy(&Users.Name).After(ctx, "", 1000)
			require.NoError(t, err)
			require.Empty(t, page.Next)
		})

		t.Run("bad cursors", func(t *testing.T) {
			q := db.Users.OrderBy(&Users.Name)
			_, err := q.After(ctx, "%%%", 10)
			require.ErrorIs(t, err, ant.ErrCursor)
			_, err = q.After(ctx, base64.RawURLEncoding.EncodeToString([]byte(`{"a":1}`)), 10)
			require.ErrorIs(t, err, ant.ErrCursor)
			_, err = q.After(ctx, base64.RawURLEncoding.EncodeToString([]byte(`["a"]`)), 10)
			require.ErrorIs(t, err, ant.ErrCursor)
		})

		t.Run("a cursor needs plain columns", func(t *testing.T) {
			_, err := db.Users.Order(ant.Lower(&Users.Name).Asc()).After(ctx, "", 10)
			require.ErrorIs(t, err, ant.ErrCursor)
		})

		t.Run("a NULL in the ordering is refused, not skipped", func(t *testing.T) {
			_, err := db.Users.Order(ant.Col(&Users.Bio).Asc()).After(ctx, "", 1)
			require.ErrorIs(t, err, ant.ErrCursor)
		})

		// A cursor holds a place, not a count of the rows before it, so After
		// cannot stop at a limit that spans its pages.
		t.Run("Limit and Offset are refused", func(t *testing.T) {
			_, err := db.Users.OrderBy(&Users.Name).Limit(5).After(ctx, "", 2)
			require.ErrorIs(t, err, ant.ErrCursor)
			_, err = db.Users.OrderBy(&Users.Name).Offset(5).After(ctx, "", 2)
			require.ErrorIs(t, err, ant.ErrCursor)
		})

		t.Run("a bad size", func(t *testing.T) {
			_, err := db.Users.OrderBy(&Users.Name).After(ctx, "", 0)
			require.ErrorIs(t, err, ant.ErrPaging)
			require.NotErrorIs(t, err, ant.ErrCursor)
		})
	})
}

// A cursor compares columns, and no comparison matches NULL, so After refuses
// an ordering by a column that can hold NULL, before it reads anything, rather
// than end the pages where the NULL rows begin.
type Task struct {
	ID     int64
	Title  string
	DueAt  *time.Time
	DoneAt sql.Null[time.Time]
	Rank   *int
}

var Tasks = ant.Fields[Task]()

// NotNull says the column holds no NULL, whatever the Go type could hold.
func (Task) Configure(b *ant.ModelBuilder[Task]) {
	b.Column(&Tasks.Rank).NotNull()
}

type TasksDB struct {
	ant.Context
	Tasks ant.Table[Task]
}

const tasksDDL = `CREATE TABLE tasks (
	id BIGSERIAL PRIMARY KEY, title TEXT NOT NULL, due_at TIMESTAMPTZ, done_at TIMESTAMPTZ, rank INT NOT NULL)`

// A column that can hold NULL pages once the ordering says where its NULLs go,
// since PostgreSQL puts them last by default and the other databases first.
// Without NullsFirst or NullsLast, After returns ErrCursor.
func TestAfterOrdersByANullableColumn(t *testing.T) {
	fixture.RunWith(t, tasksDDL, func(t *testing.T, db *TasksDB) {
		ctx := t.Context()
		due := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

		// Five tasks with a due date, then five without.
		for i := range 10 {
			task := &Task{Title: fmt.Sprintf("t%d", i), Rank: &i}

			if i < 5 {
				at := due.AddDate(0, 0, i)
				task.DueAt, task.DoneAt = &at, sql.Null[time.Time]{V: at, Valid: true}
			}

			require.NoError(t, db.Tasks.Insert(ctx, task))
		}

		// titles reads every page of q, three rows at a time.
		titles := func(t *testing.T, q *ant.Query[Task]) []string {
			var out []string

			cursor := ""

			for range 5 {
				page, err := q.After(ctx, cursor, 3)
				require.NoError(t, err)

				for _, task := range page.Items {
					out = append(out, task.Title)
				}

				if cursor = page.Next; cursor == "" {
					break
				}
			}

			return out
		}

		t.Run("a pointer column, NULLs last", func(t *testing.T) {
			// Rows that tie, as the NULLs do, follow the key.
			q := db.Tasks.Order(ant.Col(&Tasks.DueAt).Asc().NullsLast())

			require.Equal(
				t,
				[]string{"t0", "t1", "t2", "t3", "t4", "t5", "t6", "t7", "t8", "t9"},
				titles(t, q),
			)
		})

		t.Run("a sql.Null column, newest first and NULLs first", func(t *testing.T) {
			q := db.Tasks.Order(ant.Col(&Tasks.DoneAt).Desc().NullsFirst())

			require.Equal(
				t,
				[]string{"t5", "t6", "t7", "t8", "t9", "t4", "t3", "t2", "t1", "t0"},
				titles(t, q),
			)
		})

		for name, q := range map[string]*ant.Query[Task]{
			"a pointer column":           db.Tasks.Order(ant.Col(&Tasks.DueAt).Asc()),
			"descending":                 db.Tasks.Order(ant.Col(&Tasks.DueAt).Desc()),
			"a sql.Null column":          db.Tasks.Order(ant.Col(&Tasks.DoneAt).Asc()),
			"after a column that is not": db.Tasks.Order(ant.Col(&Tasks.Title).Asc(), ant.Col(&Tasks.DueAt).Asc()),
		} {
			t.Run(name+" without a place for NULLs is refused", func(t *testing.T) {
				// The third row has a value, so only the column says it may not.
				_, err := q.After(ctx, "", 3)
				require.ErrorIs(t, err, ant.ErrCursor)
				require.ErrorContains(t, err, "can be NULL")
				require.ErrorContains(t, err, "NullsFirst or NullsLast")
			})
		}

		t.Run("a pointer column configured NotNull pages", func(t *testing.T) {
			q := db.Tasks.Order(ant.Col(&Tasks.Rank).Desc())

			require.Equal(
				t,
				[]string{"t9", "t8", "t7", "t6", "t5", "t4", "t3", "t2", "t1", "t0"},
				titles(t, q),
			)
		})
	})
}
