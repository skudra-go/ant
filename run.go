package ant

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/skudra-go/ant/internal/exec"
	"github.com/skudra-go/ant/internal/meta"
	"github.com/skudra-go/ant/internal/scan"
	"github.com/skudra-go/ant/qb"
)

// primaryKey is the context key of OnPrimary.
type primaryKey struct{}

// OnPrimary sends the reads made with ctx to the primary, not a replica.
func OnPrimary(ctx context.Context) context.Context {
	return context.WithValue(ctx, primaryKey{}, true)
}

// reader chooses where a statement runs: a plain SELECT outside a transaction can
// use a replica, unless this context wrote recently.
func (s *state) reader(ctx context.Context, stmt qb.Statement) exec.Executor {
	selecting, ok := stmt.(*qb.Select)
	if !ok {
		s.wrote()

		return s.exec
	}

	return s.selectReader(ctx, selecting.Lock != nil)
}

// selectReader is reader for a SELECT, which locked says takes row locks.
func (s *state) selectReader(ctx context.Context, locked bool) exec.Executor {
	if len(s.replicas) == 0 || s.tx != nil || locked || ctx.Value(primaryKey{}) != nil {
		return s.exec
	}

	if sticky := s.settings.sticky; sticky > 0 &&
		time.Since(time.Unix(0, s.lastWrite.Load())) < sticky {
		return s.exec
	}

	return s.replicas[s.shared.nextReplica.Add(1)%uint64(len(s.replicas))]
}

// wrote records a write, for sticky reads.
func (s *state) wrote() {
	if s.settings.sticky > 0 {
		s.lastWrite.Store(time.Now().UnixNano())
	}
}

// query renders and runs a statement that returns rows.
func (s *state) query(ctx context.Context, stmt qb.Statement) (*sql.Rows, error) {
	text, args, err := s.dialect.Render(qb.Contextual{Context: ctx, Statement: stmt})
	if err != nil {
		return nil, err
	}

	return s.reader(ctx, stmt).QueryContext(ctx, text, args...)
}

// queryOn is query on a chosen executor, as a write's own read must be.
func (s *state) queryOn(
	ctx context.Context,
	to exec.Executor,
	stmt qb.Statement,
) (*sql.Rows, error) {
	text, args, err := s.dialect.Render(qb.Contextual{Context: ctx, Statement: stmt})
	if err != nil {
		return nil, err
	}

	s.wrote()

	return to.QueryContext(ctx, text, args...)
}

// querySQL runs a SELECT with no row locks, already rendered.
func (s *state) querySQL(ctx context.Context, text string, args []any) (*sql.Rows, error) {
	return s.selectReader(ctx, false).QueryContext(ctx, text, args...)
}

// run renders and runs a statement, returning the rows affected.
func (s *state) run(ctx context.Context, stmt qb.Statement) (int64, error) {
	return s.runOn(ctx, s.exec, stmt)
}

// runOn is run on a chosen executor.
func (s *state) runOn(ctx context.Context, to exec.Executor, stmt qb.Statement) (int64, error) {
	text, args, err := s.dialect.Render(qb.Contextual{Context: ctx, Statement: stmt})
	if err != nil {
		return 0, err
	}

	s.wrote()

	result, err := to.ExecContext(ctx, text, args...)
	if err != nil {
		return 0, err
	}

	return result.RowsAffected()
}

// debugStatement logs a statement for Query.Debug.
func (s *state) debugStatement(ctx context.Context, stmt qb.Statement) {
	text, args, err := s.dialect.Render(qb.Contextual{Context: ctx, Statement: stmt})
	if err != nil {
		return
	}

	slog.InfoContext(ctx, "ant: query", "sql", text, "values", args)
}

// checkColumns enforces WithStrict.
func (s *state) checkColumns(rows *sql.Rows, model *meta.Model) error {
	if !s.settings.strict {
		return nil
	}

	columns, err := rows.Columns()
	if err != nil {
		return err
	}

	for _, name := range columns {
		if model.Column(name) == nil {
			return errors.Join(
				fmt.Errorf("%w: %s on %s", ErrUnknownColumn, name, model.Type.Name()),
				rows.Close(),
			)
		}
	}

	return nil
}

// collectPairs reads two-column rows and closes them.
func collectPairs(rows *sql.Rows) (out [][2]any, err error) {
	defer func() { err = errors.Join(err, rows.Close()) }()

	for rows.Next() {
		var pair [2]any
		if err := rows.Scan(&pair[0], &pair[1]); err != nil {
			return nil, err
		}

		out = append(out, pair)
	}

	return out, rows.Err()
}

// collect reads all rows and closes them.
func collect[T any](rows *sql.Rows) (out []T, err error) {
	defer func() { err = errors.Join(err, rows.Close()) }()

	into, err := scan.For[T](rows)
	if err != nil {
		return nil, err
	}

	out = []T{}

	// One value is scanned into for every row and copied into the results,
	// which saves an allocation per row. It is cleared first, since a scan
	// leaves a field with no column as it was.
	var v, zero T

	for rows.Next() {
		v = zero

		if err := into(&v); err != nil {
			return nil, err
		}

		out = append(out, v)
	}

	return out, rows.Err()
}

// entityBlock is the most entities one block holds. A block lives as long as
// any entity in it, so it is kept small enough for that to cost little.
const entityBlock = 1024

// collectEntities reads all rows and closes them. Entities are allocated in
// blocks, which saves most of the per-row allocations. limit is the most rows
// the statement can return, or 0 if it has no limit: the first block is no
// larger, so that reading one row by its key does not allocate eight.
func collectEntities[T any](rows *sql.Rows, limit int) (out []*T, err error) {
	defer func() { err = errors.Join(err, rows.Close()) }()

	into, err := scan.For[T](rows)
	if err != nil {
		return nil, err
	}

	first := 8
	if limit > 0 && limit < first {
		first = limit
	}

	out = make([]*T, 0, first)

	var block []T

	for rows.Next() {
		if len(block) == 0 {
			block = make([]T, min(max(len(out), first), entityBlock))
		}

		v := &block[0]
		block = block[1:]

		if err := into(v); err != nil {
			return nil, err
		}

		out = append(out, v)
	}

	return out, rows.Err()
}

// first reads one row and closes the rows, or returns ErrNotFound.
func first[T any](rows *sql.Rows) (*T, error) {
	v := new(T)
	if err := readInto(rows, v); err != nil {
		return nil, err
	}

	return v, nil
}

// readInto scans one row into v and closes the rows, or returns ErrNotFound.
func readInto[T any](rows *sql.Rows, v *T) (err error) {
	defer func() { err = errors.Join(err, rows.Close()) }()

	into, err := scan.For[T](rows)
	if err != nil {
		return err
	}

	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return err
		}

		return ErrNotFound
	}

	return into(v)
}

// stream yields rows until the consumer stops or an error occurs.
func stream[T any](rows *sql.Rows, yield func(T, error) bool) {
	defer rows.Close()

	var zero T

	into, err := scan.For[T](rows)
	if err != nil {
		yield(zero, err)

		return
	}

	// One value is scanned into for every row, and the consumer gets a copy,
	// which saves an allocation per row. It is cleared first, since a scan
	// leaves a field with no column as it was.
	var v T

	for rows.Next() {
		v = zero

		if err := into(&v); err != nil {
			yield(zero, err)

			return
		}

		if !yield(v, nil) {
			return
		}
	}

	if err := rows.Err(); err != nil {
		yield(zero, err)
	}
}
