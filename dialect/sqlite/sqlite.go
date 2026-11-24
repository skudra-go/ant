// Package sqlite is the SQLite dialect.
package sqlite

import (
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/skudra-go/ant/dialect"
	"github.com/skudra-go/ant/qb"
)

// Dialect is the SQLite dialect, for version 3.39 or later. Import a driver
// registered as "sqlite". SQLite locks the whole database for a write, so it has
// no row locks.
var Dialect dialect.Dialect = impl{}

type impl struct{}

var syntax = dialect.Syntax{
	Missing: []dialect.Feature{
		dialect.LateralJoin,
		dialect.Merge,
		dialect.RowLocks,
		dialect.DefaultKeyword,
		dialect.DateCast,
		dialect.DatePart,
		dialect.ReadOnlyInTx,
	},
	Unlimited:     "-1",
	Explain:       "EXPLAIN QUERY PLAN",
	NoILike:       true,
	NoRowIn:       true,
	BareSets:      true,
	ValuesUnion:   true,
	UTCTimes:      true,
	MaxParameters: 32766,
	// A statement on the connection steps its own cursor, beside the open one.
	RunsWhileStreaming: true,
	NoArrayParams:      true,
	PlainValues:        true,
}

func (impl) Name() string       { return "sqlite" }
func (impl) DriverName() string { return "sqlite" }

// CheckDSN refuses a data source name that leaves foreign keys, busy_timeout,
// or the transaction lock mode implicit. Each setting has an explicit opt-out:
// foreign_keys(0), busy_timeout(0), and _txlock=deferred respectively.
func (impl) CheckDSN(dsn string) error {
	_, query, _ := strings.Cut(dsn, "?")

	values, err := url.ParseQuery(query)
	if err != nil {
		return fmt.Errorf("ant: sqlite: %w", err)
	}

	hasPragma := func(want string, withValue bool) bool {
		for _, pragma := range values["_pragma"] {
			name, value, parenthesized := strings.Cut(pragma, "(")
			if !parenthesized {
				name, value, _ = strings.Cut(pragma, "=")
			}

			if !strings.EqualFold(strings.TrimSpace(name), want) {
				continue
			}

			if !withValue || strings.TrimSpace(strings.TrimSuffix(value, ")")) != "" {
				return true
			}
		}

		return false
	}

	if !values.Has("_foreign_keys") && !values.Has("_fk") && !hasPragma("foreign_keys", false) {
		return errors.New(
			"ant: sqlite: the data source name needs _pragma=foreign_keys(1); " +
				"without it SQLite enforces no foreign key, and accepts a row that points at nothing. " +
				"Use _pragma=foreign_keys(0) to keep them off on purpose",
		)
	}

	var missing []string

	busyTimeout := values.Get("_busy_timeout")

	if values.Has("_timeout") {
		// The driver gives the alias precedence, including an empty value that
		// suppresses the primary key.
		busyTimeout = values.Get("_timeout")
	}

	if !hasPragma("busy_timeout", true) && busyTimeout == "" {
		missing = append(missing, "_pragma=busy_timeout(5000)")
	}

	if values.Get("_txlock") == "" {
		missing = append(missing, "_txlock=immediate")
	}

	if len(missing) > 0 {
		return errors.New(
			"ant: sqlite: the data source name needs " + strings.Join(missing, " and ") + "; " +
				"without them concurrent writes can fail with SQLITE_BUSY. " +
				"Use _pragma=busy_timeout(0) or _txlock=deferred to keep a default on purpose",
		)
	}

	return nil
}

func (impl) Placeholder(n int) string { return "?" }

func (impl) Quote(ident string) string { return dialect.QuoteWith(ident, "\"", "\"") }

// AppendQuote and AppendPlaceholder are Quote and Placeholder written into a
// buffer, which Render uses.
func (impl) AppendQuote(dst []byte, ident string) []byte {
	return dialect.AppendQuoteWith(dst, ident, "\"", "\"")
}

func (impl) AppendPlaceholder(dst []byte, _ int) []byte { return append(dst, '?') }

func (impl) SupportsReturning() bool { return true }

func (impl) Syntax() *dialect.Syntax { return &syntax }

func (d impl) Render(stmt qb.Statement) (string, []any, error) {
	return dialect.Render(d, stmt)
}

func init() { dialect.RegisterErrors(state) }

// state translates SQLite's extended result codes, read through an interface so
// that any driver exposing them works.
func state(err error) string {
	var e interface{ Code() int }
	if !errors.As(err, &e) {
		return ""
	}

	switch e.Code() {
	case 1555, 2067: // primary key, unique
		return "23505"
	case 787:
		return "23503"
	case 1299:
		return "23502"
	case 275:
		return "23514"
	case 5, 261, 517: // busy
		return "40001"
	case 1: // SQLITE_ERROR, a code for many errors
		// SQLite gives a missing table no code of its own, only this text.
		if strings.Contains(err.Error(), "no such table") {
			return "42P01"
		}
	}

	return ""
}
