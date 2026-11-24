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
		"file:app.db?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)&_txlock=immediate",
		":memory:?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)&_txlock=immediate",
		"file:app.db?_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)&_txlock=immediate",
		"file:app.db?_pragma=FOREIGN_KEYS(ON)&_pragma=busy_timeout(5000)&_txlock=immediate",
		"file:app.db?_pragma=foreign_keys%3D1&_pragma=busy_timeout(5000)&_txlock=immediate",
		"file:app.db?_pragma=%20foreign_keys%20(1)&_pragma=busy_timeout(5000)&_txlock=immediate",
		"file:app.db?_pragma=foreign_keys(0)&_pragma=busy_timeout(5000)&_txlock=immediate",
		"file:app.db?_foreign_keys=on&_pragma=busy_timeout(5000)&_txlock=immediate",
		"file:app.db?_fk=1&_pragma=busy_timeout(5000)&_txlock=immediate",
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
		"file:"+path+"?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)&_txlock=immediate",
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

func TestCheckDSNRequiresLockSettings(t *testing.T) {
	d := Dialect.(dialect.DSNChecker)

	for _, dsn := range []string{
		"file:app.db?_pragma=foreign_keys(1)",
		"file:app.db?_pragma=foreign_keys(1)&_txlock=immediate",
		"file:app.db?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)",
		"file:app.db?_pragma=foreign_keys(1)&_pragma=busy_timeout&_txlock=immediate",
		"file:app.db?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)&_txlock=",
		"file:app.db?_pragma=foreign_keys(1)&_busy_timeout=5000&_timeout=&_txlock=immediate",
	} {
		if err := d.CheckDSN(dsn); err == nil {
			t.Fatalf("%q must be refused: the lock settings are incomplete", dsn)
		}
	}

	for _, dsn := range []string{
		"file:app.db?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)&_txlock=immediate",
		"file:app.db?_pragma=foreign_keys(1)&_pragma=BUSY_TIMEOUT(5000)&_txlock=immediate",
		"file:app.db?_pragma=foreign_keys(1)&_pragma=busy_timeout=5000&_txlock=exclusive",
		"file:app.db?_pragma=foreign_keys(0)&_pragma=busy_timeout(0)&_txlock=deferred",
		"file:app.db?_pragma=foreign_keys(1)&_busy_timeout=5000&_txlock=immediate",
		"file:app.db?_pragma=foreign_keys(1)&_timeout=0&_txlock=deferred",
		"file:app.db?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)&_timeout=&_txlock=immediate",
	} {
		if err := d.CheckDSN(dsn); err != nil {
			t.Fatalf("%q must be accepted: %v", dsn, err)
		}
	}
}

func TestOpenRequiresLockSettings(t *testing.T) {
	type appDB struct {
		ant.Context
	}

	path := filepath.Join(t.TempDir(), "app.db")

	for _, dsn := range []string{
		"file:" + path + "?_pragma=foreign_keys(1)",
		"file:" + path + "?_pragma=foreign_keys(1)&_pragma=busy_timeout&_txlock=immediate",
		"file:" + path + "?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)&_txlock=",
		"file:" + path + "?_pragma=foreign_keys(1)&_busy_timeout=5000&_timeout=&_txlock=immediate",
	} {
		db, err := ant.Open[appDB](context.Background(), Dialect, dsn)
		if err == nil {
			_ = db.Close()

			t.Errorf("%q must be refused: the lock settings are incomplete", dsn)
		}
	}

	for _, test := range []struct {
		dsn         string
		busyTimeout int
	}{
		{
			dsn:         "file:" + path + "?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)&_txlock=immediate",
			busyTimeout: 5000,
		},
		{
			dsn:         "file:" + path + "?_pragma=foreign_keys(1)&_pragma=busy_timeout(0)&_txlock=deferred",
			busyTimeout: 0,
		},
		{
			dsn:         "file:" + path + "?_pragma=foreign_keys(1)&_busy_timeout=4000&_txlock=immediate",
			busyTimeout: 4000,
		},
		{
			dsn:         "file:" + path + "?_pragma=foreign_keys(1)&_timeout=3000&_txlock=deferred",
			busyTimeout: 3000,
		},
	} {
		db, err := ant.Open[appDB](context.Background(), Dialect, test.dsn)
		if err != nil {
			t.Errorf("%q must open: %v", test.dsn, err)

			continue
		}

		var busyTimeout int
		if err := db.DB().
			QueryRowContext(context.Background(), "PRAGMA busy_timeout").
			Scan(&busyTimeout); err != nil {
			t.Errorf("%q: read busy_timeout: %v", test.dsn, err)
		} else if busyTimeout != test.busyTimeout {
			t.Errorf("%q: busy_timeout is %d, want %d", test.dsn, busyTimeout, test.busyTimeout)
		}

		if err := db.Close(); err != nil {
			t.Error(err)
		}
	}
}

// A writable transaction holds the write lock from its start only when the DSN
// begins it IMMEDIATE or EXCLUSIVE (#313).
func TestBeginLocks(t *testing.T) {
	d := Dialect.(dialect.BeginLocker)

	for dsn, want := range map[string]bool{
		"file:app.db?_pragma=foreign_keys(1)&_txlock=immediate": true,
		"file:app.db?_txlock=EXCLUSIVE&_pragma=foreign_keys(1)": true,
		"file:app.db?_pragma=foreign_keys(1)&_txlock=deferred":  false,
		"file:app.db?_pragma=foreign_keys(1)&_txlock=":          false,
		"file:app.db?_pragma=foreign_keys(1)":                   false,
		"file:app.db":                                           false,
		"file:app.db?_txlock=%zz":                               false,
	} {
		if got := d.BeginLocks(dsn); got != want {
			t.Errorf("BeginLocks(%q) = %v, want %v", dsn, got, want)
		}
	}
}
