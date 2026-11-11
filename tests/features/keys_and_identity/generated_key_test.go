package keys_and_identity

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/skudra-go/ant"
	"github.com/skudra-go/ant/tests/features/fixture"
)

// An integer key is database-generated. A zero key means "assign one". This is
// the only place ant reads meaning into a zero value.
type Ticket struct {
	ID    int64
	Title string
}

// NotGenerated says the application always supplies the key, so zero is a real
// value.
type Seat struct {
	Number int
	Row    string
}

func (Seat) Configure(b *ant.ModelBuilder[Seat]) {
	b.Key(&Seats.Number).NotGenerated()
}

var Seats = ant.Fields[Seat]()

type DeskDB struct {
	ant.Context
	Tickets ant.Table[Ticket]
	Seats   ant.Table[Seat]
}

func TestGeneratedKey(t *testing.T) {
	ddl := `CREATE TABLE tickets (id BIGSERIAL PRIMARY KEY, title TEXT NOT NULL);
	        CREATE TABLE seats (number INT PRIMARY KEY, row TEXT NOT NULL)`
	fixture.RunWith(t, ddl, func(t *testing.T, db *DeskDB) {
		ctx := t.Context()

		t.Run("the key is copied back", func(t *testing.T) {
			ticket := &Ticket{Title: "One"}
			require.NoError(t, db.Tickets.Insert(ctx, ticket))
			require.NotZero(t, ticket.ID)
		})

		t.Run("each entity gets its own key", func(t *testing.T) {
			a, b := &Ticket{Title: "A"}, &Ticket{Title: "B"}
			require.NoError(t, db.Tickets.Insert(ctx, a, b))
			require.NotZero(t, a.ID)
			require.NotZero(t, b.ID)
			require.NotEqual(t, a.ID, b.ID)
		})

		t.Run("NotGenerated writes zero", func(t *testing.T) {
			seat := &Seat{Number: 0, Row: "A"}
			require.NoError(t, db.Seats.Insert(ctx, seat))
			found, err := db.Seats.Find(ctx, 0)
			require.NoError(t, err)
			require.Equal(t, "A", found.Row)
		})
	})
}
