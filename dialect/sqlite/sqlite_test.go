package sqlite

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	_ "modernc.org/sqlite"

	"github.com/skudra-go/ant"
	"github.com/skudra-go/ant/dialect"
)

// SQLite turns foreign keys off on every new connection, so a data source name
// that does not say otherwise gives a database that accepts rows pointing at
// nothing. Refusing the DSN says so where the mistake is made.
func TestCheckDSNRequiresForeignKeys(t *testing.T) {
	d, ok := Dialect.(dialect.DSNChecker)
	if !ok {
		t.Fatal("the SQLite dialect must check the data source name")
	}

	for _, dsn := range []string{
		"file:app.db",
		"app.db",
		":memory:",
		"file:app.db?_pragma=busy_timeout(5000)&_txlock=immediate",
		"file:app.db?_pragma=foreign_key_check",
		"file:app.db?foreign_keys=1",
	} {
		err := d.CheckDSN(dsn)
		if err == nil {
			t.Fatalf("%q must be refused: SQLite would enforce no foreign key", dsn)
		}

		if !strings.Contains(err.Error(), "foreign_keys(1)") {
			t.Fatalf("%q: the error must name the setting, got %v", dsn, err)
		}
	}

	for _, dsn := range []string{
		"file:app.db?_pragma=foreign_keys(1)",
		":memory:?_pragma=foreign_keys(1)",
		"file:app.db?_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)&_txlock=immediate",
		"file:app.db?_pragma=FOREIGN_KEYS(ON)",
		"file:app.db?_pragma=foreign_keys%3D1",
		"file:app.db?_pragma=%20foreign_keys%20(1)",
		"file:app.db?_pragma=foreign_keys(0)",
		"file:app.db?_foreign_keys=on",
		"file:app.db?_fk=1",
	} {
		if err := d.CheckDSN(dsn); err != nil {
			t.Fatalf("%q must be accepted: %v", dsn, err)
		}
	}

	if err := d.CheckDSN("file:app.db?_pragma=%zz"); err == nil {
		t.Fatal("a query the driver cannot parse must be refused")
	}
}

// The check runs before connecting, so a database without foreign keys is
// refused at Open rather than found out from an orphan row.
func TestOpenRefusesADSNWithoutForeignKeys(t *testing.T) {
	type appDB struct {
		ant.Context
	}

	path := filepath.Join(t.TempDir(), "app.db")

	_, err := ant.Open[appDB](context.Background(), Dialect, "file:"+path)
	if err == nil {
		t.Fatal("Open must refuse a data source name without foreign_keys")
	}

	db, err := ant.Open[appDB](
		context.Background(),
		Dialect,
		"file:"+path+"?_pragma=foreign_keys(1)",
	)
	if err != nil {
		t.Fatal(err)
	}

	defer db.Close()

	var on int
	if err := db.DB().
		QueryRowContext(context.Background(), "PRAGMA foreign_keys").
		Scan(&on); err != nil {
		t.Fatal(err)
	}

	if on != 1 {
		t.Fatalf("foreign_keys is %d, want 1", on)
	}
}
