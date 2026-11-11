package model_definition

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/skudra-go/ant"
	"github.com/skudra-go/ant/tests/features/fixture"
)

// View maps a model to a view, which can be read but not written.
type ActiveUser struct {
	ID   int64
	Name string
}

func (ActiveUser) Configure(b *ant.ModelBuilder[ActiveUser]) { b.View("active_users") }

// Schema places a table in a database schema.
type Invoice2 struct {
	ID   int64
	Note string
}

func (Invoice2) Configure(b *ant.ModelBuilder[Invoice2]) {
	b.Schema("billing")
	b.Table("invoices2")
}

// NoKey drops the convention, for a table whose ID column is not unique.
type LogLine struct {
	ID   int64
	Text string
}

func (LogLine) Configure(b *ant.ModelBuilder[LogLine]) { b.NoKey() }

type ViewsDB struct {
	ant.Context
	Users    ant.Table[fixture.User]
	Active   ant.Table[ActiveUser]
	Invoices ant.Table[Invoice2]
	Lines    ant.Table[LogLine]
}

var (
	ActiveUsers = ant.Fields[ActiveUser]()
	Invoice2s   = ant.Fields[Invoice2]()
)

func TestViewsAndSchemas(t *testing.T) {
	ddl := `
		CREATE VIEW active_users AS SELECT id, name FROM users WHERE age > 18;
		CREATE SCHEMA IF NOT EXISTS billing;
		DROP TABLE IF EXISTS billing.invoices2;
		CREATE TABLE billing.invoices2 (id BIGSERIAL PRIMARY KEY, note TEXT NOT NULL);
		CREATE TABLE log_lines (id BIGINT NOT NULL, text TEXT NOT NULL)`
	fixture.RunTrackedWith(t, ddl, func(t *testing.T, db *ViewsDB) {
		ctx := t.Context()
		require.NoError(t, db.Users.Insert(ctx, &fixture.User{Name: "Ann", Email: "a", Age: 30}))

		t.Run("a view reads like a table", func(t *testing.T) {
			got, err := db.Active.Where(ant.Eq(&ActiveUsers.Name, "Ann")).Single(ctx)
			require.NoError(t, err)
			require.NotZero(t, got.ID)
		})

		t.Run("a view refuses writes", func(t *testing.T) {
			require.ErrorIs(t, db.Active.Insert(ctx, &ActiveUser{Name: "x"}), ant.ErrReadOnly)

			_, err := db.Active.Where(ant.Gt(&ActiveUsers.ID, 0)).
				Update(ctx, ant.Set(&ActiveUsers.Name, "x"))
			require.ErrorIs(t, err, ant.ErrReadOnly)
			_, err = db.Active.Where(ant.Gt(&ActiveUsers.ID, 0)).Delete(ctx)
			require.ErrorIs(t, err, ant.ErrReadOnly)
			_, err = db.Active.InsertMany(ctx, []*ActiveUser{{Name: "x"}})
			require.ErrorIs(t, err, ant.ErrReadOnly)
			_, err = db.Active.Upsert(ctx, &ActiveUser{Name: "x"}, ant.OnConflict(&ActiveUsers.ID))
			require.ErrorIs(t, err, ant.ErrReadOnly)
		})

		t.Run("a tracked view row refuses SaveChanges", func(t *testing.T) {
			session := ant.Session(db)
			got, err := session.Active.First(ctx)
			require.NoError(t, err)

			got.Name = "changed"

			require.ErrorIs(t, session.SaveChanges(ctx), ant.ErrReadOnly)
		})

		t.Run("a schema-qualified table", func(t *testing.T) {
			row := &Invoice2{Note: "x"}
			require.NoError(t, db.Invoices.Insert(ctx, row))
			got, err := db.Invoices.NoTracking().Where(ant.Eq(&Invoice2s.Note, "x")).Single(ctx)
			require.NoError(t, err)
			require.Equal(t, row.ID, got.ID)
			row.Note = "y"

			require.NoError(t, db.SaveChanges(ctx))
			require.NoError(t, db.Invoices.Truncate(ctx))
		})

		t.Run("NoKey allows repeated IDs", func(t *testing.T) {
			require.NoError(
				t,
				db.Lines.Insert(ctx, &LogLine{ID: 1, Text: "a"}, &LogLine{ID: 1, Text: "b"}),
			)

			got, err := db.Lines.All(ctx)
			require.NoError(t, err)
			require.Len(t, got, 2)
		})
	})
}
