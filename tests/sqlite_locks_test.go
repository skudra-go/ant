package tests

import (
	"database/sql"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/skudra-go/ant"
	"github.com/skudra-go/ant/internal/testdb"
)

// On SQLite, a writable transaction from a DSN with _txlock=immediate or
// exclusive holds the database's write lock from its first statement, so
// ForUpdate and ForShare inside it add nothing and are accepted. Wherever that
// lock is not certain they are still refused (#313).
func TestSQLiteRowLocksInImmediateTx(t *testing.T) {
	testdb.ForOnly(t, []string{"sqlite"}, func(t *testing.T, target testdb.Target) {
		ctx := t.Context()

		for _, mode := range []string{"immediate", "exclusive", "IMMEDIATE"} {
			t.Run(mode, func(t *testing.T) {
				dsn := strings.Replace(target.DSN, "_txlock=immediate", "_txlock="+mode, 1)
				app, err := ant.Open[AppDB](ctx, target.Dialect, dsn, ant.WithTracking(true))
				require.NoError(t, err)
				t.Cleanup(func() { app.Close() })

				prefix := mode
				users := seedUsers(t, app, prefix+"-from", 30, prefix+"-to", 25)
				ids := []int64{users[0].ID, users[1].ID}

				var counts []int

				err = app.Tx(ctx, func(tx *AppDB) error {
					locked, err := tx.Users.Where(ant.In(&Users.ID, ids...)).
						OrderBy(&Users.ID).
						ForUpdate().
						All(ctx)
					if err != nil {
						return err
					}

					locked[0].Age -= 5
					locked[1].Age += 5

					shared, err := tx.Users.Where(ant.Eq(&Users.ID, ids[0])).ForShare().All(ctx)
					if err != nil {
						return err
					}

					joined, err := tx.Posts.Where(ant.Eq(&Posts.UserID, ids[0])).
						IncludeJoined(&Posts.User).
						ForUpdate().
						All(ctx)
					if err != nil {
						return err
					}

					counts = []int{len(locked), len(shared), len(joined)}

					text, _, err := tx.Users.Where(ant.In(&Users.ID, ids...)).
						ForUpdate().
						ToSQLContext(ctx)
					if err != nil {
						return err
					}

					if strings.Contains(text, "FOR UPDATE") {
						return errors.New("ToSQLContext kept the lock: " + text)
					}

					if _, err := tx.Users.Where(ant.In(&Users.ID, ids...)).
						ForUpdate().
						Explain(ctx); err != nil {
						return err
					}

					return tx.SaveChanges(ctx)
				})
				require.NoError(t, err)
				require.Equal(t, []int{2, 1, 0}, counts)

				from, err := app.Users.NoTracking().Where(ant.Eq(&Users.ID, ids[0])).First(ctx)
				require.NoError(t, err)
				require.Equal(t, 25, from.Age)
			})
		}

		t.Run("nested and in a session", func(t *testing.T) {
			app := openApp(t, target)
			alice := seedUsers(t, app, "nested-alice", 30)[0]

			err := ant.Session(app).Tx(ctx, func(tx *AppDB) error {
				return tx.Tx(ctx, func(inner *AppDB) error {
					_, err := inner.Users.Where(ant.Eq(&Users.ID, alice.ID)).ForUpdate().All(ctx)

					return err
				})
			})
			require.NoError(t, err, "a savepoint is in the transaction that holds the lock")
		})

		t.Run("refused where the lock is not certain", func(t *testing.T) {
			app := openApp(t, target)

			deferred, err := ant.Open[AppDB](
				ctx,
				target.Dialect,
				strings.Replace(target.DSN, "_txlock=immediate", "_txlock=deferred", 1),
			)
			require.NoError(t, err)
			t.Cleanup(func() { deferred.Close() })

			_, err = app.Users.Query().ForUpdate().All(ctx)
			require.ErrorIs(t, err, ant.ErrUnsupported, "outside a transaction")

			err = app.Tx(
				ctx,
				func(tx *AppDB) error {
					_, err := tx.Users.Query().ForUpdate().All(ctx)

					return err
				},
				ant.ReadOnly(),
			)
			require.ErrorIs(t, err, ant.ErrUnsupported, "a read-only transaction begins deferred")

			err = deferred.Tx(ctx, func(tx *AppDB) error {
				_, err := tx.Users.Query().ForUpdate().All(ctx)

				return err
			})
			require.ErrorIs(t, err, ant.ErrUnsupported, "with _txlock=deferred")

			pool, err := sql.Open("sqlite", target.DSN)
			require.NoError(t, err)
			t.Cleanup(func() { pool.Close() })

			wrapped, err := ant.Wrap[AppDB](target.Dialect, pool)
			require.NoError(t, err)

			err = wrapped.Tx(ctx, func(tx *AppDB) error {
				_, err := tx.Users.Query().ForUpdate().All(ctx)

				return err
			})
			require.ErrorIs(
				t,
				err,
				ant.ErrUnsupported,
				"on a pool ant did not open, whose DSN it cannot read",
			)

			sqlTx, err := pool.BeginTx(ctx, nil)
			require.NoError(t, err)
			t.Cleanup(func() { _ = sqlTx.Rollback() })

			used, err := ant.UseTx(app, sqlTx)
			require.NoError(t, err)

			_, err = used.Users.Query().ForUpdate().All(ctx)
			require.ErrorIs(t, err, ant.ErrUnsupported, "in a transaction begun elsewhere")
		})
	})
}
