package migrate

import (
	"context"
	"database/sql"
	"fmt"
	"reflect"
	"strings"
	"time"

	"github.com/skudra-go/ant/schema"
)

// record is one row of the history table. Key is the version, or the file of a
// repeatable migration.
type record struct {
	Kind      Kind
	Key       string
	Name      string
	Checksum  string
	AppliedAt time.Time
	Duration  time.Duration
	Success   bool
	Error     string
}

type execer interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

// history reads and writes the table of applied migrations.
type history struct {
	d      schema.Dialect
	schema string
	table  string
}

func (h history) name() string {
	name := h.d.Quote(h.table)

	if h.schema != "" {
		name = h.d.Quote(h.schema) + "." + name
	}

	return name
}

// definition describes the table with the dialect's own types, so history needs
// no SQL per database.
func (h history) definition() (*schema.Table, error) {
	column := func(name string, t reflect.Type, nullable bool) (*schema.Column, error) {
		columnType, ok := h.d.ColumnType(t, schema.Facets{})
		if !ok {
			return nil, fmt.Errorf("ant: dialect has no type for %s", t)
		}

		return &schema.Column{Name: name, Type: columnType, Nullable: nullable}, nil
	}

	text := reflect.TypeFor[string]()
	whole := reflect.TypeFor[int64]()
	flag := reflect.TypeFor[bool]()
	when := reflect.TypeFor[time.Time]()

	t := &schema.Table{
		Schema: h.schema,
		Name:   h.table,
		PrimaryKey: &schema.Key{
			Name:    schema.PrimaryKeyName(h.d, h.table),
			Columns: []string{"kind", "version"},
		},
	}

	for _, c := range []struct {
		name     string
		kind     reflect.Type
		nullable bool
	}{
		{"kind", text, false},
		{"version", text, false},
		{"name", text, false},
		{"checksum", text, false},
		{"applied_at", when, false},
		{"duration_ms", whole, false},
		{"success", flag, false},
		{"error", text, true},
	} {
		out, err := column(c.name, c.kind, c.nullable)
		if err != nil {
			return nil, err
		}

		t.Columns = append(t.Columns, out)
	}

	return t, nil
}

func (h history) ensure(ctx context.Context, e execer) error {
	t, err := h.definition()
	if err != nil {
		return err
	}

	statements, err := h.d.EnsureTable(t)
	if err != nil {
		return err
	}

	for _, statement := range statements {
		if _, err := e.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("ant: create history table: %w", err)
		}
	}

	return nil
}

// read returns every row, or nothing if the table does not exist yet.
func (h history) read(ctx context.Context, q schema.Querier) ([]record, error) {
	rows, err := q.QueryContext(
		ctx,
		"SELECT kind, version, name, checksum, applied_at, duration_ms, success, error FROM "+h.name(),
	)
	if err != nil {
		if h.d.IsMissingTable(err) {
			return nil, nil
		}

		return nil, fmt.Errorf("ant: read history: %w", err)
	}
	defer rows.Close()

	var out []record

	for rows.Next() {
		var (
			r       record
			ms      int64
			failure sql.NullString
		)

		if err := rows.Scan(
			&r.Kind,
			&r.Key,
			&r.Name,
			&r.Checksum,
			&r.AppliedAt,
			&ms,
			&r.Success,
			&failure,
		); err != nil {
			return nil, fmt.Errorf("ant: read history: %w", err)
		}

		r.Duration, r.Error = time.Duration(ms)*time.Millisecond, failure.String
		out = append(out, r)
	}

	return out, rows.Err()
}

func (h history) remove(ctx context.Context, e execer, kind Kind, key string) error {
	_, err := e.ExecContext(
		ctx,
		fmt.Sprintf(
			"DELETE FROM %s WHERE kind = %s AND version = %s",
			h.name(),
			h.d.Placeholder(1),
			h.d.Placeholder(2),
		),
		string(kind),
		key,
	)

	return err
}

func (h history) write(ctx context.Context, e execer, r record) error {
	if err := h.remove(ctx, e, r.Kind, r.Key); err != nil {
		return err
	}

	var failure any

	if r.Error != "" {
		failure = r.Error
	}

	marks := make([]string, 8)
	for i := range marks {
		marks[i] = h.d.Placeholder(i + 1)
	}

	_, err := e.ExecContext(
		ctx,
		fmt.Sprintf(
			"INSERT INTO %s (kind, version, name, checksum, applied_at, duration_ms, success, error) VALUES (%s)",
			h.name(),
			strings.Join(marks, ", "),
		),
		string(r.Kind),
		r.Key,
		r.Name,
		r.Checksum,
		r.AppliedAt.UTC(),
		r.Duration.Milliseconds(),
		r.Success,
		failure,
	)

	return err
}

// writeAlone is write in a transaction of its own, for a connection outside
// one, so a crash between taking out the old row and putting in the new one
// cannot leave neither.
func (h history) writeAlone(ctx context.Context, conn *sql.Conn, r record) error {
	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if err := h.write(ctx, tx, r); err != nil {
		return err
	}

	return tx.Commit()
}

// literal quotes text as a SQL string, for scripts run by other tools.
func literal(text string) string {
	return "'" + strings.ReplaceAll(text, "'", "''") + "'"
}

// insertScript returns the statement that records m as applied, as plain SQL.
func (h history) insertScript(m Migration) string {
	return fmt.Sprintf(
		"INSERT INTO %s (kind, version, name, checksum, applied_at, duration_ms, success) VALUES (%s, %s, %s, %s, CURRENT_TIMESTAMP, 0, true)",
		h.name(),
		literal(string(m.Kind)),
		literal(m.key()),
		literal(m.Name),
		literal(m.Checksum()),
	)
}
