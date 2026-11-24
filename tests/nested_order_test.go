package tests

import (
	"slices"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/skudra-go/ant"
	"github.com/skudra-go/ant/internal/testdb"
)

// jrSeed opens the Jr models on target, with two essays and four notes.
func jrSeed(t *testing.T, target testdb.Target) *JrDB {
	t.Helper()

	ctx := t.Context()

	jrTables(t, target)

	db, err := ant.Open[JrDB](ctx, target.Dialect, target.DSN, ant.WithTracking(false))
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })

	one := int64(1)

	require.NoError(
		t,
		db.JrEssays.Insert(ctx, &JrEssay{ID: 1, Title: "e1"}, &JrEssay{ID: 2, Title: "e2"}),
	)

	require.NoError(t, db.JrNotes.Insert(
		ctx,
		&JrNote{
			ID:        1,
			OwnerType: "essay",
			OwnerID:   1,
			Weight:    1,
		},
		&JrNote{
			ID:         3,
			OwnerType:  "essay",
			OwnerID:    1,
			Weight:     3,
			ParentType: "note",
			ParentID:   &one,
		},
		&JrNote{
			ID:         4,
			OwnerType:  "essay",
			OwnerID:    2,
			Weight:     7,
			ParentType: "essay",
			ParentID:   &one,
		},
		&JrNote{
			ID:        5,
			OwnerType: "video",
			OwnerID:   1,
			Weight:    9,
		},
	))

	return db
}

func jrIDs(notes []*JrNote) []int64 {
	ids := make([]int64, 0, len(notes))

	for _, n := range notes {
		ids = append(ids, n.ID)
	}

	return ids
}

// An ordered query nested as a subquery, derived table or CTE runs on every
// database: SQL Server refuses an ORDER BY there without OFFSET (error 1033), and
// it cannot change which rows the nested query yields, so it is left out. With
// paging it stays, since it picks the rows (#93).
func TestConformanceNestedOrder(t *testing.T) {
	testdb.ForEach(t, func(t *testing.T, target testdb.Target) {
		ctx := t.Context()
		db := jrSeed(t, target)
		byWeight := db.JrNotes.Where(ant.Eq(&JrNotes.OwnerType, "essay")).OrderBy(&JrNotes.Weight)

		t.Run("exists", func(t *testing.T) {
			essays, err := db.JrEssays.
				Where(
					ant.Exists(
						byWeight.Where(
							ant.EqCol(&JrNotes.OwnerID, &JrEssays.ID),
							ant.Gt(&JrNotes.Weight, int64(5)),
						),
					),
				).
				All(ctx)
			require.NoError(t, err)
			require.Len(t, essays, 1)
			require.Equal(t, int64(2), essays[0].ID)
		})

		t.Run("in a subquery", func(t *testing.T) {
			essays, err := db.JrEssays.
				Where(
					ant.InSub(
						&JrEssays.ID,
						ant.SubOf(
							byWeight.Where(ant.Gt(&JrNotes.Weight, int64(5))),
							&JrNotes.OwnerID,
						),
					),
				).
				All(ctx)
			require.NoError(t, err)
			require.Len(t, essays, 1)
			require.Equal(t, int64(2), essays[0].ID)
		})

		t.Run("as a value", func(t *testing.T) {
			sub := ant.SubOf(byWeight.Where(ant.Eq(&JrNotes.ID, int64(4))), &JrNotes.OwnerID)
			essays, err := db.JrEssays.Where(ant.Col(&JrEssays.ID).EqExpr(sub.Expr())).All(ctx)
			require.NoError(t, err)
			require.Len(t, essays, 1)
			require.Equal(t, int64(2), essays[0].ID)
		})

		t.Run("from", func(t *testing.T) {
			notes, err := db.JrNotes.Query().From(byWeight).OrderBy(&JrNotes.ID).All(ctx)
			require.NoError(t, err)

			ids := jrIDs(notes)
			require.Equal(t, []int64{1, 3, 4}, ids)
		})

		t.Run("from a CTE", func(t *testing.T) {
			notes, err := db.JrNotes.Query().
				FromCTE("by_weight", byWeight).
				OrderBy(&JrNotes.ID).
				All(ctx)
			require.NoError(t, err)

			ids := jrIDs(notes)
			require.Equal(t, []int64{1, 3, 4}, ids)
		})

		t.Run("joined", func(t *testing.T) {
			ids, err := ant.JoinSub(db.JrEssays.Query(), byWeight, ant.EqCol(&JrEssays.ID, &JrNotes.OwnerID)).
				Select(func(_ *JrEssay, n *JrNote) int64 { return n.ID }).
				All(ctx)
			require.NoError(t, err)
			slices.Sort(ids)
			require.Equal(t, []int64{1, 3, 4}, ids)
		})

		t.Run("paged keeps its order", func(t *testing.T) {
			heaviest := db.JrNotes.OrderByDesc(&JrNotes.Weight).Limit(2)
			notes, err := db.JrNotes.Query().From(heaviest).OrderBy(&JrNotes.ID).All(ctx)
			require.NoError(t, err)

			ids := jrIDs(notes)
			require.Equal(t, []int64{4, 5}, ids, "the two heaviest notes")
		})
	})
}

