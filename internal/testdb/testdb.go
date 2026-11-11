// Package testdb runs ant's integration tests against every database listed in
// ANT_TEST_DATABASES.
package testdb

import (
	"context"
	"crypto/rand"
	"database/sql"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	mysqldriver "github.com/go-sql-driver/mysql" // driver "mysql"
	_ "github.com/jackc/pgx/v5/stdlib"           // driver "pgx"
	_ "github.com/microsoft/go-mssqldb"          // driver "sqlserver"
	_ "modernc.org/sqlite"                       // driver "sqlite"

	"github.com/skudra-go/ant/dialect"
	"github.com/skudra-go/ant/dialect/mysql"
	"github.com/skudra-go/ant/dialect/postgres"
	"github.com/skudra-go/ant/dialect/sqlite"
	"github.com/skudra-go/ant/dialect/sqlserver"
)

const connectTimeout = 10 * time.Second

// Target is one database in the matrix.
type Target struct {
	Name    string
	Dialect dialect.Dialect
	DSN     string
	DB      *sql.DB // set by Connect
}

type spec struct {
	dialect    dialect.Dialect
	env        string
	defaultDSN string
}

// The default DSNs match docker-compose.yml. MySQL and MariaDB connect as root,
// since each test creates a database of its own.
var specs = map[string]spec{
	"postgres": {
		postgres.Dialect,
		"ANT_PG_DSN",
		"postgres://ant:ant@127.0.0.1:55432/ant_test?sslmode=disable",
	},
	"mysql": {
		mysql.Dialect,
		"ANT_MYSQL_DSN",
		"root:ant@tcp(127.0.0.1:53306)/ant_test?parseTime=true",
	},
	"mariadb": {
		mysql.MariaDB,
		"ANT_MARIADB_DSN",
		"root:ant@tcp(127.0.0.1:53307)/ant_test?parseTime=true",
	},
	"sqlite": {sqlite.Dialect, "", ""},
	"sqlserver": {
		sqlserver.Dialect,
		"ANT_MSSQL_DSN",
		"sqlserver://sa:Ant_test_1%21@127.0.0.1:51433?database=master&encrypt=disable",
	},
}

// Targets returns the databases named in ANT_TEST_DATABASES, a comma-separated
// list. An unknown name yields a target whose Connect fails the test.
func Targets() []Target {
	var out []Target

	for name := range strings.SplitSeq(os.Getenv("ANT_TEST_DATABASES"), ",") {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}

		s := specs[name]

		dsn := s.defaultDSN

		if s.env != "" {
			if v := os.Getenv(s.env); v != "" {
				dsn = v
			}
		}

		out = append(out, Target{Name: name, Dialect: s.dialect, DSN: dsn})
	}

	return out
}

// ForEach runs fn once per target, each with a freshly reset schema. It skips
// when no databases are configured, so plain go test needs no database.
func ForEach(t *testing.T, fn func(t *testing.T, target Target)) {
	t.Helper()
	ForOnly(t, nil, fn)
}

// ForOnly is ForEach limited to the named databases, or all of them with no
// names. Each test gets a database of its own, so a top-level test runs in
// parallel with the others unless it called Serial first.
func ForOnly(t *testing.T, names []string, fn func(t *testing.T, target Target)) {
	t.Helper()

	targets := Targets()

	if len(names) > 0 {
		targets = slices.DeleteFunc(
			targets,
			func(target Target) bool { return !slices.Contains(names, target.Name) },
		)
	}

	if len(targets) == 0 {
		t.Skip("no database in ANT_TEST_DATABASES for this test")
	}

	Parallel(t)

	for _, target := range targets {
		t.Run(target.Name, func(t *testing.T) {
			target.Connect(t)
			target.ResetSchema(t)
			fn(t, target)
		})
	}
}

// serial and parallel hold the tests that called Serial and those already made
// parallel, so a test that reaches ForEach twice calls t.Parallel once.
var serial, parallel sync.Map

// Serial keeps t out of the parallel run, for a test that changes what the
// whole process shares: environment variables, slog's default logger, or the
// keys of package secure. go test runs a package's serial tests first, before
// the parallel ones start. Call it before ForEach or ForOnly.
func Serial(t *testing.T) {
	serial.Store(t, true)
	t.Cleanup(func() { serial.Delete(t) })
}

