package tests

import (
	"database/sql"
	"testing"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"

	"github.com/skudra-go/ant"
	"github.com/skudra-go/ant/internal/testdb"
)

// Ne, NotIn and Not of Eq or In match NULL on a column that can hold one, as
// Go's != does, where SQL's <> leaves it out. Eq(nil) is IS NULL, so a filter
// "not Ann" is read as Go, and the rows with no nick are in it (#293).
func TestConformanceNotEqualMatchesNull(t *testing.T) {
	testdb.ForEach(t, func(t *testing.T, target testdb.Target) {
		db := openTickets(t, target)
		ctx := t.Context()

		ann, bob, lowerAnn := "Ann", "bob", "ann"
		one := sql.Null[float64]{V: 1, Valid: true}

		require.NoError(t, db.Tickets.Insert(ctx,
			&Ticket{ID: 1, Nick: &ann, Score: sql.Null[float64]{V: 2, Valid: true}, Amount: decimal.NewFromInt(5)},
			&Ticket{ID: 2, Nick: &bob, Score: one, Amount: decimal.NewFromInt(5)},
			&Ticket{ID: 3, Amount: decimal.NewFromInt(5)},
		))

		cases := []struct {
			name string
			cond ant.Cond
			want []int64
		}{
			{"Ne", ant.Ne(&Tickets.Nick, &ann), []int64{2, 3}},
			{"Ne on sql.Null", ant.Ne(&Tickets.Score, one), []int64{1, 3}},
			{"Ne nil is IS NOT NULL", ant.Ne(&Tickets.Nick, nil), []int64{1, 2}},
			{"NotIn", ant.NotIn(&Tickets.Nick, &ann), []int64{2, 3}},
			{"NotIn of two", ant.NotIn(&Tickets.Nick, &ann, &bob), []int64{3}},
			{"NotIn with nil leaves NULL out", ant.NotIn(&Tickets.Nick, nil, &ann), []int64{2}},
			{"NotIn of none", ant.NotIn(&Tickets.Nick), []int64{1, 2, 3}},
			{"Not Eq", ant.Not(ant.Eq(&Tickets.Nick, &ann)), []int64{2, 3}},
			{"Not In", ant.Not(ant.In(&Tickets.Nick, &ann)), []int64{2, 3}},
			{"Not In with nil", ant.Not(ant.In(&Tickets.Nick, nil, &ann)), []int64{2}},
			{"Not Ne", ant.Not(ant.Ne(&Tickets.Nick, &ann)), []int64{1}},
			{"Not NotIn", ant.Not(ant.NotIn(&Tickets.Nick, &ann)), []int64{1}},
			{"Not Not Eq", ant.Not(ant.Not(ant.Eq(&Tickets.Nick, &ann))), []int64{1}},
			{"NoneOf", ant.NoneOf(&ann, &Tickets.Nick), []int64{2, 3}},
			{
				"Not Or, by De Morgan",
				ant.Not(ant.Or(ant.Eq(&Tickets.Nick, &ann), ant.Eq(&Tickets.Score, one))),
				[]int64{3},
			},
			{
				"Not And, by De Morgan",
				ant.Not(ant.And(ant.Eq(&Tickets.Nick, &bob), ant.Gt(&Tickets.ID, int64(0)))),
				[]int64{1, 3},
			},
			{"Col Ne", ant.Col(&Tickets.Nick).Ne(&ann), []int64{2, 3}},
			{"Not Col Eq", ant.Not(ant.Col(&Tickets.Nick).Eq(&ann)), []int64{2, 3}},
			{"Lower Ne", ant.Lower(&Tickets.Nick).Ne(&lowerAnn), []int64{2, 3}},
			{"Gt follows SQL", ant.Not(ant.Gt(&Tickets.Score, one)), []int64{2}},
		}

		for _, c := range cases {
			t.Run(c.name, func(t *testing.T) {
				got, err := db.Tickets.Where(c.cond).OrderBy(&Tickets.ID).All(ctx)
				require.NoError(t, err)
				require.Equal(t, c.want, rowIDs(got, ticketID))
			})
		}

		t.Run("a column that can't be NULL keeps <>", func(t *testing.T) {
			query, _, err := db.Tickets.Where(
				ant.Ne(&Tickets.ID, int64(1)),
				ant.NotIn(&Tickets.Amount, decimal.NewFromInt(1)),
				ant.Not(ant.Eq(&Tickets.Ref, Tickets.Ref)),
			).
				ToSQL()
			require.NoError(t, err)
			require.NotContains(t, query, " OR ", "the soft-delete filter is the only IS NULL")
		})

		t.Run("a bulk write", func(t *testing.T) {
			n, err := db.Tickets.Where(ant.Ne(&Tickets.Nick, &ann)).
				Update(ctx, ant.Set(&Tickets.Amount, decimal.NewFromInt(7)))
			require.NoError(t, err)
			require.EqualValues(t, 2, n, "the row with no nick is not Ann")
		})
	})
}
