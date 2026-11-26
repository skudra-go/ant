package tests

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/skudra-go/ant"
	"github.com/skudra-go/ant/internal/testdb"
)

// After pages by a column that can hold NULL when the ordering places its NULLs
// with NullsFirst or NullsLast: each page seeks past the NULL rows as the order
// puts them, so every row comes once, in the order All gives. Without a place,
// which differs by database, it says to give one (#307).
func TestConformanceAfterOrdersByANullableColumn(t *testing.T) {
	testdb.ForEach(t, func(t *testing.T, target testdb.Target) {
		app := openApp(t, target)
		ctx := t.Context()

		bio := func(s string) *string { return &s }
		people := []struct {
			name string
			bio  *string
		}{
			{"ann", bio("b2")},
			{"ben", nil},
			{"cid", bio("b1")},
			{"dot", bio("b2")},
			{"eve", nil},
			{"fay", bio("b3")},
			{"gus", nil},
		}

		for i, p := range people {
			u := &User{
				Name:  p.name,
				Email: p.name + "@x",
				Age:   i,
				Bio:   p.bio,
			}
			require.NoError(t, app.Users.Insert(ctx, u))
		}

		bios := ant.Col(&Users.Bio)
		names := ant.Col(&Users.Name)

		everyone := app.Users.Query()
		withBio := app.Users.Where(ant.IsNotNull(&Users.Bio))
		noBio := app.Users.Where(ant.IsNull(&Users.Bio))

		cases := []struct {
			name  string
			q     *ant.Query[User]
			order []ant.Sort
			first []string // the order begins so, to show where the NULLs went
		}{
			{
				"asc, nulls first",
				everyone,
				[]ant.Sort{bios.Asc().NullsFirst()},
				[]string{"", "", "", "b1"},
			},
			{
				"asc, nulls last",
				everyone,
				[]ant.Sort{bios.Asc().NullsLast()},
				[]string{"b1", "b2", "b2", "b3", ""},
			},
			{
				"desc, nulls first",
				everyone,
				[]ant.Sort{bios.Desc().NullsFirst()},
				[]string{"", "", "", "b3"},
			},
			{
				"desc, nulls last",
				everyone,
				[]ant.Sort{bios.Desc().NullsLast()},
				[]string{"b3", "b2", "b2", "b1", ""},
			},
			{
				"then a name",
				everyone,
				[]ant.Sort{bios.Asc().NullsLast(), names.Desc()},
				[]string{"b1", "b2", "b2"},
			},
			{
				"after a name",
				everyone,
				[]ant.Sort{names.Asc(), bios.Asc().NullsFirst()},
				[]string{"b2", ""},
			},
			{
				"IsNotNull, newest first",
				withBio,
				[]ant.Sort{bios.Desc().NullsLast()},
				[]string{"b3", "b2", "b2", "b1"},
			},
			{"only NULLs", noBio, []ant.Sort{bios.Asc().NullsLast()}, []string{"", "", ""}},
		}

		for _, c := range cases {
			t.Run(c.name, func(t *testing.T) {
				// After ends the ordering with the key, so ties come by key.
				want, err := c.q.Order(append(c.order, ant.Col(&Users.ID).Asc())...).All(ctx)
				require.NoError(t, err)

				q := c.q.Order(c.order...)

				for _, size := range []int{1, 2, 3, 10} {
					var got []*User

					cursor := ""

					for range len(people) + 1 {
						page, err := q.After(ctx, cursor, size)
						require.NoError(t, err)

						got = append(got, page.Items...)

						if page.Next == "" {
							break
						}

						cursor = page.Next
					}

					require.Equal(t, idsOf(want), idsOf(got), "pages of %d", size)
				}

				var first []string

				for _, u := range want[:len(c.first)] {
					if u.Bio == nil {
						first = append(first, "")
					} else {
						first = append(first, *u.Bio)
					}
				}

				require.Equal(t, c.first, first)
			})
		}

		t.Run("without a place for the NULLs", func(t *testing.T) {
			for _, q := range []*ant.Query[User]{
				app.Users.Order(bios.Asc()),
				app.Users.OrderByDesc(&Users.Bio),
				app.Users.Where(ant.IsNotNull(&Users.Bio)).Order(bios.Desc()),
			} {
				_, err := q.After(ctx, "", 2)
				require.ErrorIs(t, err, ant.ErrCursor)
				require.ErrorContains(t, err, "Bio can be NULL")
				require.ErrorContains(t, err, "NullsFirst or NullsLast")
			}
		})
	})
}

func idsOf(users []*User) []int64 {
	out := make([]int64, len(users))

	for i, u := range users {
		out[i] = u.ID
	}

	return out
}
