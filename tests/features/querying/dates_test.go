package querying

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/skudra-go/ant"
	"github.com/skudra-go/ant/tests/features/fixture"
)

type Event struct {
	ID   int64
	Name string
	At   time.Time
}

type CalendarDB struct {
	ant.Context
	Events ant.Table[Event]
}

var Events = ant.Fields[Event]()

// Dates compare against the database's clock, so results do not depend on the
// application's.
func TestDates(t *testing.T) {
	ddl := `CREATE TABLE events (id BIGSERIAL PRIMARY KEY, name TEXT NOT NULL, at TIMESTAMPTZ NOT NULL)`
	fixture.RunWith(t, ddl, func(t *testing.T, db *CalendarDB) {
		ctx := t.Context()
		now, err := ant.Raw[time.Time](db, `SELECT now()`).First(ctx)
		require.NoError(t, err)

		require.NoError(t, db.Events.Insert(ctx,
			&Event{Name: "past", At: now.Add(-48 * time.Hour)},
			&Event{Name: "today", At: *now},
			&Event{Name: "future", At: now.Add(48 * time.Hour)}))

		names := func(c ant.Cond) []string {
			got, err := ant.Pluck(ctx, db.Events.Where(c).OrderBy(&Events.At), &Events.Name)
			require.NoError(t, err)

			return got
		}

		require.Equal(t, []string{"past", "today"}, names(ant.Past(&Events.At)))
		require.Equal(t, []string{"future"}, names(ant.Future(&Events.At)))
		require.Equal(t, []string{"today"}, names(ant.Today(&Events.At)))

		t.Run("DatePart", func(t *testing.T) {
			year := float64(now.UTC().Year())
			require.Contains(
				t,
				names(ant.SQLCond("? >= ?", ant.DatePart(&Events.At, "year"), year-1)),
				"today",
			)
		})

		t.Run("Latest and Oldest", func(t *testing.T) {
			got, err := db.Events.Query().Latest(&Events.At).First(ctx)
			require.NoError(t, err)
			require.Equal(t, "future", got.Name)
			got, err = db.Events.Query().Oldest(&Events.At).First(ctx)
			require.NoError(t, err)
			require.Equal(t, "past", got.Name)
		})

		t.Run("random and cleared ordering", func(t *testing.T) {
			got, err := db.Events.Query().OrderByRandom().All(ctx)
			require.NoError(t, err)
			require.Len(t, got, 3)
			got, err = db.Events.OrderBy(&Events.Name).ClearOrder().All(ctx)
			require.NoError(t, err)
			require.Len(t, got, 3)
		})
	})
}
