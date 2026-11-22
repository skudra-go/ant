package tests

import (
	"context"
	"database/sql"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"

	"github.com/skudra-go/ant"
	"github.com/skudra-go/ant/dialect"
	"github.com/skudra-go/ant/internal/testdb"
)

// Ticket has a column of each shape the typed helpers once refused: a nullable
// time, text and integer, a uuid, a decimal, an sql.Null and a soft delete
// (#171, #174).
type Ticket struct {
	ID        int64
	DueAt     *time.Time
	Nick      *string
	Votes     *int
	Ref       uuid.UUID
	Amount    decimal.Decimal
	Score     sql.Null[float64]
	DeletedAt ant.DeletedAt
	Notes     []TicketNote
	LastNote  *TicketNote

	NoteTotal decimal.Decimal               `ant:"computed"`
	NoteMost  sql.Null[decimal.Decimal]     `ant:"computed"`
	NoteAvg   sql.Null[float64]             `ant:"computed"`
	NoteLast  sql.Null[*time.Time]          `ant:"computed"`
	NoteFirst sql.Null[sql.Null[time.Time]] `ant:"computed"`
}

func (Ticket) Configure(b *ant.ModelBuilder[Ticket]) {
	b.Table("tickets")
	b.HasMany(&Tickets.Notes).ForeignKey(&TicketNotes.TicketID)
	b.HasOne(&Tickets.LastNote).ForeignKey(&TicketNotes.TicketID).Latest(&TicketNotes.At)
}

// TicketNote belongs to a ticket, at a time that may be unknown.
type TicketNote struct {
	ID       int64
	TicketID int64
	At       *time.Time
	Seen     sql.Null[time.Time]
	Cost     decimal.Decimal
}

func (TicketNote) Configure(b *ant.ModelBuilder[TicketNote]) { b.Table("ticket_notes") }

// TicketDB holds tickets and their notes.
type TicketDB struct {
	ant.Context
	Tickets     ant.Table[Ticket]
	TicketNotes ant.Table[TicketNote]
}

var (
	Tickets     = ant.Fields[Ticket]()
	TicketNotes = ant.Fields[TicketNote]()
)

var (
	uuids = map[string]string{
		"postgres":  "UUID",
		"mysql":     "CHAR(36)",
		"mariadb":   "CHAR(36)",
		"sqlite":    "CHAR(36)",
		"sqlserver": "CHAR(36)",
	}
	doubles = map[string]string{
		"postgres":  "DOUBLE PRECISION",
		"mysql":     "DOUBLE",
		"mariadb":   "DOUBLE",
		"sqlite":    "REAL",
		"sqlserver": "FLOAT",
	}
)

func openTickets(t *testing.T, target testdb.Target) *TicketDB {
	t.Helper()
	ctx := t.Context()

	for _, ddl := range []string{
		fmt.Sprintf(`CREATE TABLE tickets (
			id BIGINT PRIMARY KEY,
			due_at %[1]s NULL,
			nick VARCHAR(40) NULL,
			votes INT NULL,
			ref %[2]s NOT NULL,
			amount NUMERIC(12,2) NOT NULL,
			score %[3]s NULL,
			deleted_at %[1]s NULL)`,
			timestamps[target.Name],
			uuids[target.Name],
			doubles[target.Name],
		),
		fmt.Sprintf(`CREATE TABLE ticket_notes (
			id BIGINT PRIMARY KEY,
			ticket_id BIGINT NOT NULL,
			at %[1]s NULL,
			seen %[1]s NULL,
			cost NUMERIC(12,2) NOT NULL)`,
			timestamps[target.Name],
		),
	} {
		_, err := target.DB.ExecContext(ctx, ddl)
		require.NoError(t, err)
	}

	t.Cleanup(func() {
		_, _ = target.DB.ExecContext(context.Background(), "DROP TABLE ticket_notes")
		_, _ = target.DB.ExecContext(context.Background(), "DROP TABLE tickets")
	})

	db, err := ant.Open[TicketDB](ctx, target.Dialect, target.DSN)
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })

	return db
}

func rowIDs[S ~[]E, E any](rows S, id func(E) int64) []int64 {
	out := make([]int64, 0, len(rows))

	for _, r := range rows {
		out = append(out, id(r))
	}

	return out
}

func ticketID(t *Ticket) int64 { return t.ID }

