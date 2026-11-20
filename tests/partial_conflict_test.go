package tests

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/skudra-go/ant"
	"github.com/skudra-go/ant/dialect"
	"github.com/skudra-go/ant/internal/testdb"
)

type Acct struct {
	ID        int64
	Email     string
	Name      string
	DeletedAt ant.DeletedAt
}

func (Acct) Configure(b *ant.ModelBuilder[Acct]) {
	b.Table("conf_accts")
	b.Index(&Accts.Email).Unique().Where("deleted_at IS NULL")
}

var Accts = ant.Fields[Acct]()

type AcctsDB struct {
	ant.Context
	Accts ant.Table[Acct]
}

// A soft-deleted model keeps its emails unique with a partial index, so a
// deleted one can be used again. PostgreSQL and SQLite match a partial index to
// ON CONFLICT only when the statement repeats its predicate, so an upsert by
// email failed with "no unique or exclusion constraint matching" (#150).
func TestConformanceUpsertOnAPartialUniqueIndex(t *testing.T) {
	testdb.ForEach(t, func(t *testing.T, target testdb.Target) {
		if !has(target, dialect.OnConflict) || dialect.SyntaxOf(target.Dialect).DuplicateKey {
			t.Skip("no conflict target on " + target.Name)
		}

		ctx := t.Context()
		key := "BIGSERIAL PRIMARY KEY"

		if target.Name == "sqlite" {
			key = "INTEGER PRIMARY KEY AUTOINCREMENT"
		}

		for _, stmt := range []string{
			`CREATE TABLE conf_accts (id ` + key + `, email VARCHAR(50) NOT NULL,
				name VARCHAR(50) NOT NULL, deleted_at TIMESTAMP NULL)`,
			`CREATE UNIQUE INDEX conf_accts_email_live ON conf_accts (email)
				WHERE deleted_at IS NULL`,
		} {
			_, err := target.DB.ExecContext(ctx, stmt)
			require.NoError(t, err)
		}

		t.Cleanup(func() {
			_, _ = target.DB.ExecContext(context.Background(), "DROP TABLE conf_accts")
		})

		db, err := ant.Open[AcctsDB](ctx, target.Dialect, target.DSN)
		require.NoError(t, err)
		t.Cleanup(func() { db.Close() })

		byEmail := ant.OnConflict(&Accts.Email)

		_, err = db.Accts.Upsert(ctx, &Acct{Email: "a@x", Name: "one"}, byEmail)
		require.NoError(t, err)

		_, err = db.Accts.Where(ant.Eq(&Accts.Email, "a@x")).Delete(ctx)
		require.NoError(t, err)

		again := &Acct{Email: "a@x", Name: "two"}
		_, err = db.Accts.Upsert(ctx, again, byEmail)
		require.NoError(t, err)

		_, err = db.Accts.Upsert(ctx, &Acct{Email: "a@x", Name: "three"}, byEmail)
		require.NoError(t, err)

		live, err := db.Accts.All(ctx)
		require.NoError(t, err)
		require.Len(t, live, 1, "the deleted row stays deleted")
		require.Equal(t, "three", live[0].Name)
		require.Equal(t, again.ID, live[0].ID)

		all, err := db.Accts.WithDeleted().Count(ctx)
		require.NoError(t, err)
		require.EqualValues(t, 2, all)

		t.Run("DoNothing", func(t *testing.T) {
			inserted, err := db.Accts.Upsert(
				ctx,
				&Acct{Email: "a@x", Name: "four"},
				ant.OnConflict(&Accts.Email).DoNothing(),
			)
			require.NoError(t, err)
			require.False(t, inserted)
		})

		t.Run("InsertMany", func(t *testing.T) {
			n, err := db.Accts.InsertMany(
				ctx,
				[]*Acct{{Email: "a@x", Name: "five"}, {Email: "b@x", Name: "b"}},
				ant.WithConflict(byEmail),
			)
			require.NoError(t, err)
			require.EqualValues(t, 2, n)

			live, err := db.Accts.OrderBy(&Accts.Email).All(ctx)
			require.NoError(t, err)
			require.Len(t, live, 2)
			require.Equal(t, "five", live[0].Name)
		})
	})
}
