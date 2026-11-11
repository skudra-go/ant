package migrations

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/skudra-go/ant"
	"github.com/skudra-go/ant/dialect/postgres"
)

// Ticket uses a native enum, a sequence, and comments.
type Ticket struct {
	ID     int64
	Number int64
	Status string
	Code   string
}

func (Ticket) Configure(b *ant.ModelBuilder[Ticket]) {
	b.Comment("Support tickets")
	b.Column(&Tickets.Number).Sequence("ticket_numbers", ant.StartAt(1000))

	b.Column(&Tickets.Status).
		Enum("ticket_status", "open", "closed").
		Default("'open'").
		Comment("Where it stands")

	b.Column(&Tickets.Code).Fixed(4)
}

// Tickets is Ticket's probe.
var Tickets = ant.Fields[Ticket]()

// TicketDB holds it.
type TicketDB struct {
	ant.Context
	Tickets ant.Table[Ticket]
}

// Ticket2 adds an enum value.
type Ticket2 struct {
	ID     int64
	Number int64
	Status string
	Code   string
}

func (Ticket2) Configure(b *ant.ModelBuilder[Ticket2]) {
	b.Table("tickets")
	b.Comment("Support tickets")
	b.Column(&Ticket2s.Number).Sequence("ticket_numbers", ant.StartAt(1000))

	b.Column(&Ticket2s.Status).
		Enum("ticket_status", "open", "pending", "closed").
		Default("'open'").
		Comment("Where it stands")

	b.Column(&Ticket2s.Code).Fixed(4)
}

// Ticket2s is Ticket2's probe.
var Ticket2s = ant.Fields[Ticket2]()

// Ticket2DB holds it.
type Ticket2DB struct {
	ant.Context
	Tickets ant.Table[Ticket2]
}

// Ticket3 drops an enum value.
type Ticket3 struct {
	ID     int64
	Number int64
	Status string
	Code   string
}

func (Ticket3) Configure(b *ant.ModelBuilder[Ticket3]) {
	b.Table("tickets")
	b.Comment("Support tickets")
	b.Column(&Ticket3s.Number).Sequence("ticket_numbers", ant.StartAt(1000))

	b.Column(&Ticket3s.Status).
		Enum("ticket_status", "open", "pending").
		Default("'open'").
		Comment("Where it stands")

	b.Column(&Ticket3s.Code).Fixed(4)
}

// Ticket3s is Ticket3's probe.
var Ticket3s = ant.Fields[Ticket3]()

// Ticket3DB holds it.
type Ticket3DB struct {
	ant.Context
	Tickets ant.Table[Ticket3]
}

// Migrations create enums and sequences before the tables that use them.
func TestEnumsSequencesAndComments(t *testing.T) {
	each(t, func(t *testing.T, p *project) {
		must[TicketDB](p, "diff", "start")
		text := p.read("versions/0001_start.up.sql")
		require.Contains(t, text, `CREATE TYPE "ticket_status" AS ENUM ('open', 'closed')`)
		require.Contains(t, text, `CREATE SEQUENCE "ticket_numbers" START WITH 1000`)
		require.Contains(t, text, `"code" char(4) NOT NULL`)
		require.Contains(t, text, `COMMENT ON TABLE "tickets" IS 'Support tickets'`)
		must[TicketDB](p, "up")

		t.Run("the database fills what the model leaves zero", func(t *testing.T) {
			db := open[TicketDB](p)
			ticket := &Ticket{Code: "abcd"}
			require.NoError(t, db.Tickets.Insert(t.Context(), ticket))
			require.EqualValues(t, 1000, ticket.Number)
			require.Equal(t, "open", ticket.Status)
		})

		t.Run("drift sees nothing wrong", func(t *testing.T) {
			require.Contains(t, must[TicketDB](p, "drift"), "No drift")
		})

		t.Run("a new enum value is one statement", func(t *testing.T) {
			must[Ticket2DB](p, "diff", "add_pending")

			require.Contains(
				t,
				p.read("versions/0002_add_pending.up.sql"),
				`ALTER TYPE "ticket_status" ADD VALUE IF NOT EXISTS 'pending'`,
			)

			must[Ticket2DB](p, "up")
			require.Contains(t, must[Ticket2DB](p, "drift"), "No drift")
		})

		t.Run("removing a value needs a person, and diff says so", func(t *testing.T) {
			out := must[Ticket3DB](p, "diff", "remove_closed", "--dry-run")
			require.Contains(t, out, "PostgreSQL cannot remove value closed")
		})
	})
}