// requireDecimal checks a decimal exactly, except on SQLite, which has no
// decimal type: a NUMERIC column with a fraction holds a REAL, so a sum of them
// is a float's, to two places.
func requireDecimal(
	t *testing.T,
	target testdb.Target,
	want string,
	got decimal.Decimal,
	msg string,
) {
	t.Helper()

	if target.Name == "sqlite" {
		got = got.Round(2)
	}

	require.True(
		t,
		decimal.RequireFromString(want).Equal(got),
		"%s: got %s, want %s",
		msg,
		got,
		want,
	)
}

// Ordering, comparing, text, arithmetic, dates and aggregates take a column of
// any type the database can work with: a pointer, an sql.Null, a uuid, a
// decimal or ant.DeletedAt, not only the plain types Go can order (#171, #174).
func TestConformanceHelpersTakeAnyColumn(t *testing.T) {
	testdb.ForEach(t, func(t *testing.T, target testdb.Target) {
		db := openTickets(t, target)
		ctx := t.Context()

		past := time.Date(2020, 3, 4, 5, 6, 7, 0, time.UTC)
		future := time.Date(2100, 1, 2, 3, 4, 5, 0, time.UTC)
		ann, bob := "Ann", "bob"
		five := 5

		require.NoError(t, db.Tickets.Insert(ctx,
			&Ticket{
				ID:     1,
				DueAt:  &past,
				Nick:   &ann,
				Votes:  &five,
				Ref:    uuid.MustParse("00000000-0000-0000-0000-000000000003"),
				Amount: decimal.RequireFromString("12.50"),
				Score:  sql.Null[float64]{V: 2, Valid: true},
			},
			&Ticket{
				ID:     2,
				DueAt:  &future,
				Nick:   &bob,
				Ref:    uuid.MustParse("00000000-0000-0000-0000-000000000001"),
				Amount: decimal.RequireFromString("3.25"),
				Score:  sql.Null[float64]{V: 1, Valid: true},
			},
			&Ticket{
				ID:     3,
				Ref:    uuid.MustParse("00000000-0000-0000-0000-000000000002"),
				Amount: decimal.RequireFromString("7.00"),
			},
		))

		require.NoError(t, db.TicketNotes.Insert(ctx,
			&TicketNote{
				ID:       10,
				TicketID: 1,
				At:       &past,
				Cost:     decimal.RequireFromString("1.10"),
			},
			&TicketNote{
				ID:       11,
				TicketID: 1,
				At:       &future,
				Cost:     decimal.RequireFromString("2.20"),
			},
			&TicketNote{ID: 12, TicketID: 2, Cost: decimal.RequireFromString("4.00")},
		))

		known := db.Tickets.Where(ant.IsNotNull(&Tickets.DueAt))

		t.Run("order by", func(t *testing.T) {
			got, err := known.OrderBy(&Tickets.DueAt).All(ctx)
			require.NoError(t, err)
			require.Equal(t, []int64{1, 2}, rowIDs(got, ticketID), "*time.Time")

			got, err = known.OrderByDesc(&Tickets.DueAt).All(ctx)
			require.NoError(t, err)
			require.Equal(t, []int64{2, 1}, rowIDs(got, ticketID))

			got, err = db.Tickets.OrderBy(&Tickets.Ref).All(ctx)
			require.NoError(t, err)
			require.Equal(t, []int64{2, 3, 1}, rowIDs(got, ticketID), "uuid.UUID")

			got, err = db.Tickets.OrderByDesc(&Tickets.Amount).All(ctx)
			require.NoError(t, err)
			require.Equal(t, []int64{1, 3, 2}, rowIDs(got, ticketID), "decimal.Decimal")

			got, err = db.Tickets.Where(ant.IsNotNull(&Tickets.Score)).
				OrderBy(&Tickets.Score).
				All(ctx)
			require.NoError(t, err)
			require.Equal(t, []int64{2, 1}, rowIDs(got, ticketID), "sql.Null[float64]")

			got, err = db.Tickets.Query().
				OrderBy(&Tickets.DeletedAt).
				ThenByDesc(&Tickets.Ref).
				All(ctx)
			require.NoError(t, err)
			require.Equal(t, []int64{1, 3, 2}, rowIDs(got, ticketID), "ant.DeletedAt, then a uuid")

			got, err = known.Latest(&Tickets.DueAt).All(ctx)
			require.NoError(t, err)
			require.Equal(t, []int64{2, 1}, rowIDs(got, ticketID), "Latest")

			got, err = known.Oldest(&Tickets.DueAt).All(ctx)
			require.NoError(t, err)
			require.Equal(t, []int64{1, 2}, rowIDs(got, ticketID), "Oldest")

			notes, err := db.Tickets.Query().
				JoinMany(&Tickets.Notes).
				Where(ant.IsNotNull(&TicketNotes.At)).
				OrderByDesc(&TicketNotes.At).
				Select(func(_ *Ticket, n *TicketNote) int64 { return n.ID }).
				All(ctx)
			require.NoError(t, err)
			require.Equal(t, []int64{11, 10}, notes, "a join's OrderBy")
		})

		t.Run("compare", func(t *testing.T) {
			now := time.Now()

			got, err := db.Tickets.Where(ant.Lt(&Tickets.DueAt, &now)).All(ctx)
			require.NoError(t, err)
			require.Equal(t, []int64{1}, rowIDs(got, ticketID), "Lt on *time.Time")

			got, err = db.Tickets.Where(ant.Gte(&Tickets.DueAt, &now)).All(ctx)
			require.NoError(t, err)
			require.Equal(t, []int64{2}, rowIDs(got, ticketID), "Gte on *time.Time")

			got, err = db.Tickets.Where(ant.Gt(&Tickets.Amount, decimal.NewFromInt(5))).
				OrderBy(&Tickets.ID).
				All(ctx)
			require.NoError(t, err)
			require.Equal(t, []int64{1, 3}, rowIDs(got, ticketID), "Gt on decimal.Decimal")

			got, err = db.Tickets.Where(ant.Lte(&Tickets.Score, sql.Null[float64]{V: 1, Valid: true})).
				All(ctx)
			require.NoError(t, err)
			require.Equal(t, []int64{2}, rowIDs(got, ticketID), "Lte on sql.Null[float64]")

			got, err = db.Tickets.Where(ant.Between(
				&Tickets.Ref,
				uuid.MustParse("00000000-0000-0000-0000-000000000002"),
				uuid.MustParse("00000000-0000-0000-0000-000000000003"),
			)).
				OrderBy(&Tickets.ID).
				All(ctx)
			require.NoError(t, err)
			require.Equal(t, []int64{1, 3}, rowIDs(got, ticketID), "Between on uuid.UUID")

			nothing, err := db.Tickets.Where(ant.Gt(&Tickets.DueAt, (*time.Time)(nil))).All(ctx)
			require.NoError(t, err)
			require.Empty(t, nothing, "a comparison with NULL matches nothing, as in SQL")
		})

		t.Run("dates", func(t *testing.T) {
			got, err := db.Tickets.Where(ant.Past(&Tickets.DueAt)).All(ctx)
			require.NoError(t, err)
			require.Equal(t, []int64{1}, rowIDs(got, ticketID), "Past on *time.Time")

			got, err = db.Tickets.Where(ant.Future(&Tickets.DueAt)).All(ctx)
			require.NoError(t, err)
			require.Equal(t, []int64{2}, rowIDs(got, ticketID), "Future on *time.Time")

			got, err = db.Tickets.Where(ant.Past(&Tickets.DeletedAt)).All(ctx)
			require.NoError(t, err)
			require.Empty(t, got, "Past on ant.DeletedAt")

			today, err := db.Tickets.Where(ant.Today(&Tickets.DueAt)).All(ctx)

			if has(target, dialect.DateCast) {
				require.NoError(t, err)
				require.Empty(t, today)
			} else {
				require.ErrorIs(t, err, ant.ErrUnsupported)
			}

			type year struct{ Year float64 }

			Y := ant.Fields[year]()
			years, err := ant.Project[year](
				known.OrderBy(&Tickets.ID),
				ant.IntoExpr(&Y.Year, ant.DatePart(&Tickets.DueAt, "year")),
			).
				All(ctx)

			if has(target, dialect.DatePart) {
				require.NoError(t, err)
				require.Equal(t, []year{{2020}, {2100}}, years, "DatePart on *time.Time")
			} else {
				require.ErrorIs(t, err, ant.ErrUnsupported)
			}
		})

		t.Run("text", func(t *testing.T) {
			got, err := db.Tickets.Where(ant.Like(&Tickets.Nick, "A%")).All(ctx)
			require.NoError(t, err)
			require.Equal(t, []int64{1}, rowIDs(got, ticketID), "Like on *string")

			got, err = db.Tickets.Where(ant.ILike(&Tickets.Nick, "B%")).All(ctx)
			require.NoError(t, err)
			require.Equal(t, []int64{2}, rowIDs(got, ticketID), "ILike on *string")

			lowered := "ann"
			got, err = db.Tickets.Where(ant.Lower(&Tickets.Nick).Eq(&lowered)).All(ctx)
			require.NoError(t, err)
			require.Equal(t, []int64{1}, rowIDs(got, ticketID), "Lower on *string")

			type text struct {
				Upper  *string
				Length int
			}

			X := ant.Fields[text]()
			out, err := ant.Project[text](
				db.Tickets.Where(ant.Eq(&Tickets.ID, int64(2))),
				ant.IntoExpr(&X.Upper, ant.Upper(&Tickets.Nick)),
				ant.IntoExpr(&X.Length, ant.Length(&Tickets.Nick)),
			).
				All(ctx)
			require.NoError(t, err)
			require.Len(t, out, 1)
			require.Equal(t, "BOB", *out[0].Upper, "Upper on *string")
			require.Equal(t, 3, out[0].Length, "Length on *string")
		})

		t.Run("aggregates", func(t *testing.T) {
			sum, err := ant.SumOf(ctx, db.Tickets.Query(), &Tickets.Amount)
			require.NoError(t, err)
			requireDecimal(t, target, "22.75", sum, "SumOf on decimal.Decimal")

			most, err := ant.MaxOf(ctx, db.Tickets.Query(), &Tickets.Amount)
			require.NoError(t, err)
			requireDecimal(t, target, "12.50", most, "MaxOf on decimal.Decimal")

			least, err := ant.MinOf(ctx, db.Tickets.Query(), &Tickets.Amount)
			require.NoError(t, err)
			requireDecimal(t, target, "3.25", least, "MinOf on decimal.Decimal")

			avg, err := ant.AvgOf(ctx, db.Tickets.Query(), &Tickets.Amount)
			require.NoError(t, err)
			require.InDelta(t, 22.75/3, avg, 0.001, "AvgOf on decimal.Decimal")

			votes, err := ant.SumOf(ctx, db.Tickets.Query(), &Tickets.Votes)
			require.NoError(t, err)
			require.NotNil(t, votes)
			require.Equal(t, 5, *votes, "SumOf on *int")

			none, err := ant.SumOf(
				ctx,
				db.Tickets.Where(ant.Eq(&Tickets.ID, int64(3))),
				&Tickets.Votes,
			)
			require.NoError(t, err)
			require.Nil(t, none, "the sum of NULLs is NULL, which a *int reads as nil")

			first, err := ant.MinOf(ctx, db.Tickets.Query(), &Tickets.Nick)
			require.NoError(t, err)
			require.Equal(t, "Ann", *first, "MinOf on *string")

			type totals struct {
				Sum   decimal.Decimal
				Votes *int
				Least decimal.Decimal
				Avg   float64
			}

			S := ant.Fields[totals]()
			rows, err := ant.Project[totals](db.Tickets.Query(),
				ant.IntoExpr(&S.Sum, ant.Sum(&Tickets.Amount)),
				ant.IntoExpr(&S.Votes, ant.Sum(&Tickets.Votes)),
				ant.IntoExpr(&S.Least, ant.Min(&Tickets.Amount)),
				ant.IntoExpr(&S.Avg, ant.Avg(&Tickets.Score)),
			).
				All(ctx)
			require.NoError(t, err)
			require.Len(t, rows, 1)
			requireDecimal(t, target, "22.75", rows[0].Sum, "Sum on decimal.Decimal")
			require.Equal(t, 5, *rows[0].Votes, "Sum on *int")
			requireDecimal(t, target, "3.25", rows[0].Least, "Min on decimal.Decimal")
			require.InDelta(t, 1.5, rows[0].Avg, 0.001, "Avg on sql.Null[float64]")
		})

		// SQLite reads an aggregate of a time column as text, which no time
		// type scans from, whatever field holds it (#317).
		t.Run("time aggregates", func(t *testing.T) {
			if target.Name == "sqlite" {
				t.Skip("SQLite reads MAX of a time column as text (#317)")
			}

			latest, err := ant.MaxOf(ctx, db.Tickets.Query(), &Tickets.DueAt)
			require.NoError(t, err)
			require.NotNil(t, latest)
			require.True(t, future.Equal(*latest), "MaxOf on *time.Time: %v", latest)

			type times struct{ Latest *time.Time }

			M := ant.Fields[times]()
			rows, err := ant.Project[times](db.Tickets.Query(),
				ant.IntoExpr(&M.Latest, ant.Max(&Tickets.DueAt)),
			).
				All(ctx)
			require.NoError(t, err)
			require.True(t, future.Equal(*rows[0].Latest), "Max on *time.Time")

			got, err := db.Tickets.OrderBy(&Tickets.ID).
				With(&Tickets.NoteLast, ant.MaxRelated(&Tickets.Notes, &TicketNotes.At)).
				With(&Tickets.NoteFirst, ant.MinRelated(&Tickets.Notes, &TicketNotes.Seen)).
				All(ctx)
			require.NoError(t, err)
			require.True(t, got[0].NoteLast.Valid)
			require.True(t, future.Equal(*got[0].NoteLast.V), "MaxRelated on *time.Time")
			require.False(t, got[0].NoteFirst.Valid, "MinRelated of NULLs on sql.Null[time.Time]")
			require.False(t, got[2].NoteLast.Valid)
		})

		t.Run("related", func(t *testing.T) {
			got, err := db.Tickets.OrderBy(&Tickets.ID).
				With(&Tickets.NoteTotal, ant.SumRelated(&Tickets.Notes, &TicketNotes.Cost)).
				With(&Tickets.NoteMost, ant.MaxRelated(&Tickets.Notes, &TicketNotes.Cost)).
				With(&Tickets.NoteAvg, ant.AvgRelated(&Tickets.Notes, &TicketNotes.Cost)).
				All(ctx)
			require.NoError(t, err)
			require.Len(t, got, 3)
			requireDecimal(t, target, "3.30", got[0].NoteTotal, "SumRelated on decimal.Decimal")
			requireDecimal(t, target, "2.20", got[0].NoteMost.V, "MaxRelated on decimal.Decimal")
			require.InDelta(t, 1.65, got[0].NoteAvg.V, 0.001, "AvgRelated on decimal.Decimal")
			requireDecimal(t, target, "0", got[2].NoteTotal, "SumRelated with no rows")
			require.False(t, got[2].NoteMost.Valid)

			ticket, err := db.Tickets.Query().IncludeOne(&Tickets.LastNote).Find(ctx, int64(1))
			require.NoError(t, err)
			require.NotNil(t, ticket.LastNote)
			require.Equal(t, int64(11), ticket.LastNote.ID, "RelationBuilder.Latest on *time.Time")
		})

		t.Run("increments", func(t *testing.T) {
			_, err := db.Tickets.Where(ant.In(&Tickets.ID, int64(1), int64(2))).
				Update(ctx, ant.Inc(&Tickets.Votes, 1))
			require.NoError(t, err)

			_, err = db.Tickets.Where(ant.Eq(&Tickets.ID, int64(3))).
				Update(ctx, ant.Dec(&Tickets.Votes, 2))
			require.NoError(t, err)

			_, err = db.Tickets.Where(ant.Eq(&Tickets.ID, int64(1))).
				Update(ctx, ant.Inc(&Tickets.Amount, decimal.RequireFromString("0.25")))
			require.NoError(t, err)

			_, err = db.Tickets.Where(ant.Eq(&Tickets.ID, int64(2))).
				Update(ctx, ant.Dec(&Tickets.Score, 0.5))
			require.NoError(t, err)

			got, err := db.Tickets.OrderBy(&Tickets.ID).All(ctx)
			require.NoError(t, err)
			require.Equal(t, 6, *got[0].Votes, "Inc on *int")
			require.Equal(t, 1, *got[1].Votes, "Inc on a NULL *int counts from 0")
			require.Equal(t, -2, *got[2].Votes, "Dec on a NULL *int counts from 0")
			requireDecimal(t, target, "12.75", got[0].Amount, "Inc on decimal.Decimal")
			require.InDelta(t, 0.5, got[1].Score.V, 0.001, "Dec on sql.Null[float64]")

			// A whole float is a whole step, as Inc[V Number] took the constant 2.0.
			_, err = db.Tickets.Where(ant.Eq(&Tickets.ID, int64(1))).
				Update(ctx, ant.Inc(&Tickets.Votes, 2.0))
			require.NoError(t, err)

			// An integer column would round a fraction away.
			_, err = db.Tickets.Where(ant.Eq(&Tickets.ID, int64(1))).
				Update(ctx, ant.Inc(&Tickets.Votes, 1.5))
			require.ErrorContains(t, err, "not 1.5")

			again, err := db.Tickets.Find(ctx, int64(1))
			require.NoError(t, err)
			require.Equal(t, 8, *again.Votes, "the refused step wrote nothing")
		})
	})
}
