package tests

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/skudra-go/ant"
	"github.com/skudra-go/ant/dialect"
	"github.com/skudra-go/ant/internal/testdb"
)

// streamBlocks reports whether a connection of target can run nothing else while
// Each reads rows from it.
func streamBlocks(target testdb.Target) bool {
	return !dialect.SyntaxOf(target.Dialect).RunsWhileStreaming
}

// requireRefusedInEach checks the error of a statement run inside an Each loop
// on one connection: where the connection cannot run it, ant names the cause.
func requireRefusedInEach(t *testing.T, target testdb.Target, err error) {
	t.Helper()

	if !streamBlocks(target) {
		require.NoError(t, err)

		return
	}

	require.ErrorIs(t, err, ant.ErrQueryInEach)
}

// eachThen streams db's users and runs f once inside the loop, returning f's
// error.
func eachThen(ctx context.Context, db *AppDB, f func(u User) error) error {
	for u, err := range db.Users.Each(ctx) {
		if err != nil {
			return err
		}

		return f(u)
	}

	return nil
}

// Each in a transaction streams on its one connection, so a statement in the
// loop cannot run there on most databases (#163).
func TestEachRefusesStatementsOnItsConnection(t *testing.T) {
	statements := []struct {
		name string
		run  func(ctx context.Context, db *AppDB, u User) error
	}{
		{"query", func(ctx context.Context, db *AppDB, u User) error {
			_, err := db.Posts.Where(ant.Eq(&Posts.UserID, u.ID)).Count(ctx)

			return err
		}},
		{"insert", func(ctx context.Context, db *AppDB, u User) error {
			return db.Posts.Insert(ctx, &Post{UserID: u.ID, Title: "in the loop"})
		}},
		{"save", func(ctx context.Context, db *AppDB, u User) error {
			if err := ant.Add(ctx, db, &Post{UserID: u.ID, Title: "saved"}); err != nil {
				return err
			}

			return db.SaveChanges(ctx)
		}},
		{"nested tx", func(ctx context.Context, db *AppDB, u User) error {
			return db.Tx(ctx, func(inner *AppDB) error {
				return inner.Posts.Insert(ctx, &Post{UserID: u.ID, Title: "nested"})
			})
		}},
		{"nested each", func(ctx context.Context, db *AppDB, u User) error {
			for _, err := range db.Posts.Each(ctx) {
				return err
			}

			return nil
		}},
	}

	testdb.ForEach(t, func(t *testing.T, target testdb.Target) {
		app := openApp(t, target)
		seedUsers(t, app, "Ann", 30, "Bob", 40)

		for _, s := range statements {
			t.Run(s.name, func(t *testing.T) {
				ctx := t.Context()

				var inLoop error

				err := app.Tx(ctx, func(tx *AppDB) error {
					inLoop = eachThen(ctx, tx, func(u User) error { return s.run(ctx, tx, u) })

					// The loop has ended, so the connection is free again.
					_, err := tx.Users.Count(ctx)

					return err
				})
				require.NoError(t, err)
				requireRefusedInEach(t, target, inLoop)
			})
		}
	})
}