// LineItem has a foreign key of two columns, which relations cannot express.
type LineItem struct {
	ID      int64
	OrderID int64
	Region  string
}

func (LineItem) Configure(b *ant.ModelBuilder[LineItem]) {
	b.ForeignKey(ant.F(&LineItems.OrderID), ant.F(&LineItems.Region)).
		References(ant.F(&ShardedOrders.ID), ant.F(&ShardedOrders.Region)).
		OnDelete(ant.Cascade)
}

// ShardedOrder has a composite key.
type ShardedOrder struct {
	ID     int64
	Region string
}

func (ShardedOrder) Configure(b *ant.ModelBuilder[ShardedOrder]) {
	b.CompositeKey(ant.F(&ShardedOrders.ID), ant.F(&ShardedOrders.Region))
}

// Probes.
var (
	LineItems     = ant.Fields[LineItem]()
	ShardedOrders = ant.Fields[ShardedOrder]()
)

// ShardDB holds them.
type ShardDB struct {
	ant.Context
	Orders ant.Table[ShardedOrder]
	Items  ant.Table[LineItem]
}

func TestCompositeForeignKey(t *testing.T) {
	each(t, func(t *testing.T, p *project) {
		must[ShardDB](p, "diff", "start")

		require.Contains(
			t,
			p.read("versions/0001_start.up.sql"),
			`FOREIGN KEY ("order_id", "region") REFERENCES "sharded_orders" ("id", "region") ON DELETE CASCADE`,
		)

		must[ShardDB](p, "up")
		require.Contains(t, must[ShardDB](p, "drift"), "No drift")
	})
}

// Reading is a row in a table partitioned by month.
type Reading struct {
	ID    int64
	At    time.Time
	Value float64
}

func (Reading) Configure(b *ant.ModelBuilder[Reading]) {
	b.Key(&Readings.ID).NotGenerated()
	b.CompositeKey(ant.F(&Readings.ID), ant.F(&Readings.At))
	b.PartitionByRange(ant.F(&Readings.At))
}

// Readings is Reading's probe.
var Readings = ant.Fields[Reading]()

// PartitionDB holds it.
type PartitionDB struct {
	ant.Context
	Readings ant.Table[Reading]
}

// Migrations create the partitioned parent, and postgres.Partitions keeps its
// months, which is routine work rather than a schema change.
func TestPartitions(t *testing.T) {
	each(t, func(t *testing.T, p *project) {
		ctx := t.Context()

		must[PartitionDB](p, "diff", "start")
		require.Contains(t, p.read("versions/0001_start.up.sql"), `PARTITION BY RANGE ("at")`)
		must[PartitionDB](p, "up")
		db := open[PartitionDB](p)
		months, err := postgres.MonthlyPartitions[Reading](db)
		require.NoError(t, err)

		start := time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC)
		created, err := months.Ensure(ctx, start, 2)
		require.NoError(t, err)

		require.Equal(
			t,
			[]string{"readings_2026_01", "readings_2026_02", "readings_2026_03"},
			created,
		)

		t.Run("Ensure is safe to repeat", func(t *testing.T) {
			created, err := months.Ensure(ctx, start, 2)
			require.NoError(t, err)
			require.Empty(t, created)
		})

		t.Run("rows land in their month", func(t *testing.T) {
			require.NoError(t, db.Readings.Insert(ctx,
				&Reading{ID: 1, At: start, Value: 1},
				&Reading{ID: 2, At: start.AddDate(0, 1, 0), Value: 2}))

			require.Equal(t, 1, scalar[int](p, "SELECT count(*) FROM readings_2026_01"))
		})

		t.Run("drift looks at the parent, and ignores the months", func(t *testing.T) {
			require.Contains(t, must[PartitionDB](p, "drift"), "No drift")
		})

		t.Run("DropBefore removes whole months only", func(t *testing.T) {
			cutoff := time.Date(2026, 2, 15, 0, 0, 0, 0, time.UTC)
			dropped, err := months.DropBefore(ctx, cutoff)
			require.NoError(t, err)
			require.Equal(t, []string{"readings_2026_01"}, dropped)

			left, err := months.List(ctx)
			require.NoError(t, err)
			require.Len(t, left, 2)
			require.Equal(t, 1, scalar[int](p, "SELECT count(*) FROM readings"))
		})
	})
}
