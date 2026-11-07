// Package sqlserver is the SQL Server dialect.
package sqlserver

import (
	"errors"
	"strings"

	"github.com/skudra-go/ant/dialect"
	"github.com/skudra-go/ant/qb"
)

// Dialect is the SQL Server dialect, for version 2019 or later. Import a driver
// registered as "sqlserver". Rows return through OUTPUT, which SQL Server refuses
// on a table with triggers.
var Dialect dialect.Dialect = impl{}

type impl struct{}

var syntax = dialect.Syntax{
	Missing: []dialect.Feature{
		dialect.LateralJoin,
		dialect.RowLocks,
		dialect.OnConflict,
		dialect.DateCast,
		dialect.DatePart,
		dialect.SubqueryWith,
	},
	OffsetFetch:        true,
	Output:             true,
	NoILike:            true,
	NoFilter:           true,
	NoNullsOrder:       true,
	NoBooleans:         true,
	NoRowIn:            true,
	MergeSemicolon:     true,
	MergeHoldLock:      true,
	TransactSavepoints: true,
	UTCTimes:           true,
	Functions:          map[string]string{"LENGTH": "LEN", "RANDOM": "NEWID"},
	MaxParameters:      2000, // 2100, less what the driver's sp_executesql call uses
}

func (impl) Name() string       { return "sqlserver" }
func (impl) DriverName() string { return "sqlserver" }

func (impl) Placeholder(n int) string { return dialect.Numbered("@p", n) }

func (impl) Quote(ident string) string { return dialect.QuoteWith(ident, "[", "]") }

// SupportsReturning is true because OUTPUT does the same job.
func (impl) SupportsReturning() bool { return true }

func (impl) Syntax() *dialect.Syntax { return &syntax }

func (d impl) Render(stmt qb.Statement) (string, []any, error) {
	return dialect.Render(d, stmt)
}

func init() { dialect.RegisterErrors(state) }

// state translates SQL Server's error numbers, read through an interface so that
// ant does not import the driver. Number 547 covers two kinds of constraint.
func state(err error) string {
	var e interface {
		SQLErrorNumber() int32
		SQLErrorMessage() string
	}

	if !errors.As(err, &e) {
		return ""
	}

	switch e.SQLErrorNumber() {
	case 2601, 2627:
		return "23505"
	case 547:
		if strings.Contains(e.SQLErrorMessage(), "CHECK") {
			return "23514"
		}

		return "23503"
	case 515:
		return "23502"
	case 1205:
		return "40P01"
	}

	return ""
}