// Every way to get a context on one connection refuses a statement inside its
// Each loop, and the pool, which gives the statement a connection of its own,
// does not (#163).
func TestEachRefusesStatementsOnEveryOneConnectionContext(t *testing.T) {
	count := func(ctx context.Context, db *AppDB, u User) error {
		_, err := db.Posts.Where(ant.Eq(&Posts.UserID, u.ID)).Count(ctx)

		return err
	}

	testdb.ForEach(t, func(t *testing.T, target testdb.Target) {
		app := openApp(t, target)
		seedUsers(t, app, "Ann", 30)

		t.Run("pool", func(t *testing.T) {
			ctx := t.Context()

			require.NoError(t, eachThen(ctx, app, func(u User) error { return count(ctx, app, u) }))
		})

		t.Run("begin", func(t *testing.T) {
			ctx := t.Context()

			tx, err := ant.Begin(ctx, app)
			require.NoError(t, err)

			defer tx.Rollback()

			requireRefusedInEach(
				t,
				target,
				eachThen(ctx, tx, func(u User) error { return count(ctx, tx, u) }),
			)

			require.NoError(t, tx.Commit())
		})

		t.Run("savepoint", func(t *testing.T) {
			ctx := t.Context()

			require.NoError(t, app.Tx(ctx, func(tx *AppDB) error {
				inner, err := ant.Begin(ctx, tx)
				if err != nil {
					return err
				}

				requireRefusedInEach(
					t,
					target,
					eachThen(ctx, inner, func(u User) error { return count(ctx, tx, u) }),
				)

				return inner.Commit()
			}))
		})

		t.Run("session in tx", func(t *testing.T) {
			ctx := t.Context()

			require.NoError(t, app.Tx(ctx, func(tx *AppDB) error {
				s := ant.Session(tx)

				requireRefusedInEach(
					t,
					target,
					eachThen(ctx, s, func(u User) error { return count(ctx, tx, u) }),
				)

				return nil
			}))
		})

		t.Run("use tx", func(t *testing.T) {
			ctx := t.Context()

			sqlTx, err := target.DB.BeginTx(ctx, nil)
			require.NoError(t, err)

			defer sqlTx.Rollback()

			tx, err := ant.UseTx(app, sqlTx)
			require.NoError(t, err)

			requireRefusedInEach(
				t,
				target,
				eachThen(ctx, tx, func(u User) error { return count(ctx, tx, u) }),
			)

			require.NoError(t, sqlTx.Commit())
		})

		t.Run("use conn", func(t *testing.T) {
			ctx := t.Context()

			conn, err := target.DB.Conn(ctx)
			require.NoError(t, err)

			defer conn.Close()

			db, err := ant.UseConn(app, conn)
			require.NoError(t, err)

			requireRefusedInEach(
				t,
				target,
				eachThen(ctx, db, func(u User) error { return count(ctx, db, u) }),
			)

			// A transaction on the connection cannot begin either.
			requireRefusedInEach(t, target, eachThen(ctx, db, func(u User) error {
				return db.Tx(ctx, func(tx *AppDB) error { return count(ctx, tx, u) })
			}))

			// A loop that panics lets go of the connection too.
			func() {
				defer func() { require.NotNil(t, recover()) }()

				for range db.Users.Each(ctx) {
					panic("in the loop")
				}
			}()

			_, err = db.Users.Count(ctx)
			require.NoError(t, err)
		})

		t.Run("projection", func(t *testing.T) {
			ctx := t.Context()

			require.NoError(t, app.Tx(ctx, func(tx *AppDB) error {
				var inLoop error

				for _, err := range ant.Project[userName](
					tx.Users.Query(),
					ant.Into(&userNames.Name, &Users.Name),
				).Each(ctx) {
					if err != nil {
						return err
					}

					_, inLoop = tx.Posts.Count(ctx)

					break
				}

				requireRefusedInEach(t, target, inLoop)

				return nil
			}))
		})
	})
}

type userName struct{ Name string }

var userNames = ant.Fields[userName]()

// Where the connection can run statements while it streams, a loop that runs
// them reads every row, and ant refuses none. Elsewhere the first is refused,
// and the transaction goes on. SQL Server looked able to, but only while the
// rows fit in the driver's buffer: past that, the statement waited for good
// (#163).
func TestEachLoopOverManyRows(t *testing.T) {
	testdb.ForEach(t, func(t *testing.T, target testdb.Target) {
		ctx := t.Context()
		app := openApp(t, target)

		users := make([]*User, 300)
		for i := range users {
			users[i] = &User{Name: fmt.Sprint("u", i), Email: fmt.Sprintf("u%d@example.com", i)}
		}

		_, err := app.Users.InsertMany(ctx, users)
		require.NoError(t, err)

		seen := 0

		err = app.Tx(ctx, func(tx *AppDB) error {
			for u, err := range tx.Users.OrderBy(&Users.ID).Each(ctx) {
				if err != nil {
					return err
				}

				seen++

				if err := tx.Posts.Insert(ctx, &Post{UserID: u.ID, Title: "x"}); err != nil {
					return err
				}
			}

			return nil
		})

		if streamBlocks(target) {
			require.ErrorIs(t, err, ant.ErrQueryInEach)
			require.Equal(t, 1, seen)
			require.Zero(t, countRows(t, target.DB, "posts"))

			return
		}

		require.NoError(t, err)
		require.Equal(t, 300, seen)
		require.Equal(t, 300, countRows(t, target.DB, "posts"))
	})
}