// Parallel runs a top-level test in parallel with the others, unless it called
// Serial. A subtest is left as it is, since its parent may check what it did
// once it returns. Calling it again for the same test does nothing.
func Parallel(t *testing.T) {
	if strings.Contains(t.Name(), "/") {
		return
	}

	if _, ok := serial.Load(t); ok {
		return
	}

	if _, done := parallel.LoadOrStore(t, true); done {
		return
	}

	t.Cleanup(func() { parallel.Delete(t) })
	t.Parallel()
}

// Connect opens a database of the test's own on the target: a temporary file
// on SQLite, a schema on PostgreSQL, and a database elsewhere, each dropped when
// the test ends. A database named in ANT_TEST_DATABASES that cannot be reached
// fails the test, so that a green run means it ran.
func (target *Target) Connect(t *testing.T) {
	t.Helper()

	if target.Dialect == nil {
		t.Fatalf("unknown database %q in ANT_TEST_DATABASES", target.Name)
	}

	if target.Name == "sqlite" {
		path := filepath.Join(t.TempDir(), "ant.db")
		target.DSN = "file:" + path + "?_pragma=foreign_keys(1)"
	}

	db := target.open(t)

	switch target.Name {
	case "postgres":
		db = target.isolate(t, db)
	case "mysql", "mariadb", "sqlserver":
		db = target.ownDatabase(t, db)
	}

	target.DB = db
}

func (target *Target) open(t *testing.T) *sql.DB {
	t.Helper()

	db, err := sql.Open(target.Dialect.DriverName(), target.DSN)
	if err != nil {
		t.Fatalf("%s: %v", target.Name, err)
	}

	t.Cleanup(func() { db.Close() })

	ctx, cancel := context.WithTimeout(t.Context(), connectTimeout)
	defer cancel()

	if err := db.PingContext(ctx); err != nil {
		t.Fatalf("%s unreachable: %v", target.Name, err)
	}

	return db
}

// isolate moves the test into its own PostgreSQL schema, so tests can run in
// parallel against one server.
func (target *Target) isolate(t *testing.T, admin *sql.DB) *sql.DB {
	t.Helper()

	schema := name()

	// admin is used again only to drop the schema, so it keeps no connection
	// open while the test runs.
	admin.SetMaxIdleConns(0)

	if _, err := admin.ExecContext(t.Context(), "CREATE SCHEMA "+schema); err != nil {
		t.Fatalf("create schema: %v", err)
	}

	t.Cleanup(func() {
		if _, err := admin.ExecContext(
			context.Background(),
			"DROP SCHEMA "+schema+" CASCADE",
		); err != nil {
			t.Errorf("drop schema: %v", err)
		}
	})

	target.DSN += "&search_path=" + schema

	return target.open(t)
}

// ownDatabase moves the test into a database of its own on MySQL, MariaDB, or
// SQL Server, so tests can run in parallel against one server. Its user needs
// the right to create databases.
func (target *Target) ownDatabase(t *testing.T, admin *sql.DB) *sql.DB {
	t.Helper()

	database := name()
	drop := "DROP DATABASE " + database

	admin.SetMaxIdleConns(0)

	switch target.Name {
	case "sqlserver":
		// A connection the test left open would keep the database in use.
		drop = "ALTER DATABASE " + database + " SET SINGLE_USER WITH ROLLBACK IMMEDIATE; " + drop

		u, err := url.Parse(target.DSN)
		if err != nil {
			t.Fatalf("%s DSN: %v", target.Name, err)
		}

		query := u.Query()
		query.Set("database", database)
		u.RawQuery = query.Encode()
		target.DSN = u.String()
	default:
		cfg, err := mysqldriver.ParseDSN(target.DSN)
		if err != nil {
			t.Fatalf("%s DSN: %v", target.Name, err)
		}

		cfg.DBName = database
		target.DSN = cfg.FormatDSN()
	}

	if _, err := admin.ExecContext(t.Context(), "CREATE DATABASE "+database); err != nil {
		t.Fatalf("create database (the user needs the right to): %v", err)
	}

	t.Cleanup(func() {
		if _, err := admin.ExecContext(context.Background(), drop); err != nil {
			t.Errorf("drop database: %v", err)
		}
	})

	return target.open(t)
}

// name returns a fresh name for a test's schema or database.
func name() string { return "ant_" + strings.ToLower(rand.Text()) }

// ResetSchema recreates the fixture tables.
func (target *Target) ResetSchema(t *testing.T) {
	t.Helper()

	for _, stmt := range schema(target.Name) {
		if _, err := target.DB.ExecContext(t.Context(), stmt); err != nil {
			t.Fatalf("reset schema: %s: %v", stmt, err)
		}
	}
}
