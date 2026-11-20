package tests

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/skudra-go/ant"
	"github.com/skudra-go/ant/internal/testdb"
)

type Logged struct {
	Kind   string
	Status string `ant:"default='new'"`
}

func (Logged) Configure(b *ant.ModelBuilder[Logged]) {
	b.Table("conf_logged")
	b.NoKey()
}

type Coded struct {
	Code string
	Name string
	Tier int `ant:"default=3"`
}

func (Coded) Configure(b *ant.ModelBuilder[Coded]) {
	b.Table("conf_coded")
	b.Key(&Codeds.Code).NotGenerated()
	b.Column(&Codeds.Code).Default("'k'")
}

var (
	Loggeds = ant.Fields[Logged]()
	Codeds  = ant.Fields[Coded]()
)

type ReadBackDB struct {
	ant.Context
	Logged ant.Table[Logged]
	Coded  ant.Table[Coded]
}

func openReadBack(t *testing.T, target testdb.Target, ddl ...string) *ReadBackDB {
	t.Helper()

	ctx := t.Context()

	for _, stmt := range ddl {
		_, err := target.DB.ExecContext(ctx, stmt)
		require.NoError(t, err)
	}

	t.Cleanup(func() {
		for _, table := range []string{"conf_logged", "conf_coded"} {
			_, _ = target.DB.ExecContext(context.Background(), "DROP TABLE "+table)
		}
	})

	db, err := ant.Open[ReadBackDB](ctx, target.Dialect, target.DSN)
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })

	return db
}

// Without RETURNING, what the database fills is read back by the row's key. A
// model with no key has none to find its row by, and the read matched every row,
// so a new row took the first stored row's values (#109). A key the database
// fills, other than one LastInsertId reports, can't be learned there at all.
func TestConformanceReadBackNeedsAKey(t *testing.T) {
	testdb.ForEach(t, func(t *testing.T, target testdb.Target) {
		ctx := t.Context()
		db := openReadBack(
			t,
			target,
			`CREATE TABLE conf_logged (kind VARCHAR(20) NOT NULL,
				status VARCHAR(20) NOT NULL DEFAULT 'new')`,
			`CREATE TABLE conf_coded (code VARCHAR(20) NOT NULL DEFAULT 'k' PRIMARY KEY,
				name VARCHAR(20) NOT NULL, tier INT NOT NULL DEFAULT 3)`,
		)

		_, err := target.DB.ExecContext(
			ctx,
			`INSERT INTO conf_logged (kind, status) VALUES ('seed', 'old')`,
		)
		require.NoError(t, err)

		t.Run("a model with no key", func(t *testing.T) {
			e := &Logged{Kind: "x"}
			require.NoError(t, db.Logged.Insert(ctx, e))

			stored, err := db.Logged.Where(ant.Eq(&Loggeds.Kind, "x")).Single(ctx)
			require.NoError(t, err)
			require.Equal(t, "new", stored.Status)

			if target.Dialect.SupportsReturning() {
				require.Equal(t, "new", e.Status)
			} else {
				require.Empty(t, e.Status, "another row's values are never read into it")
			}
		})

		t.Run("a key the database fills", func(t *testing.T) {
			e := &Coded{Name: "a"}
			err := db.Coded.Insert(ctx, e)

			if target.Dialect.SupportsReturning() {
				require.NoError(t, err)
				require.Equal(t, "k", e.Code)
				require.Equal(t, 3, e.Tier)

				return
			}

			require.ErrorIs(t, err, ant.ErrUnsupported)
			require.ErrorContains(t, err, "Coded.Code")

			n, err := db.Coded.Count(ctx)
			require.NoError(t, err)
			require.Zero(t, n, "refused before it was written")
		})
	})
}
