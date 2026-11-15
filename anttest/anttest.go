// Package anttest holds helpers for testing code that uses ant, against a real
// database:
//
//	func TestSignup(t *testing.T) {
//		db := anttest.Isolate(t, shared)                   // rolled back at the end
//		anttest.Load(t, db, fixtures, "plans.json")        // rows from a file
//		user := users.Create(t, &db.Users)                 // a row from a factory
//		...
//	}
package anttest

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"slices"
	"strings"
	"testing"

	"github.com/skudra-go/ant"
	"github.com/skudra-go/ant/dialect"
)

// Isolate returns a context bound to a transaction that rolls back when t ends, so
// a test leaves nothing behind and tests can share a database. It is the same type
// as db. Code that commits for real, such as through a second connection, does not
// see its rows, so a test of concurrency needs a database of its own.
func Isolate[DB any](t testing.TB, db *DB) *DB {
	t.Helper()
	// Detached, because the test's context ends before its cleanups run.
	tx, err := ant.Begin(context.WithoutCancel(t.Context()), db)
	if err != nil {
		t.Fatalf("anttest: begin: %v", err)
	}

	t.Cleanup(func() {
		if err := any(tx).(interface{ Rollback() error }).Rollback(); err != nil {
			t.Errorf("anttest: rollback: %v", err)
		}
	})

	return tx
}

// Load inserts rows from JSON files. Each file is named after a table and holds an
// array of objects keyed by column, such as users.json:
//
//	[{"id": 1, "name": "Ada", "created_at": "2026-01-01T00:00:00Z"}]
//
// Files load in the order given, so parents go first. Rows are written as they are,
// with no hooks, defaults from Go, or timestamps. A nested object or array is
// stored as JSON text. Names are quoted for the context's database, and a value
// must be in a form that database reads: MySQL and MariaDB take a time as
// "2026-01-01 00:00:00", which the others read too, and not with T and Z.
func Load[DB any](t testing.TB, db *DB, fsys fs.FS, files ...string) {
	t.Helper()

	h, ok := any(db).(interface {
		ant.Handle
		Dialect() dialect.Dialect
	})
	if !ok {
		t.Fatalf("anttest: %T does not embed ant.Context", db)
	}

	for _, file := range files {
		if err := loadFile(t, h, h.Dialect(), fsys, file); err != nil {
			t.Fatalf("anttest: load %s: %v", file, err)
		}
	}
}

func loadFile(t testing.TB, h ant.Handle, d dialect.Dialect, fsys fs.FS, file string) error {
	data, err := fs.ReadFile(fsys, file)
	if err != nil {
		return err
	}

	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()

	var rows []map[string]any
	if err := decoder.Decode(&rows); err != nil {
		return err
	}

	table := strings.TrimSuffix(file[strings.LastIndex(file, "/")+1:], ".json")

	for i, row := range rows {
		statement, args, err := insertRow(d, table, row)
		if err != nil {
			return fmt.Errorf("row %d: %w", i, err)
		}

		if _, err := ant.Raw[any](h, statement, args...).Exec(t.Context()); err != nil {
			return fmt.Errorf("row %d: %w", i, err)
		}
	}

	return nil
}

// insertRow writes one row's INSERT, with names quoted as d quotes them: a
// double quote is a string to MySQL and MariaDB, which quote with backticks.
func insertRow(d dialect.Dialect, table string, row map[string]any) (string, []any, error) {
	columns := make([]string, 0, len(row))

	for column := range row {
		columns = append(columns, column)
	}

	slices.Sort(columns)
	args := make([]any, len(columns))

	marks := make([]string, len(columns))

	for i, column := range columns {
		marks[i] = "?"

		value, err := plain(row[column])
		if err != nil {
			return "", nil, err
		}

		args[i] = value
		columns[i] = d.Quote(column)
	}

	return fmt.Sprintf(
		"INSERT INTO %s (%s) VALUES (%s)",
		d.Quote(table),
		strings.Join(columns, ", "),
		strings.Join(marks, ", "),
	), args, nil
}

func plain(v any) (any, error) {
	switch v := v.(type) {
	case json.Number:
		if n, err := v.Int64(); err == nil {
			return n, nil
		}

		return v.Float64()
	case map[string]any, []any:
		text, err := json.Marshal(v)

		return string(text), err
	}

	return v, nil
}
