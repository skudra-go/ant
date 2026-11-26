package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"time"

	"github.com/skudra-go/ant"
	"github.com/skudra-go/ant/schema"
)

// Database is a context that embeds ant.Context.
type Database interface {
	DB() *sql.DB
}

// Partitions manages the monthly partitions of a table declared with
// b.PartitionByRange on a time column. Migrations create the parent table, and this
// creates and removes what hangs off it, which is routine work and not a schema
// change. Dropping a month is instant, where deleting its rows would take hours and
// leave the table bloated.
//
//	events := postgres.MonthlyPartitions[Event](db)
//	events.Ensure(ctx, time.Now(), 3)             // this month and the next three
//	events.DropBefore(ctx, cutoff)                // whole months older than cutoff
//
// Partitions are named table_2026_10, and run from midnight UTC on the first of
// the month. Partitions with other names are left alone.
type Partitions struct {
	db     *sql.DB
	schema string
	table  string
}

// MonthlyPartitions returns the manager for model T's table.
func MonthlyPartitions[T any](db Database) (*Partitions, error) {
	schemaName, table, err := ant.TableOf[T]()
	if err != nil {
		return nil, err
	}

	return &Partitions{db: db.DB(), schema: schemaName, table: table}, nil
}

func (p *Partitions) name(table string) string {
	return impl{}.table(schema.Ref{Schema: p.schema, Name: table})
}

func monthStart(t time.Time) time.Time {
	t = t.UTC()

	return time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, time.UTC)
}

// Partition is one month of a table.
type Partition struct {
	Name  string    `json:"name"`
	Month time.Time `json:"month"` // midnight UTC on the first
}

// Ensure creates the partitions for the month holding from and the months after
// it, and returns the ones it created. It is safe to run on a schedule, and from
// several instances at once.
func (p *Partitions) Ensure(
	ctx context.Context,
	from time.Time,
	monthsAhead int,
) ([]string, error) {
	existing, err := p.List(ctx)
	if err != nil {
		return nil, err
	}

	var created []string

	start := monthStart(from)

	for i := 0; i <= monthsAhead; i++ {
		month := start.AddDate(0, i, 0)
		name := fmt.Sprintf("%s_%04d_%02d", p.table, month.Year(), month.Month())

		if slices.ContainsFunc(existing, func(e Partition) bool { return e.Name == name }) {
			continue
		}

		statement := fmt.Sprintf(
			"CREATE TABLE IF NOT EXISTS %s PARTITION OF %s FOR VALUES FROM ('%s') TO ('%s')",
			p.name(name),
			p.name(p.table),
			month.Format("2006-01-02 15:04:05Z07:00"),
			month.AddDate(0, 1, 0).Format("2006-01-02 15:04:05Z07:00"),
		)

		if _, err := p.db.ExecContext(ctx, statement); err != nil {
			return created, fmt.Errorf("postgres: create partition %s: %w", name, err)
		}

		created = append(created, name)
	}

	return created, nil
}

// List returns the monthly partitions, oldest first.
func (p *Partitions) List(ctx context.Context) ([]Partition, error) {
	rows, err := p.db.QueryContext(
		ctx,
		`
		SELECT c.relname FROM pg_inherits i
		JOIN pg_class c ON c.oid = i.inhrelid
		WHERE i.inhparent = to_regclass($1)`,
		p.name(p.table),
	)
	if err != nil {
		return nil, fmt.Errorf("postgres: list partitions: %w", err)
	}

	var out []Partition

	pattern := regexp.MustCompile("^" + regexp.QuoteMeta(p.table) + `_(\d{4})_(\d{2})$`)

	err = each(rows, func() error {
		var name string
		if err := rows.Scan(&name); err != nil {
			return err
		}

		if m := pattern.FindStringSubmatch(name); m != nil {
			year, _ := strconv.Atoi(m[1])
			month, _ := strconv.Atoi(m[2])
			out = append(
				out,
				Partition{
					Name:  name,
					Month: time.Date(year, time.Month(month), 1, 0, 0, 0, 0, time.UTC),
				},
			)
		}

		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("postgres: list partitions: %w", err)
	}

	slices.SortFunc(out, func(a, b Partition) int { return a.Month.Compare(b.Month) })

	return out, nil
}

// DropBefore removes every partition whose whole month ended at or before cutoff,
// with its rows, and returns their names. A month partly after cutoff stays, so no
// row newer than cutoff is lost. When another partitioned table has a foreign key to
// this one, call DropBefore on that table first.
func (p *Partitions) DropBefore(ctx context.Context, cutoff time.Time) ([]string, error) {
	existing, err := p.List(ctx)
	if err != nil {
		return nil, err
	}

	var dropped []string

	for _, partition := range existing {
		if partition.Month.AddDate(0, 1, 0).After(cutoff) {
			continue
		}
		// Detached first, because a table that other partitions refer to cannot
		// be dropped directly, and a detach checks no rows still need it.
		tx, err := p.db.BeginTx(ctx, nil)
		if err != nil {
			return dropped, err
		}

		_, err = tx.ExecContext(
			ctx,
			fmt.Sprintf(
				"ALTER TABLE %s DETACH PARTITION %s",
				p.name(p.table),
				p.name(partition.Name),
			),
		)
		if err == nil {
			_, err = tx.ExecContext(ctx, "DROP TABLE "+p.name(partition.Name))
		}

		if err != nil {
			tx.Rollback()

			return dropped, fmt.Errorf("postgres: drop partition %s: %w", partition.Name, err)
		}

		if err := tx.Commit(); err != nil {
			return dropped, err
		}

		dropped = append(dropped, partition.Name)
	}

	return dropped, nil
}
