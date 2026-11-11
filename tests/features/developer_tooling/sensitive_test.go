package developer_tooling

import (
	"bytes"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/skudra-go/ant"
	"github.com/skudra-go/ant/tests/features/fixture"
)

// Account has a value that must never reach a log.
type Account struct {
	ID       int64
	Name     string
	Password string
}

func (Account) Configure(b *ant.ModelBuilder[Account]) { b.Column(&Accounts.Password).Sensitive() }

// Accounts is Account's probe.
var Accounts = ant.Fields[Account]()

// AccountDB holds it.
type AccountDB struct {
	ant.Context
	Accounts ant.Table[Account]
}

// LogValues puts values in logs, and Sensitive keeps chosen ones out regardless.
func TestSensitiveFields(t *testing.T) {
	const ddl = `CREATE TABLE accounts (id BIGSERIAL PRIMARY KEY, name TEXT NOT NULL, password TEXT NOT NULL)`

	fixture.Each(t, func(t *testing.T, target fixture.Target) {
		ctx := t.Context()
		_, err := target.DB.ExecContext(ctx, ddl)
		require.NoError(t, err)

		var logs bytes.Buffer

		logger := slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
		db := fixture.Open[AccountDB](t, target, ant.WithLogger(logger, ant.LogValues()))

		account := &Account{Name: "visible-name", Password: "hunter2"}
		require.NoError(t, db.Accounts.Insert(ctx, account))

		require.NoError(
			t,
			db.Accounts.Insert(
				ctx,
				&Account{Name: "other", Password: "hunter3"},
				&Account{Name: "third", Password: "hunter4"},
			),
		)

		_, err = db.Accounts.Where(ant.Eq(&Accounts.ID, account.ID)).
			Update(ctx, ant.Set(&Accounts.Password, "hunter5"))
		require.NoError(t, err)
		found, err := db.Accounts.Where(ant.Eq(&Accounts.Password, "hunter5")).First(ctx)
		require.NoError(t, err, "the real value reached the database")
		require.Equal(t, account.ID, found.ID)

		require.Contains(t, logs.String(), "visible-name", "other values still log")
		require.NotContains(t, logs.String(), "hunter")
		require.Contains(t, logs.String(), "[redacted]")
	})
}

// Slim has fewer fields than its table.
type Slim struct {
	ID   int64
	Name string
}

func (Slim) Configure(b *ant.ModelBuilder[Slim]) { b.Table("users") }

// SlimDB holds it.
type SlimDB struct {
	ant.Context
	Users ant.Table[Slim]
}

// WithStrict refuses a result column with no field, which catches a model that
// has fallen behind its table.
func TestStrictScan(t *testing.T) {
	fixture.Each(t, func(t *testing.T, target fixture.Target) {
		ctx := t.Context()
		seed := fixture.Open[fixture.AppDB](t, target)
		fixture.Seed(t, seed)

		loose := fixture.Open[SlimDB](t, target, ant.WithLooseScan())
		rows, err := ant.Raw[Slim](loose, "SELECT * FROM users").All(ctx)
		require.NoError(t, err)
		require.Len(t, rows, 3)

		strict := fixture.Open[SlimDB](t, target, ant.WithStrict())
		_, err = ant.Raw[Slim](strict, "SELECT * FROM users").All(ctx)
		require.ErrorIs(t, err, ant.ErrUnknownColumn)
	})
}