// DISTINCT with paging and no order, or with NULLS FIRST and NULLS LAST, runs on
// SQL Server, which refuses an ORDER BY item outside a DISTINCT select list
// (error 145) (#95).
func TestConformanceDistinctOrder(t *testing.T) {
	testdb.ForEach(t, func(t *testing.T, target testdb.Target) {
		ctx := t.Context()
		db := jrSeed(t, target)

		t.Run("paged with no order", func(t *testing.T) {
			notes, err := db.JrNotes.Distinct().Column(&JrNotes.OwnerType).Limit(5).All(ctx)
			require.NoError(t, err)

			types := make([]string, 0, len(notes))

			for _, n := range notes {
				types = append(types, n.OwnerType)
			}

			require.ElementsMatch(t, []string{"essay", "video"}, types)
		})

		parents := func(t *testing.T, q *ant.Query[JrNote]) []*int64 {
			t.Helper()

			notes, err := q.All(ctx)
			require.NoError(t, err)

			out := make([]*int64, 0, len(notes))

			for _, n := range notes {
				out = append(out, n.ParentID)
			}

			return out
		}

		one := int64(1)
		distinct := db.JrNotes.Distinct().Column(&JrNotes.ParentID)
		parent := ant.Col(&JrNotes.ParentID)

		t.Run("NULLs where the database puts them", func(t *testing.T) {
			require.Equal(
				t,
				[]*int64{nil, &one},
				parents(t, distinct.Order(parent.Asc().NullsFirst())),
			)

			require.Equal(
				t,
				[]*int64{&one, nil},
				parents(t, distinct.Order(parent.Desc().NullsLast())),
			)

			require.Equal(
				t,
				[]*int64{nil, &one},
				parents(t, distinct.Order(parent.Asc().NullsFirst()).Limit(5)),
			)
		})

		t.Run("NULLs moved", func(t *testing.T) {
			if target.Name == "sqlserver" {
				for _, q := range []*ant.Query[JrNote]{
					distinct.Order(parent.Asc().NullsLast()),
					distinct.Order(parent.Desc().NullsFirst()),
				} {
					_, err := q.All(ctx)
					require.ErrorIs(t, err, ant.ErrUnsupported)
					require.ErrorContains(t, err, "SELECT DISTINCT")
				}

				return
			}

			require.Equal(
				t,
				[]*int64{&one, nil},
				parents(t, distinct.Order(parent.Asc().NullsLast())),
			)

			require.Equal(
				t,
				[]*int64{nil, &one},
				parents(t, distinct.Order(parent.Desc().NullsFirst())),
			)
		})

		t.Run("NULLs moved without DISTINCT", func(t *testing.T) {
			q := db.JrNotes.Column(&JrNotes.ParentID).
				Order(parent.Asc().NullsLast(), ant.Col(&JrNotes.ID).Asc())
			require.Equal(t, []*int64{&one, &one, nil, nil}, parents(t, q))

			q = db.JrNotes.Column(&JrNotes.ParentID).
				Order(parent.Desc().NullsFirst(), ant.Col(&JrNotes.ID).Asc())
			require.Equal(t, []*int64{nil, nil, &one, &one}, parents(t, q))
		})
	})
}
