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

type Line struct {
	ID      int64
	Qty     int
	Price   int
	Total   int `ant:"readonly"`
	Touched int `ant:"readonly,token"`
}

func (Line) Configure(b *ant.ModelBuilder[Line]) {
	b.Table("conf_lines")
	b.Key(&Lines.ID).NotGenerated()
}

var Lines = ant.Fields[Line]()

type LinesDB struct {
	ant.Context
	Lines ant.Table[Line]
}

// linesDDL makes a table whose total the database computes, and whose touched
// column a trigger moves on with every update, in each database's words.
func linesDDL(target testdb.Target) []string {
	table := func(total string) string {
		return `CREATE TABLE conf_lines (id BIGINT PRIMARY KEY, qty INT NOT NULL,
			price INT NOT NULL, total ` + total + `, touched INT NOT NULL DEFAULT 0)`
	}

	switch target.Name {
	case "postgres":
		return []string{
			table("INT GENERATED ALWAYS AS (qty * price) STORED"),
			`CREATE FUNCTION conf_lines_touch() RETURNS trigger LANGUAGE plpgsql AS $$
				BEGIN NEW.touched := OLD.touched + 1; RETURN NEW; END $$`,
			`CREATE TRIGGER conf_lines_touch BEFORE UPDATE ON conf_lines
				FOR EACH ROW EXECUTE FUNCTION conf_lines_touch()`,
		}
	case "mysql", "mariadb":
		return []string{
			table("INT GENERATED ALWAYS AS (qty * price) STORED"),
			`CREATE TRIGGER conf_lines_touch BEFORE UPDATE ON conf_lines
				FOR EACH ROW SET NEW.touched = OLD.touched + 1`,
		}
	case "sqlite":
		return []string{
			table("INT GENERATED ALWAYS AS (qty * price) STORED"),
			`CREATE TRIGGER conf_lines_touch AFTER UPDATE OF qty, price ON conf_lines
				BEGIN UPDATE conf_lines SET touched = OLD.touched + 1 WHERE id = NEW.id; END`,
		}
	case "sqlserver":
		return []string{
			table("AS (qty * price) PERSISTED"),
			`CREATE TRIGGER conf_lines_touch ON conf_lines AFTER UPDATE AS
				UPDATE l SET touched = d.touched + 1
				FROM conf_lines l JOIN deleted d ON d.id = l.id`,
		}
	}

	return nil
}

// After an update, what the database fills, a generated column or one a trigger
// keeps, is read back as it is after an insert. A trigger-kept token stayed at
// the loaded value, so the entity's second save was refused as a conflict though
// no one else had written the row (#156).
func TestConformanceUpdateReadsBackWhatTheDatabaseFills(t *testing.T) {
	testdb.ForEach(t, func(t *testing.T, target testdb.Target) {
		ctx := t.Context()

		for _, stmt := range linesDDL(target) {
			_, err := target.DB.ExecContext(ctx, stmt)
			require.NoError(t, err)
		}

		t.Cleanup(func() {
			_, _ = target.DB.ExecContext(context.Background(), "DROP TABLE conf_lines")

			if target.Name == "postgres" {
				_, _ = target.DB.ExecContext(context.Background(), "DROP FUNCTION conf_lines_touch")
			}
		})

		root, err := ant.Open[LinesDB](ctx, target.Dialect, target.DSN)
		require.NoError(t, err)
		t.Cleanup(func() { root.Close() })

		db := ant.Session(root)
		line := &Line{ID: 1, Qty: 2, Price: 3}
		require.NoError(t, db.Lines.Insert(ctx, line))
		require.Equal(t, 6, line.Total)
		require.Zero(t, line.Touched)

		line.Qty = 5

		require.NoError(t, db.SaveChanges(ctx))
		require.Equal(t, 15, line.Total)
		require.Equal(t, 1, line.Touched)

		changed, err := db.HasChanges(ctx)
		require.NoError(t, err)
		require.False(t, changed, "what was read back is what the row holds")

		line.Price = 4

		require.NoError(t, db.SaveChanges(ctx), "the token is the one the row holds")
		require.Equal(t, 20, line.Total)
		require.Equal(t, 2, line.Touched)

		stored, err := root.Lines.Find(ctx, int64(1))
		require.NoError(t, err)
		require.Equal(t, *line, *stored)

		t.Run("a write by someone else is still a conflict", func(t *testing.T) {
			_, err := root.Lines.Where(ant.Eq(&Lines.ID, int64(1))).
				Update(ctx, ant.Set(&Lines.Qty, 7))
			require.NoError(t, err)

			line.Qty = 8

			require.ErrorIs(t, db.SaveChanges(ctx), ant.ErrConcurrency)
			require.Equal(t, 20, line.Total, "a refused save reads nothing back")
		})
	})
}
