// Package dialect defines the interface each database implements, plus
// helpers they share.
package dialect

import (
	"strconv"
	"strings"

	"github.com/skudra-go/ant/qb"
)

// Dialect adapts ant to one database.
type Dialect interface {
	// Name identifies the dialect, such as "postgres".
	Name() string
	// DriverName is the database/sql driver to open. The application imports
	// the driver, so ant has no dependency on it.
	DriverName() string
	// Placeholder returns the bind parameter for position n, counted from 1.
	Placeholder(n int) string
	// Quote quotes an identifier.
	Quote(ident string) string
	// SupportsReturning reports whether an INSERT can hand back columns, through
	// RETURNING or an equivalent. Without it, keys come from LastInsertId.
	SupportsReturning() bool
	// Render converts a statement to SQL and its arguments.
	Render(stmt qb.Statement) (sql string, args []any, err error)
}

// Appender is implemented by a dialect that can write a quoted identifier and a
// placeholder straight into the statement being rendered. It is optional: Render
// uses it when it is there, which saves an allocation for every identifier and
// placeholder of every statement, and Quote and Placeholder when it is not. The
// two must write what Quote and Placeholder return.
type Appender interface {
	// AppendQuote appends the quoted identifier to dst.
	AppendQuote(dst []byte, ident string) []byte
	// AppendPlaceholder appends the bind parameter for position n to dst.
	AppendPlaceholder(dst []byte, n int) []byte
}

// DSNChecker is implemented by a dialect whose driver needs an option in the
// data source name for its rows to scan into the types ant maps columns to.
// Open calls it before connecting, so a DSN that would fail on the first read
// is refused where it is written rather than at the first query that scans a
// column the option governs.
type DSNChecker interface {
	// CheckDSN reports what the data source name is missing, or nil.
	CheckDSN(dsn string) error
}

// ChangedRowsOnly is implemented by a dialect whose driver counts only the rows
// an UPDATE altered, not the rows it matched. Saving a value a row already
// holds then reports zero, which reads as the row having gone, so ant checks
// the row is still there before it calls that a conflict. Implementing this
// says nothing about INSERT: a conflict that changed nothing still reports
// zero, and ant keeps reading that as "the conflict did nothing".
type ChangedRowsOnly interface {
	// CountsChangedRows reports whether affected counts changed rows only.
	CountsChangedRows() bool
}

// QuoteWith wraps ident in open and close, doubling any embedded close.
func QuoteWith(ident, open, close string) string {
	return open + strings.ReplaceAll(ident, close, close+close) + close
}

// AppendQuoteWith appends what QuoteWith returns to dst.
func AppendQuoteWith(dst []byte, ident, open, close string) []byte {
	dst = append(dst, open...)

	for {
		i := strings.Index(ident, close)
		if i < 0 || close == "" {
			break
		}

		dst = append(dst, ident[:i]...)
		dst = append(dst, close...)
		dst = append(dst, close...)
		ident = ident[i+len(close):]
	}

	dst = append(dst, ident...)

	return append(dst, close...)
}

// AppendNumbered appends what Numbered returns to dst.
func AppendNumbered(dst []byte, prefix string, n int) []byte {
	return strconv.AppendInt(append(dst, prefix...), int64(n), 10)
}
