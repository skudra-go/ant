package mysql

import (
	"fmt"
	"testing"

	"github.com/go-sql-driver/mysql"
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
