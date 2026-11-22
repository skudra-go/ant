package mysql

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/go-sql-driver/mysql"

	"github.com/skudra-go/ant"
	"github.com/skudra-go/ant/dialect"
)

// The driver's error numbers become the SQLSTATE codes ant classifies errors by.
// MariaDB reports a row changed since a transaction's snapshot as 1020, and
// rolls the transaction back, so it is retried like a serialization failure.
func TestState(t *testing.T) {
	cases := map[uint16]string{
		1062: "23505",
		1452: "23503",
		1048: "23502",
		4025: "23514",
		1213: "40P01",
		1146: "42P01",
		1020: "40001",
		1064: "",
	}

	for number, want := range cases {
		err := fmt.Errorf("wrapped: %w", &mysql.MySQLError{Number: number})

		if got := state(err); got != want {
			t.Errorf("error %d: state %q, want %q", number, got, want)
		}
	}
}

// Without parseTime the driver hands DATE and TIMESTAMP back as []byte, so the
// first read of a time column fails with a scan error that names neither the
// setting nor the way out. Refusing the DSN says so where the mistake is made.
func TestCheckDSNRequiresParseTime(t *testing.T) {
	d, ok := Dialect.(dialect.DSNChecker)
	if !ok {
		t.Fatal("the MySQL dialect must check the data source name")
	}

	for _, dsn := range []string{
		"user:pw@tcp(host:3306)/db",
		"user:pw@tcp(host:3306)/db?parseTime=false",
	} {
		err := d.CheckDSN(dsn)
		if err == nil {
			t.Fatalf("%q must be refused: without parseTime every read of a time column fails", dsn)
		}

		if !strings.Contains(err.Error(), "parseTime") {
			t.Fatalf("%q: the error must name the setting, got %v", dsn, err)
		}
	}

	for _, dsn := range []string{
		"user:pw@tcp(host:3306)/db?parseTime=true",
		"user:pw@tcp(host:3306)/db?parseTime=1&loc=UTC",
	} {
		if err := d.CheckDSN(dsn); err != nil {
			t.Fatalf("%q must be accepted: %v", dsn, err)
		}
	}
}

// The check runs before connecting, so a DSN the driver could not scan time
// columns from is refused at Open rather than at the first query.
func TestOpenRefusesADSWithoutParseTime(t *testing.T) {
	type appDB struct {
		ant.Context
	}

	_, err := ant.Open[appDB](
		context.Background(),
		Dialect,
		"user:pw@tcp(127.0.0.1:1)/db",
	)
	if err == nil {
		t.Fatal("Open must refuse a data source name without parseTime")
	}

	if !strings.Contains(err.Error(), "parseTime") {
		t.Fatalf("the error must name the setting, got %v", err)
	}
}
