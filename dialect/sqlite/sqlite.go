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

// CheckDSN refuses a data source name that does not say whether foreign keys are
// on. SQLite turns them off on every new connection unless told, so REFERENCES
// constraints accept a row whose foreign key points at nothing, and no error
// says so. The driver takes _pragma=foreign_keys(1), or _foreign_keys or _fk; a
// value that turns them off is a choice, and is accepted.
func (impl) CheckDSN(dsn string) error {
	_, query, _ := strings.Cut(dsn, "?")

	values, err := url.ParseQuery(query)
	if err != nil {
		return fmt.Errorf("ant: sqlite: %w", err)
	}

	if values.Has("_foreign_keys") || values.Has("_fk") {
		return nil
	}

	for _, pragma := range values["_pragma"] {
		name, _, _ := strings.Cut(pragma, "(")
		name, _, _ = strings.Cut(name, "=")

		if strings.EqualFold(strings.TrimSpace(name), "foreign_keys") {
			return nil
		}
	}

	return errors.New(
		"ant: sqlite: the data source name needs _pragma=foreign_keys(1); " +
			"without it SQLite enforces no foreign key, and accepts a row that points at nothing. " +
			"Use _pragma=foreign_keys(0) to keep them off on purpose",
	)
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
	}

	return ""
}
