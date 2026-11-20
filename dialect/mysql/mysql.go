// Package mysql holds the MySQL and MariaDB dialects. It imports the driver,
// which has no other way to expose its error numbers, so importing this package
// also registers it.
package mysql

import (
	"errors"
	"fmt"

	"github.com/go-sql-driver/mysql"

	"github.com/skudra-go/ant/dialect"
	"github.com/skudra-go/ant/qb"
)

var (
	// Dialect is the MySQL dialect, for version 8.0 or later. Keys come from
	// LastInsertId, as MySQL has no RETURNING.
	Dialect dialect.Dialect = impl{name: "mysql", syntax: &mysqlSyntax}
	// MariaDB is the MariaDB dialect, for version 10.5 or later, which adds
	// INSERT and DELETE ... RETURNING.
	MariaDB dialect.Dialect = impl{name: "mariadb", returning: true, syntax: &mariaSyntax}
)

var (
	mysqlSyntax = selfReads(shared(dialect.ReturningInsert, dialect.ReturningDelete))
	mariaSyntax = noLockOf(shared(dialect.ShareLocks, dialect.LateralJoin))
)

// noLockOf marks MariaDB, which has no FOR UPDATE OF. MySQL has.
func noLockOf(s dialect.Syntax) dialect.Syntax {
	s.NoLockOf = true

	return s
}

// selfReads marks MySQL, which refuses a write whose subquery reads the table
// being written (error 1093). MariaDB allows it.
func selfReads(s dialect.Syntax) dialect.Syntax {
	s.WrapSelfReads = true

	return s
}

func shared(missing ...dialect.Feature) dialect.Syntax {
	return dialect.Syntax{
		Missing: append(
			missing,
			dialect.FullJoin,
			dialect.Merge,
			dialect.ConflictWhere,
			dialect.ReturningUpdate,
			dialect.DatePart,
			dialect.ReadOnlyInTx,
		),
		Unlimited:     "18446744073709551615",
		RetryEndsTx:   true,
		DuplicateKey:  true,
		EmptyValues:   true,
		NoILike:       true,
		NoFilter:      true,
		NoNullsOrder:  true,
		ValuesUnion:   true,
		WrapLimitedIn: true,
		HintInSelect:  true,
		Functions:     map[string]string{"RANDOM": "RAND"},
		NoArrayParams: true,
	}
}

type impl struct {
	name      string
	returning bool
	syntax    *dialect.Syntax
}

func (d impl) Name() string     { return d.name }
func (impl) DriverName() string { return "mysql" }

// CheckDSN refuses a data source name without parseTime. The driver returns
// DATE and TIMESTAMP as []byte unless it is set, and ant maps those columns to
// time.Time, so without it the writes succeed and the first read of such a
// column fails with a scan error that names neither the setting nor the fix.
func (impl) CheckDSN(dsn string) error {
	cfg, err := mysql.ParseDSN(dsn)
	if err != nil {
		return fmt.Errorf("ant: mysql: %w", err)
	}

	if !cfg.ParseTime {
		return errors.New(
			"ant: mysql: the data source name needs parseTime=true; " +
				"without it the driver returns time columns as []byte, " +
				"and every read of one fails to scan",
		)
	}

	return nil
}

// CountsChangedRows reports that the driver counts the rows an UPDATE altered,
// not the rows it matched, which is what ant reads to tell a row left as it was
// from a row that is gone. A data source name that sets clientFoundRows changes
// that, and then the check never fires: the count is already nonzero. That is
// harmless here, but ant does not set the option and cannot see one an
// application set, since the dialect is given no data source name here.
func (impl) CountsChangedRows() bool { return true }

func (impl) Placeholder(n int) string { return "?" }

func (impl) Quote(ident string) string { return dialect.QuoteWith(ident, "`", "`") }

// AppendQuote and AppendPlaceholder are Quote and Placeholder written into a
// buffer, which Render uses.
func (impl) AppendQuote(dst []byte, ident string) []byte {
	return dialect.AppendQuoteWith(dst, ident, "`", "`")
}

func (impl) AppendPlaceholder(dst []byte, _ int) []byte { return append(dst, '?') }

func (d impl) SupportsReturning() bool { return d.returning }

func (d impl) Syntax() *dialect.Syntax { return d.syntax }

func (d impl) Render(stmt qb.Statement) (string, []any, error) {
	return dialect.Render(d, stmt)
}

func init() { dialect.RegisterErrors(state) }

// state translates the driver's error numbers. Its SQLSTATE is too coarse: 23000
// covers every constraint.
func state(err error) string {
	var e *mysql.MySQLError
	if !errors.As(err, &e) {
		return ""
	}

	switch e.Number {
	case 1062:
		return "23505"
	case 1216, 1217, 1451, 1452:
		return "23503"
	case 1048:
		return "23502"
	case 3819, 4025: // MySQL, MariaDB
		return "23514"
	case 1213:
		return "40P01"
	case 1020:
		// MariaDB, from 11.6 with innodb_snapshot_isolation on by default: a
		// row changed since the transaction's snapshot. The transaction is
		// rolled back, as on a serialization failure.
		return "40001"
	}

	return ""
}
