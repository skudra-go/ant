package querying

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"

	"github.com/skudra-go/ant"
	"github.com/skudra-go/ant/dialect/postgres"
	"github.com/skudra-go/ant/tests/features/fixture"
)

// Task has the columns most tables have: a time that may be unknown, optional
// text and counts, a uuid and money.
type Task struct {
	ID     int64
	DueAt  *time.Time
	Nick   *string
	Votes  *int
	Ref    uuid.UUID
	Amount decimal.Decimal
}

type TaskDB struct {
	ant.Context
	Tasks ant.Table[Task]
}

var Tasks = ant.Fields[Task]()

// Ordering, comparisons, text, dates, arithmetic and aggregates take a field
// of any type: a pointer, an sql.Null, a uuid or a decimal, as well as the
// plain types. The database decides what it can order and add up.
func TestHelpersTakeAnyColumn(t *testing.T) {
	ddl := `CREATE TABLE tasks (
		id BIGSERIAL PRIMARY KEY,
		due_at TIMESTAMPTZ NULL,
		nick TEXT NULL,
		votes INT NULL,
		ref UUID NOT NULL,
		amount NUMERIC(12,2) NOT NULL)`
	fixture.RunWith(t, ddl, func(t *testing.T, db *TaskDB) {
		ctx := t.Context()
		overdue := time.Now().Add(-time.Hour)
		nick := "Ann"

		require.NoError(t, db.Tasks.Insert(ctx,
			&Task{
				DueAt:  &overdue,
				Nick:   &nick,
				Ref:    uuid.New(),
				Amount: decimal.RequireFromString("9.99"),
			},
			&Task{Ref: uuid.New(), Amount: decimal.RequireFromString("0.01")},
		))

		now := time.Now()
		term := "an%"

		late, err := db.Tasks.
			Where(ant.Lt(&Tasks.DueAt, &now), ant.ILike(&Tasks.Nick, term), ant.Past(&Tasks.DueAt)).
			OrderBy(&Tasks.DueAt).
			ThenBy(&Tasks.Ref).
			All(ctx)
		require.NoError(t, err)
		require.Len(t, late, 1)

		total, err := ant.SumOf(ctx, db.Tasks.Query(), &Tasks.Amount)
		require.NoError(t, err)
		require.Equal(t, "10", total.String())

		_, err = db.Tasks.Where(ant.Gt(&Tasks.Amount, decimal.NewFromInt(5))).
			Update(ctx, ant.Inc(&Tasks.Votes, 1)) // NULL counts from 0
		require.NoError(t, err)

		voted, err := db.Tasks.Where(ant.Eq(&Tasks.Votes, new(1))).Count(ctx)
		require.NoError(t, err)
		require.EqualValues(t, 1, voted)

		named, err := db.Tasks.Where(
			ant.Collate(&Tasks.Nick, "C").Eq(&nick),
			postgres.Match(&Tasks.Nick, "simple", "ann"),
		).
			Count(ctx)
		require.NoError(t, err)
		require.EqualValues(t, 1, named)
	})
}
