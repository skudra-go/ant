// Package sqlite is the SQLite dialect.
package sqlite

import (
	"errors"

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
	},
	Unlimited:     "-1",
	NoILike:       true,
	NoRowIn:       true,
	BareSets:      true,
	ValuesUnion:   true,
	UTCTimes:      true,
	MaxParameters: 32766,
	NoArrayParams: true,
}

func (impl) Name() string       { return "sqlite" }
func (impl) DriverName() string { return "sqlite" }

func (impl) Placeholder(n int) string { return "?" }

func (impl) Quote(ident string) string { return dialect.QuoteWith(ident, "\"", "\"") }

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
