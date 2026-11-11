package loading

import (
	"database/sql"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/skudra-go/ant"
	"github.com/skudra-go/ant/tests/features/fixture"
)

// A computed field is never written, and is filled by Query.With.
type Writer struct {
	ID       int64
	Name     string
	Articles []Article

	ArticleCount int64             `ant:"computed"`
	LiveCount    int64             `ant:"computed"`
	TotalViews   int               `ant:"computed"`
	AverageViews sql.Null[float64] `ant:"computed"`
	MostViews    sql.Null[int]     `ant:"computed"`
	FewestViews  sql.Null[int]     `ant:"computed"`
}

type Article struct {
	ID       int64
	WriterID int64
	Views    int
	Live     bool
}

type PressDB struct {
	ant.Context
	Writers  ant.Table[Writer]
	Articles ant.Table[Article]
}

var (
	Writers  = ant.Fields[Writer]()
	Articles = ant.Fields[Article]()
)

func TestRelatedAggregates(t *testing.T) {
	ddl := `
		CREATE TABLE writers (id BIGSERIAL PRIMARY KEY, name TEXT NOT NULL);
		CREATE TABLE articles (id BIGSERIAL PRIMARY KEY, writer_id BIGINT NOT NULL REFERENCES writers, views INT NOT NULL, live BOOL NOT NULL)`
	fixture.RunWith(t, ddl, func(t *testing.T, db *PressDB) {
		ctx := t.Context()
		ann, ben := &Writer{Name: "Ann"}, &Writer{Name: "Ben"}
		require.NoError(t, db.Writers.Insert(ctx, ann, ben))

		require.NoError(t, db.Articles.Insert(ctx,
			&Article{WriterID: ann.ID, Views: 10, Live: true},
			&Article{WriterID: ann.ID, Views: 30}))

		query := db.Writers.OrderBy(&Writers.Name)

		t.Run("count", func(t *testing.T) {
			got, err := query.With(&Writers.ArticleCount, ant.CountRelated(&Writers.Articles)).
				All(ctx)
			require.NoError(t, err)
			require.EqualValues(t, 2, got[0].ArticleCount)
			require.Zero(t, got[1].ArticleCount)
		})

		t.Run("count with a condition", func(t *testing.T) {
			got, err := query.With(
				&Writers.LiveCount,
				ant.CountRelated(&Writers.Articles, ant.Eq(&Articles.Live, true)),
			).
				All(ctx)
			require.NoError(t, err)
			require.EqualValues(t, 1, got[0].LiveCount)
		})

		t.Run("sum is zero with no rows", func(t *testing.T) {
			got, err := query.With(&Writers.TotalViews, ant.SumRelated(&Writers.Articles, &Articles.Views)).
				All(ctx)
			require.NoError(t, err)
			require.Equal(t, 40, got[0].TotalViews)
			require.Zero(t, got[1].TotalViews)
		})

		t.Run("min, max, and avg are NULL with no rows", func(t *testing.T) {
			got, err := query.
				With(&Writers.AverageViews, ant.AvgRelated(&Writers.Articles, &Articles.Views)).
				With(&Writers.MostViews, ant.MaxRelated(&Writers.Articles, &Articles.Views)).
				With(&Writers.FewestViews, ant.MinRelated(&Writers.Articles, &Articles.Views)).
				All(ctx)
			require.NoError(t, err)
			require.EqualValues(t, 20, got[0].AverageViews.V)
			require.Equal(t, 30, got[0].MostViews.V)
			require.Equal(t, 10, got[0].FewestViews.V)
			require.False(t, got[1].MostViews.Valid)
		})

		t.Run("a computed field is empty unless asked for", func(t *testing.T) {
			got, err := query.All(ctx)
			require.NoError(t, err)
			require.Zero(t, got[0].ArticleCount)
		})

		t.Run("a computed field is never written", func(t *testing.T) {
			ann.ArticleCount = 99
			require.NoError(t, db.Writers.Insert(ctx, &Writer{Name: "Cal", ArticleCount: 5}))
		})
	})
}
