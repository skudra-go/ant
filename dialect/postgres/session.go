package postgres

import (
	"context"
	"database/sql"
	"errors"
	"hash/fnv"
	"reflect"
	"time"

	"github.com/skudra-go/ant/schema"
)

// lockPoll is how often a waiting migrator tries for the lock.
const lockPoll = 200 * time.Millisecond

// Lock takes a session advisory lock on conn. It polls, rather than blocking in
// the server, so that a cancelled ctx leaves the connection usable. The lock ends
// with the session too, so a migrator that dies cannot hold it forever. A pooler in
// transaction mode, such as PgBouncer, breaks session locks, so run migrations on
// a direct connection.
func (impl) Lock(
	ctx context.Context,
	conn *sql.Conn,
	name string,
) (func(context.Context) error, error) {
	h := fnv.New64a()
	h.Write([]byte(name))
	key := int64(h.Sum64())

	for {
		var locked bool
		if err := conn.QueryRowContext(ctx, "SELECT pg_try_advisory_lock($1)", key).
			Scan(&locked); err != nil {
			return nil, err
		}

		if locked {
			return func(ctx context.Context) error {
				_, err := conn.ExecContext(ctx, "SELECT pg_advisory_unlock($1)", key)

				return err
			}, nil
		}

		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(lockPoll):
		}
	}
}

// Explain reads the driver error's extra fields. There is no interface for them,
// and the dialect must not import a driver, so it looks them up by name. pgx calls
// them Position, Detail, and Hint.
func (impl) Explain(err error) schema.Failure {
	var driverError interface{ SQLState() string }
	if !errors.As(err, &driverError) {
		return schema.Failure{}
	}

	v := reflect.Indirect(reflect.ValueOf(driverError))
	if v.Kind() != reflect.Struct {
		return schema.Failure{}
	}

	text := func(name string) string {
		if f := v.FieldByName(name); f.IsValid() && f.Kind() == reflect.String {
			return f.String()
		}

		return ""
	}
	out := schema.Failure{Detail: text("Detail"), Hint: text("Hint")}

	if position := v.FieldByName("Position"); position.IsValid() && position.CanInt() {
		out.Offset = int(position.Int())
	}

	return out
}

// SessionReset returns settings to what the connection started with, which keeps
// any search_path in the DSN.
func (impl) SessionReset() string { return "RESET ALL" }

// Summary reads the server version and each table's estimated rows and size.
func (impl) Summary(ctx context.Context, q schema.Querier) (*schema.Summary, error) {
	out := new(schema.Summary)

	err := queryRow(
		ctx,
		q,
		[]any{&out.Version, &out.Name, &out.Size},
		`SELECT current_setting('server_version'), current_database(), pg_size_pretty(pg_database_size(current_database()))`,
	)
	if err != nil {
		return nil, err
	}

	if out.Tables, err = relations(ctx, q, "'r', 'p'"); err != nil {
		return nil, err
	}

	// A materialized view holds rows, as a table does (#232).
	if out.MaterializedViews, err = relations(ctx, q, "'m'"); err != nil {
		return nil, err
	}

	return out, nil
}

// relations summarizes the relations of the given pg_class kinds in the current
// schema.
func relations(ctx context.Context, q schema.Querier, kinds string) ([]schema.TableSummary, error) {
	rows, err := q.QueryContext(ctx, `
		SELECT c.relname, GREATEST(c.reltuples, 0)::bigint, pg_size_pretty(pg_total_relation_size(c.oid))
		FROM pg_class c
		JOIN pg_namespace n ON n.oid = c.relnamespace
		WHERE c.relkind IN (`+kinds+`) AND n.nspname = current_schema()
		ORDER BY c.relname`)
	if err != nil {
		return nil, err
	}

	var out []schema.TableSummary

	err = each(rows, func() error {
		var t schema.TableSummary
		if err := rows.Scan(&t.Name, &t.Rows, &t.Size); err != nil {
			return err
		}

		out = append(out, t)

		return nil
	})

	return out, err
}

// IsMissingTable reports undefined_table.
func (impl) IsMissingTable(err error) bool {
	var driverError interface{ SQLState() string }

	return errors.As(err, &driverError) && driverError.SQLState() == "42P01"
}

// Database returns the name of the connected database.
func (impl) Database(ctx context.Context, q schema.Querier) (string, error) {
	var name string

	err := queryRow(ctx, q, []any{&name}, "SELECT current_database()")

	return name, err
}
