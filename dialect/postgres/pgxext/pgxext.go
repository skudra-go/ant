// Package pgxext holds PostgreSQL features that database/sql cannot reach and that
// need pgx itself, so that ant and dialect/postgres stay free of any driver. Import
// it only if you use pgx through its stdlib driver, as ant's documentation does.
package pgxext

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"

	"github.com/skudra-go/ant"
)

// Database is a context that embeds ant.Context.
type Database interface {
	ant.Handle
	DB() *sql.DB
}

var (
	// ErrNotPgx reports a connection from some other driver.
	ErrNotPgx = errors.New("ant: pgxext needs the pgx stdlib driver")
	// ErrInTx reports a context bound to a transaction. pgxext runs on a pool
	// connection of its own, since database/sql gives no access to a
	// transaction's, so its writes would escape the transaction.
	ErrInTx = errors.New("ant: pgxext cannot run inside a transaction")
)

func withConn(ctx context.Context, h Database, use func(*pgx.Conn) error) error {
	conn, err := h.DB().Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()

	return conn.Raw(func(raw any) error {
		c, ok := raw.(*stdlib.Conn)
		if !ok {
			return fmt.Errorf("%w: got %T", ErrNotPgx, raw)
		}

		return use(c.Conn())
	})
}

// Copy loads entities with COPY, which is by far the fastest way to insert many
// rows. Timestamps and keys made in Go are filled in, as Insert does. Unlike
// Insert, it runs outside any transaction of yours on a connection of its own, keys
// from the database do not come back, relations are ignored, and hooks do not run.
func Copy[T any](ctx context.Context, h Database, entities []*T) (int64, error) {
	bulk, err := ant.PrepareBulk(h, entities)
	if err != nil {
		return 0, err
	}

	table := pgx.Identifier{bulk.Table}

	if bulk.Schema != "" {
		table = pgx.Identifier{bulk.Schema, bulk.Table}
	}

	var copied int64

	err = withConn(ctx, h, func(c *pgx.Conn) error {
		var err error

		copied, err = c.CopyFrom(ctx, table, bulk.Columns, pgx.CopyFromRows(bulk.Rows))

		return err
	})

	return copied, err
}

// Batch queues statements to send in one round trip, which pays off when a network
// sits between the application and the database. They run in order in one implicit
// transaction: if one fails, none take effect.
type Batch struct {
	batch pgx.Batch
	err   error
}

// Exec queues a statement. Its text and arguments can come from ToSQL, UpdateSQL,
// or DeleteSQL.
func (b *Batch) Exec(query string, args ...any) *Batch {
	b.batch.Queue(query, args...)

	return b
}

// Add queues a statement from a function that returns one, such as UpdateSQL.
func (b *Batch) Add(query string, args []any, err error) *Batch {
	if err != nil {
		b.err = errors.Join(b.err, err)

		return b
	}

	return b.Exec(query, args...)
}

// Len returns the number of queued statements.
func (b *Batch) Len() int { return b.batch.Len() }

// Send runs the queued statements and returns the rows each affected.
func (b *Batch) Send(ctx context.Context, h Database) ([]int64, error) {
	if b.err != nil {
		return nil, b.err
	}

	out := make([]int64, 0, b.batch.Len())

	err := withConn(ctx, h, func(c *pgx.Conn) (failure error) {
		results := c.SendBatch(ctx, &b.batch)

		defer func() { failure = errors.Join(failure, results.Close()) }()

		for range b.batch.Len() {
			tag, err := results.Exec()
			if err != nil {
				return err
			}

			out = append(out, tag.RowsAffected())
		}

		return nil
	})
	if err != nil {
		return nil, err
	}

	return out, nil
}
