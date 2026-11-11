package migrations

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/skudra-go/ant"
	"github.com/skudra-go/ant/migrate"
	"github.com/skudra-go/ant/tests/features/fixture"
)

// Each test here has its own context type, because registrations are global and
// per context, as they would be in a program.

type GoDB struct {
	ant.Context
	Accounts ant.Table[V1Account]
}

type FailingDB struct {
	ant.Context
	Accounts ant.Table[V1Account]
}

type BatchDB struct {
	ant.Context
	Accounts ant.Table[V1Account]
}

type SeedDB struct {
	ant.Context
	Accounts ant.Table[Hooked]
}

// Hooked counts its inserts, to show hooks stay quiet while seeding.
type Hooked struct {
	ant.Base
	Name  string
	Email string `ant:"unique"`
}

func (Hooked) Configure(b *ant.ModelBuilder[Hooked]) { b.Table("accounts") }

// hookRuns is atomic, since other tests seed accounts in parallel. TestSeed,
// which reads it, runs alone.
var hookRuns atomic.Int64

// BeforeInsert is a model hook.
func (*Hooked) BeforeInsert(context.Context) error {
	hookRuns.Add(1)

	return nil
}

func init() {
	migrate.Register[GoDB]("0002_shout_names", func(ctx context.Context, tx *GoDB) error {
		accounts, err := tx.Accounts.All(ctx)
		if err != nil {
			return err
		}

		for _, a := range accounts {
			if _, err := ant.Raw[any](
				tx,
				"UPDATE accounts SET name = upper(name) WHERE id = ?",
				a.ID,
			).Exec(ctx); err != nil {
				return err
			}
		}

		return nil
	})

	migrate.Register[FailingDB]("0002_breaks", func(ctx context.Context, tx *FailingDB) error {
		if _, err := ant.Raw[any](
			tx,
			"UPDATE accounts SET name = 'changed'",
		).Exec(ctx); err != nil {
			return err
		}

		return errors.New("something went wrong")
	})

	migrate.Register[BatchDB](
		"0002_in_batches",
		func(ctx context.Context, db *BatchDB) error {
			_, err := ant.Raw[any](db, "UPDATE accounts SET name = 'batched'").Exec(ctx)

			return err
		},
		migrate.NoTransaction,
	)

	migrate.Seed[SeedDB]("accounts", func(ctx context.Context, db *SeedDB) error {
		return db.Accounts.Insert(ctx, &Hooked{Name: "Seed", Email: "seed@example.com"})
	})

	migrate.Seed[SeedDB]("broken", func(ctx context.Context, db *SeedDB) error {
		if err := db.Accounts.Insert(
			ctx,
			&Hooked{Name: "Half", Email: "half@example.com"},
		); err != nil {
			return err
		}

		return errors.New("seed failed")
	})
}

// Register adds a migration written in Go, in sequence with the SQL files, for
// data changes SQL cannot express. It gets a context bound to its transaction.
func TestGoMigration(t *testing.T) {
	each(t, func(t *testing.T, p *project) {
		must[V1DB](p, "diff", "start")
		must[GoDB](p, "up", "--to", "0001")

		p.sql(
			"INSERT INTO accounts (created_at, updated_at, name, email) VALUES (now(), now(), 'ada', 'ada@example.com')",
		)

		t.Run("it runs in order with the files", func(t *testing.T) {
			require.Contains(t, must[GoDB](p, "status"), "0002_shout_names")
			require.Contains(t, must[GoDB](p, "up"), "Applied 0002_shout_names")
			require.Equal(t, "ADA", scalar[string](p, "SELECT name FROM accounts"))
		})

		t.Run("and only once", func(t *testing.T) {
			require.Contains(t, must[GoDB](p, "up"), "Nothing to apply")
		})

		t.Run("new files are numbered after it", func(t *testing.T) {
			must[GoDB](p, "diff", "--empty", "next")
			require.True(t, p.exists("versions/0003_next.up.sql"))
		})

		t.Run("a program that forgot to import it is told", func(t *testing.T) {
			require.NoError(t, removeFile(p, "versions/0003_next.up.sql"))
			_, err := run[V1DB](p, "up")
			require.ErrorIs(t, err, migrate.ErrMissing)
			require.ErrorContains(t, err, "register its Go migration")
		})
	})
}

// An error rolls everything back, so a data migration is all or nothing.
func TestGoMigrationFailure(t *testing.T) {
	each(t, func(t *testing.T, p *project) {
		must[V1DB](p, "diff", "start")
		must[FailingDB](p, "up", "--to", "0001")

		p.sql(
			"INSERT INTO accounts (created_at, updated_at, name, email) VALUES (now(), now(), 'ada', 'ada@example.com')",
		)

		_, err := run[FailingDB](p, "up")
		require.ErrorContains(t, err, "something went wrong")
		require.Equal(t, "ada", scalar[string](p, "SELECT name FROM accounts"))
		require.Equal(t, 1, scalar[int](p, "SELECT count(*) FROM ant_migrations"))
	})
}

// migrate.NoTransaction suits work too large for one transaction.
func TestGoMigrationWithoutATransaction(t *testing.T) {
	each(t, func(t *testing.T, p *project) {
		must[V1DB](p, "diff", "start")
		must[BatchDB](p, "up", "--to", "0001")

		p.sql(
			"INSERT INTO accounts (created_at, updated_at, name, email) VALUES (now(), now(), 'ada', 'ada@example.com')",
		)

		must[BatchDB](p, "up")
		require.Equal(t, "batched", scalar[string](p, "SELECT name FROM accounts"))
	})
}

// seed fills a database with data, separately from migrations. Hooks stay quiet,
// since they usually do things like sending mail.
func TestSeed(t *testing.T) {
	fixture.Serial(t) // it counts hooks that other tests' seeds run too

	each(t, func(t *testing.T, p *project) {
		must[SeedDB](p, "diff", "start")
		must[SeedDB](p, "up")

		t.Run("named seeders run alone, with hooks muted", func(t *testing.T) {
			hookRuns.Store(0)

			require.Contains(t, must[SeedDB](p, "seed", "accounts"), "Seeded accounts")
			require.Equal(t, 1, scalar[int](p, "SELECT count(*) FROM accounts"))
			require.Zero(t, hookRuns.Load())
		})

		t.Run("Config.SeedHooks turns hooks on", func(t *testing.T) {
			p.sql("DELETE FROM accounts")
			p.config.SeedHooks = true
			must[SeedDB](p, "seed", "accounts")
			require.EqualValues(t, 1, hookRuns.Load())

			p.config.SeedHooks = false
		})

		t.Run("a failing seeder undoes only itself", func(t *testing.T) {
			_, err := run[SeedDB](p, "seed", "broken")
			require.ErrorContains(t, err, "seed broken")
			require.Equal(t, 1, scalar[int](p, "SELECT count(*) FROM accounts"))
		})

		t.Run("an unknown name is an error", func(t *testing.T) {
			_, err := run[SeedDB](p, "seed", "nope")
			require.ErrorContains(t, err, `no seeder named "nope"`)
		})
	})
}
