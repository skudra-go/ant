package performance

import (
	"encoding/base64"
	"testing"

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

		t.Run("a bad page", func(t *testing.T) {
			_, err := q.Page(ctx, 0, 10)
			require.Error(t, err)
			_, err = q.Page(ctx, 1, 0)
			require.Error(t, err)
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

		t.Run("a bad size", func(t *testing.T) {
			_, err := db.Users.OrderBy(&Users.Name).After(ctx, "", 0)
			require.Error(t, err)
		})
	})
}
