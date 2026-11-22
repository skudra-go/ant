package exec

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/skudra-go/ant/dialect"
)

// Statement is one SQL statement on its way to the database.
type Statement struct {
	SQL     string
	Args    []any
	Replica bool // read from a replica
}

// Outcome is what happened to a statement.
type Outcome struct {
	Duration time.Duration
	Err      error
}

// Interceptor observes statements. Before can replace the context, and stop the
// statement by returning an error.
type Interceptor interface {
	Before(ctx context.Context, s *Statement) (context.Context, error)
	After(ctx context.Context, s *Statement, o Outcome)
}

// Observed runs statements through interceptors.
type Observed struct {
	Executor
	Interceptors []Interceptor
	Replica      bool
	Detailed     bool
}

// Error is a failed statement. Its message is the driver's own, unless Detailed
// is set, so SQL only reaches logs and users on request.
type Error struct {
	SQL      string
	Err      error
	Detailed bool
}

func (e *Error) Error() string {
	msg := e.Err.Error()

	// The drivers' messages say only that the table is not there (#301).
	// Migrations are PostgreSQL's alone, and EnsureCreated runs everywhere.
	if undefinedTable(e.Err) {
		msg += "; the table does not exist: create it with EnsureCreated, or with a migration on PostgreSQL"
	}

	if e.Detailed {
		return msg + " [sql: " + e.SQL + "]"
	}

	return msg
}

// undefinedTable reports whether err is a database's error for a table that
// does not exist, which dialects give as PostgreSQL's code 42P01.
func undefinedTable(err error) bool {
	var e interface{ SQLState() string }
	if errors.As(err, &e) {
		return e.SQLState() == "42P01"
	}

	return dialect.StateOf(err) == "42P01"
}

// Unwrap returns the driver's error.
func (e *Error) Unwrap() error { return e.Err }

// Plain runs statements with no interceptors, and records which one failed.
type Plain struct {
	Executor
	Detailed bool
}

// ExecContext implements Executor.
func (p *Plain) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	result, err := p.Executor.ExecContext(ctx, query, args...)
	if err != nil {
		return nil, &Error{query, err, p.Detailed}
	}

	return result, nil
}

// QueryContext implements Executor.
func (p *Plain) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	rows, err := p.Executor.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, &Error{query, err, p.Detailed}
	}

	return rows, nil
}

// Wrap returns e with error context, and interceptors if there are any.
func Wrap(e Executor, interceptors []Interceptor, replica, detailed bool) Executor {
	if len(interceptors) == 0 {
		return &Plain{e, detailed}
	}

	return &Observed{e, interceptors, replica, detailed}
}

func (o *Observed) before(ctx context.Context, s *Statement) (context.Context, func(error), error) {
	for i, in := range o.Interceptors {
		next, err := in.Before(ctx, s)
		if err != nil {
			for _, earlier := range o.Interceptors[:i] {
				earlier.After(ctx, s, Outcome{Err: err})
			}

			return ctx, nil, err
		}

		ctx = next
	}

	start := time.Now()

	return ctx, func(err error) {
		outcome := Outcome{Duration: time.Since(start), Err: err}

		for i := len(o.Interceptors) - 1; i >= 0; i-- {
			o.Interceptors[i].After(ctx, s, outcome)
		}
	}, nil
}

// ExecContext implements Executor.
func (o *Observed) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	s := &Statement{SQL: query, Args: args, Replica: o.Replica}

	ctx, done, err := o.before(ctx, s)
	if err != nil {
		return nil, err
	}

	result, err := o.Executor.ExecContext(ctx, s.SQL, s.Args...)
	done(err)

	if err != nil {
		return nil, &Error{s.SQL, err, o.Detailed}
	}

	return result, nil
}

// QueryContext implements Executor.
func (o *Observed) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	s := &Statement{SQL: query, Args: args, Replica: o.Replica}

	ctx, done, err := o.before(ctx, s)
	if err != nil {
		return nil, err
	}

	rows, err := o.Executor.QueryContext(ctx, s.SQL, s.Args...)
	done(err)

	if err != nil {
		return nil, &Error{s.SQL, err, o.Detailed}
	}

	return rows, nil
}
