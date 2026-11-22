package tests

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"

	"github.com/skudra-go/ant"
	"github.com/skudra-go/ant/internal/testdb"
)

// A projection reads NULL into a field whose type can't hold it as the zero
// value, as MaxOf does: a column of a left join's side for a row with no
// match, and an aggregate over no rows. A pointer or an sql.Null field, or
// ant.Nullable, keeps the NULL (#164).
func TestConformanceProjectionReadsNullAsZero(t *testing.T) {
	testdb.ForEach(t, func(t *testing.T, target testdb.Target) {
		app := openApp(t, target)
		ctx := t.Context()
		users := seedUsers(t, app, "alice", 30, "bob", 25)
		require.NoError(t, app.Posts.Insert(ctx, &Post{UserID: users[0].ID, Title: "hello", Published: true}))

		t.Run("a left join's side", func(t *testing.T) {
			type row struct {
				Name      string
				Title     string
				Published bool
				Posted    time.Time
				PostID    int64
				Bio       *string
			}

			R := ant.Fields[row]()
			got, err := ant.Project[row](
				app.Users.LeftJoinMany(&Users.Posts).OrderBy(&Users.Name),
				ant.Into(&R.Name, &Users.Name),
				ant.Into(&R.Title, &Posts.Title),
				ant.Into(&R.Published, &Posts.Published),
				ant.Into(&R.Posted, &Posts.CreatedAt),
				ant.Into(&R.PostID, &Posts.ID),
				ant.Into(&R.Bio, &Users.Bio),
			).
				All(ctx)
			require.NoError(t, err)
			require.Len(t, got, 2)
			require.Equal(t, "hello", got[0].Title)
			require.True(t, got[0].Published)
			require.Equal(t, row{Name: "bob"}, got[1], "bob has no post")
		})

		t.Run("grouped", func(t *testing.T) {
			type top struct {
				Name   string
				Newest int64
				Count  int64
			}

			T := ant.Fields[top]()
			got, err := ant.Project[top](
				app.Users.LeftJoinMany(&Users.Posts).GroupBy(&Users.Name),
				ant.Into(&T.Name, &Users.Name),
				ant.IntoExpr(&T.Newest, ant.Max(&Posts.ID)),
				ant.IntoExpr(&T.Count, ant.CountOf(&Posts.ID)),
			).
				All(ctx)
			require.NoError(t, err)
			slices.SortFunc(got, func(a, b top) int { return strings.Compare(a.Name, b.Name) })
			require.Len(t, got, 2)
			require.NotZero(t, got[0].Newest)
			require.Equal(t, top{Name: "bob"}, got[1], "Max over no posts is zero")
		})

		t.Run("no rows", func(t *testing.T) {
			type totals struct {
				Most  int64
				Sum   int
				Avg   float64
				First string
				When  time.Time
			}

			S := ant.Fields[totals]()
			got, err := ant.Project[totals](
				app.Users.Where(ant.Eq(&Users.ID, int64(-1))),
				ant.IntoExpr(&S.Most, ant.Max(&Users.ID)),
				ant.IntoExpr(&S.Sum, ant.Sum(&Users.Age)),
				ant.IntoExpr(&S.Avg, ant.Avg(&Users.Age)),
				ant.IntoExpr(&S.First, ant.Min(&Users.Name)),
				ant.IntoExpr(&S.When, ant.Max(&Users.CreatedAt)),
			).
				First(ctx)
			require.NoError(t, err)
			require.Equal(t, totals{}, *got)
		})

		t.Run("a type with a Scanner", func(t *testing.T) {
			db := openTickets(t, target)

			type money struct{ Most decimal.Decimal }

			M := ant.Fields[money]()
			got, err := ant.Project[money](
				db.Tickets.Query(),
				ant.IntoExpr(&M.Most, ant.Max(&Tickets.Amount)),
			).
				First(ctx)
			require.NoError(t, err)
			require.True(t, got.Most.IsZero())
		})

		t.Run("a raw read still refuses NULL", func(t *testing.T) {
			type plain struct {
				ID   int64
				Name string
			}

			_, err := ant.Raw[plain](app, "SELECT 1 AS id, NULL AS name").All(ctx)
			require.Error(t, err, "a NULL where the type has none is a mistake to report")
		})
	})
}
