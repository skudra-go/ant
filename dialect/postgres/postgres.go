// Package postgres is the PostgreSQL dialect.
package postgres

import (
	"github.com/skudra-go/ant/dialect"
	"github.com/skudra-go/ant/qb"
)

// Dialect is the PostgreSQL dialect. Import a driver registered as "pgx". It also
// implements schema.Dialect, which is what migrations need.
var Dialect dialect.Dialect = impl{}

type impl struct{}

func (impl) Name() string       { return "postgres" }
func (impl) DriverName() string { return "pgx" }

func (impl) Placeholder(n int) string { return dialect.Numbered("$", n) }

func (impl) Quote(ident string) string { return dialect.QuoteWith(ident, "\"", "\"") }

// AppendQuote and AppendPlaceholder are Quote and Placeholder written into a
// buffer, which Render uses.
func (impl) AppendQuote(dst []byte, ident string) []byte {
	return dialect.AppendQuoteWith(dst, ident, "\"", "\"")
}

func (impl) AppendPlaceholder(dst []byte, n int) []byte {
	return dialect.AppendNumbered(dst, "$", n)
}

func (impl) SupportsReturning() bool { return true }

func (impl) Render(stmt qb.Statement) (string, []any, error) {
	return dialect.Render(impl{}, stmt)
}
