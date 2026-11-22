package tests

import (
	"testing"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"

	"github.com/skudra-go/ant"
	"github.com/skudra-go/ant/internal/testdb"
)

// With adds a value to a whole row, so a session tracks the rows it reads: an
// edit is saved, and the computed field is neither compared nor written. An
// instance the session already holds gets the new value and keeps its edits
// (#295).
func TestConformanceWithKeepsRowsTracked(t *testing.T) {
	testdb.ForEach(t, func(t *testing.T, target testdb.Target) {
		root := openTickets(t, target)
		ctx := t.Context()

		require.NoError(t, root.Tickets.Insert(ctx, &Ticket{
			ID:     1,
			Ref:    uuid.New(),
			Amount: decimal.NewFromInt(7),
		}))

		for i, cost := range []int64{2, 3} {
			require.NoError(t, root.TicketNotes.Insert(ctx, &TicketNote{
				ID:       int64(10 + i),
				TicketID: 1,
				Cost:     decimal.NewFromInt(cost),
			}))
		}

		total := ant.SumRelated(&Tickets.Notes, &TicketNotes.Cost)

		nickOf := func(t *testing.T) string {
			t.Helper()

			got, err := root.Tickets.Find(ctx, int64(1))
			require.NoError(t, err)

			if got.Nick == nil {
				return ""
			}

			return *got.Nick
		}

		t.Run("an edit is saved", func(t *testing.T) {
			s := ant.Session(root)
			got, err := s.Tickets.With(&Tickets.NoteTotal, total).Find(ctx, int64(1))
			require.NoError(t, err)
			require.True(t, got.NoteTotal.Equal(decimal.NewFromInt(5)), got.NoteTotal.String())

			nick := "renamed"
			got.Nick = &nick

			changed, err := s.HasChanges(ctx)
			require.NoError(t, err)
			require.True(t, changed)
			require.NoError(t, s.SaveChanges(ctx))
			require.Equal(t, "renamed", nickOf(t))
		})

		t.Run("the computed field is not compared", func(t *testing.T) {
			s := ant.Session(root)
			got, err := s.Tickets.With(&Tickets.NoteTotal, total).Find(ctx, int64(1))
			require.NoError(t, err)

			got.NoteTotal = decimal.NewFromInt(99)

			changed, err := s.HasChanges(ctx)
			require.NoError(t, err)
			require.False(t, changed)
			require.NoError(t, s.SaveChanges(ctx))
		})

		t.Run("an instance the session holds gets the value", func(t *testing.T) {
			s := ant.Session(root)
			first, err := s.Tickets.Find(ctx, int64(1))
			require.NoError(t, err)
			require.True(t, first.NoteTotal.IsZero())

			nick := "held"
			first.Nick = &nick

			again, err := s.Tickets.With(&Tickets.NoteTotal, total).Find(ctx, int64(1))
			require.NoError(t, err)
			require.Same(t, first, again)
			require.True(t, again.NoteTotal.Equal(decimal.NewFromInt(5)), again.NoteTotal.String())
			require.Equal(t, "held", *again.Nick, "the unsaved edit stays")

			all, err := s.Tickets.With(&Tickets.NoteTotal, total).All(ctx)
			require.NoError(t, err)
			require.Len(t, all, 1)
			require.Same(t, first, all[0])
		})

		t.Run("in a joined read", func(t *testing.T) {
			require.NoError(t, root.Tickets.Insert(ctx, &Ticket{
				ID:     2,
				Ref:    uuid.New(),
				Amount: decimal.NewFromInt(1),
			}))
			require.NoError(t, root.TicketNotes.Insert(ctx, &TicketNote{
				ID:       20,
				TicketID: 2,
				Cost:     decimal.NewFromInt(4),
			}))

			s := ant.Session(root)
			first, err := s.Tickets.Find(ctx, int64(2))
			require.NoError(t, err)

			got, err := s.Tickets.Where(ant.Eq(&Tickets.ID, int64(2))).
				IncludeJoined(&Tickets.LastNote).
				With(&Tickets.NoteTotal, total).
				All(ctx)
			require.NoError(t, err)
			require.Len(t, got, 1)
			require.Same(t, first, got[0])
			require.NotNil(t, first.LastNote)
			require.True(t, first.NoteTotal.Equal(decimal.NewFromInt(4)), first.NoteTotal.String())

			nick := "joined"
			first.Nick = &nick
			require.NoError(t, s.SaveChanges(ctx))

			again, err := root.Tickets.Find(ctx, int64(2))
			require.NoError(t, err)
			require.Equal(t, "joined", *again.Nick)
		})
	})
}
