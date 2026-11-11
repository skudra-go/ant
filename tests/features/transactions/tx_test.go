package transactions

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/skudra-go/ant"
	"github.com/skudra-go/ant/tests/features/fixture"
)

type (
	User  = fixture.User
	AppDB = fixture.AppDB
)

var Users = fixture.Users

func count(t *testing.T, db *AppDB) int {
	t.Helper()
	n, err := ant.Raw[int](db, `SELECT count(*) FROM users`).First(t.Context())
	require.NoError(t, err)

	return *n
}

func user(name string) *User { return &User{Name: name, Email: name + "@example.com"} }

var errStop = errors.New("stop")

// Tx commits if the function returns nil, and rolls back otherwise.
func TestTx(t *testing.T) {
	fixture.RunTracked(t, func(t *testing.T, db *AppDB) {
		ctx := t.Context()

		t.Run("commit", func(t *testing.T) {
			err := db.Tx(ctx, func(tx *AppDB) error {
				return tx.Users.Insert(ctx, user("committed"))
			})
			require.NoError(t, err)
			require.Equal(t, 1, count(t, db))
		})

		t.Run("an error rolls back and comes through", func(t *testing.T) {
			err := db.Tx(ctx, func(tx *AppDB) error {
				if err := tx.Users.Insert(ctx, user("rolled")); err != nil {
					return err
				}

				return errStop
			})
			require.ErrorIs(t, err, errStop)
			require.Equal(t, 1, count(t, db))
		})

		t.Run("a panic rolls back and keeps panicking", func(t *testing.T) {
			require.Panics(t, func() {
				_ = db.Tx(ctx, func(tx *AppDB) error {
					_ = tx.Users.Insert(ctx, user("panicked"))

					panic("boom")
				})
			})

			require.Equal(t, 1, count(t, db))

			require.NoError(
				t,
				db.Users.Insert(ctx, user("after")),
				"the connection is still usable",
			)
		})

		t.Run("reads see the transaction's own writes", func(t *testing.T) {
			err := db.Tx(ctx, func(tx *AppDB) error {
				require.NoError(t, tx.Users.Insert(ctx, user("inner")))
				require.Equal(t, 3, count(t, tx))
				require.Equal(t, 2, count(t, db), "outside it, nothing shows yet")

				return errStop
			})
			require.ErrorIs(t, err, errStop)
		})

		t.Run("SaveChanges inside saves in the transaction", func(t *testing.T) {
			u, err := db.Users.Where(ant.Eq(&Users.Name, "committed")).Single(ctx)
			require.NoError(t, err)

			u.Age = 50

			err = db.Tx(ctx, func(tx *AppDB) error {
				require.NoError(t, tx.SaveChanges(ctx))

				return errStop
			})
			require.ErrorIs(t, err, errStop)
			fresh, err := db.Users.NoTracking().Find(ctx, u.ID)
			require.NoError(t, err)
			require.Zero(t, fresh.Age)
		})

		t.Run("a rollback restores what the tracker thinks is saved", func(t *testing.T) {
			require.True(
				t,
				fixture.HasChanges(t, db),
				"the change from the rolled back save is pending again",
			)

			require.NoError(t, db.SaveChanges(ctx))
			require.False(t, fixture.HasChanges(t, db))
		})

		t.Run("a rolled back insert is not tracked", func(t *testing.T) {
			u := user("gone")
			err := db.Tx(ctx, func(tx *AppDB) error {
				require.NoError(t, tx.Users.Insert(ctx, u))

				return errStop
			})
			require.ErrorIs(t, err, errStop)
			require.Equal(t, ant.Detached, fixture.Entry(t, db, u).State())
		})

		// The rollback takes back what the save wrote into the entity: the key
		// the database generated and the timestamps. So the entity is new again,
		// and the next save inserts it, with a key of its own.
		t.Run("a rolled back save gives back the key it was given", func(t *testing.T) {
			u := user("again")
			require.NoError(t, ant.Add(ctx, db, u))

			err := db.Tx(ctx, func(tx *AppDB) error {
				require.NoError(t, tx.SaveChanges(ctx))
				require.NotZero(t, u.ID)

				return errStop
			})
			require.ErrorIs(t, err, errStop)
			require.Zero(t, u.ID, "the key went with the row")
			require.True(t, u.CreatedAt.IsZero())
			require.Equal(t, ant.Added, fixture.Entry(t, db, u).State())

			require.NoError(t, db.SaveChanges(ctx))
			require.NotZero(t, u.ID)

			stored, err := db.Users.NoTracking().Find(ctx, u.ID)
			require.NoError(t, err)
			require.Equal(t, "again", stored.Name)
		})

		t.Run("a rolled back save gives back an appended post's key", func(t *testing.T) {
			u, err := db.Users.Include(&Users.Posts).Where(ant.Eq(&Users.Name, "again")).Single(ctx)
			require.NoError(t, err)

			u.Posts = append(u.Posts, fixture.Post{Title: "draft"})
			before := u.UpdatedAt

			err = db.Tx(ctx, func(tx *AppDB) error {
				u.Name = "again, renamed"

				require.NoError(t, tx.SaveChanges(ctx))

				return errStop
			})
			require.ErrorIs(t, err, errStop)
			require.Zero(t, u.Posts[0].ID, "the post is new again")
			require.Equal(t, before, u.UpdatedAt, "and the update's timestamp went back")

			require.NoError(t, db.SaveChanges(ctx))

			again, err := db.Users.NoTracking().Include(&Users.Posts).Find(ctx, u.ID)
			require.NoError(t, err)
			require.Equal(t, "again, renamed", again.Name)
			require.Len(t, again.Posts, 1, "the next save inserted it")
		})

		t.Run("a cancelled context stops it", func(t *testing.T) {
			cancelled, cancel := context.WithCancel(ctx)
			cancel()

			err := db.Tx(cancelled, func(tx *AppDB) error { return nil })
			require.ErrorIs(t, err, context.Canceled)
		})

		t.Run("a context cancelled inside rolls back", func(t *testing.T) {
			cancellable, cancel := context.WithCancel(ctx)
			before := count(t, db)

			err := db.Tx(cancellable, func(tx *AppDB) error {
				require.NoError(t, tx.Users.Insert(cancellable, user("cancelled")))
				cancel()

				return tx.Users.Insert(cancellable, user("never"))
			})
			require.ErrorIs(t, err, context.Canceled)
			require.Equal(t, before, count(t, db))
		})

		t.Run("the closure must take this context's type", func(t *testing.T) {
			type other struct {
				ant.Context
				Users ant.Table[User]
			}

			err := db.Tx(ctx, func(tx *other) error { return nil })
			require.ErrorIs(t, err, ant.ErrTxType)
		})

		t.Run("options", func(t *testing.T) {
			err := db.Tx(
				ctx,
				func(tx *AppDB) error {
					return tx.Users.InsertOnly(ctx, user("read only"))
				},
				ant.ReadOnly(),
			)
			require.ErrorContains(t, err, "read-only")

			err = db.Tx(
				ctx,
				func(tx *AppDB) error {
					level, err := ant.Raw[string](tx, `SHOW transaction_isolation`).First(ctx)
					require.NoError(t, err)
					require.Equal(t, "serializable", *level)

					return nil
				},
				ant.Isolation(sql.LevelSerializable),
			)
			require.NoError(t, err)
		})
	})
}

// A Tx inside a Tx is a savepoint, so only the inner part rolls back.
func TestNestedTx(t *testing.T) {
	fixture.RunTracked(t, func(t *testing.T, db *AppDB) {
		ctx := t.Context()

		t.Run("an inner failure keeps the outer work", func(t *testing.T) {
			err := db.Tx(ctx, func(tx *AppDB) error {
				require.NoError(t, tx.Users.Insert(ctx, user("outer")))

				inner := tx.Tx(ctx, func(tx2 *AppDB) error {
					require.NoError(t, tx2.Users.Insert(ctx, user("inner")))

					return errStop
				})
				require.ErrorIs(t, inner, errStop)

				return nil
			})
			require.NoError(t, err)
			require.Equal(t, 1, count(t, db))
		})

		t.Run("an outer failure undoes the inner work", func(t *testing.T) {
			err := db.Tx(ctx, func(tx *AppDB) error {
				require.NoError(t, tx.Tx(ctx, func(tx2 *AppDB) error {
					return tx2.Users.Insert(ctx, user("inner2"))
				}))

				return errStop
			})
			require.ErrorIs(t, err, errStop)
			require.Equal(t, 1, count(t, db))
		})

		t.Run("a failed statement can be recovered from in a savepoint", func(t *testing.T) {
			err := db.Tx(ctx, func(tx *AppDB) error {
				clash := tx.Tx(ctx, func(tx2 *AppDB) error {
					return tx2.Users.Insert(ctx, user("outer"))
				})
				require.Error(t, clash)

				return tx.Users.Insert(ctx, user("recovered"))
			})
			require.NoError(t, err)
			require.Equal(t, 2, count(t, db))
		})

		t.Run("SaveChanges in a transaction leaves it usable after a failure", func(t *testing.T) {
			err := db.Tx(ctx, func(tx *AppDB) error {
				session := ant.Session(tx)
				require.NoError(t, ant.Add(ctx, session, user("a1")))
				require.NoError(t, ant.Add(ctx, session, user("outer")))
				require.Error(t, session.SaveChanges(ctx))

				return tx.Users.Insert(ctx, user("still works"))
			})
			require.NoError(t, err)
			require.Equal(t, 3, count(t, db))
		})
	})
}

// Begin hands you the transaction to end yourself.
func TestBegin(t *testing.T) {
	fixture.RunTracked(t, func(t *testing.T, db *AppDB) {
		ctx := t.Context()

		t.Run("commit", func(t *testing.T) {
			tx, err := ant.Begin(ctx, db)
			require.NoError(t, err)
			require.NoError(t, tx.Users.Insert(ctx, user("one")))
			require.NoError(t, tx.Commit())
			require.Equal(t, 1, count(t, db))
		})

		t.Run("rollback", func(t *testing.T) {
			tx, err := ant.Begin(ctx, db, ant.Isolation(sql.LevelRepeatableRead))
			require.NoError(t, err)
			require.NoError(t, tx.Users.Insert(ctx, user("two")))
			require.NoError(t, tx.Rollback())
			require.Equal(t, 1, count(t, db))
		})

		t.Run("ending twice", func(t *testing.T) {
			tx, err := ant.Begin(ctx, db)
			require.NoError(t, err)
			require.NoError(t, tx.Commit())
			require.ErrorIs(t, tx.Commit(), ant.ErrTxDone)
			require.ErrorIs(t, tx.Rollback(), ant.ErrTxDone)
		})

		t.Run("outside a transaction", func(t *testing.T) {
			require.ErrorIs(t, db.Commit(), ant.ErrNotInTx)
			require.ErrorIs(t, db.Savepoint(ctx, "a"), ant.ErrNotInTx)
		})

		t.Run("savepoints", func(t *testing.T) {
			tx, err := ant.Begin(ctx, db)
			require.NoError(t, err)
			require.NoError(t, tx.Users.Insert(ctx, user("kept")))
			require.NoError(t, tx.Savepoint(ctx, "before"))
			require.NoError(t, tx.Users.Insert(ctx, user("undone")))
			require.NoError(t, tx.RollbackTo(ctx, "before"))
			require.NoError(t, tx.Savepoint(ctx, "after"))
			require.NoError(t, tx.Release(ctx, "after"))
			require.NoError(t, tx.Commit())
			require.Equal(t, 2, count(t, db))
		})

		t.Run("a savepoint that does not exist", func(t *testing.T) {
			tx, err := ant.Begin(ctx, db)
			require.NoError(t, err)
			require.Error(t, tx.RollbackTo(ctx, "missing"))
			require.NoError(t, tx.Rollback())
		})
	})
}

// With WithoutAutoTx, several writes are not wrapped in a transaction.
func TestWithoutAutoTx(t *testing.T) {
	fixture.Each(t, func(t *testing.T, target fixture.Target) {
		ctx := t.Context()
		db := fixture.Open[AppDB](t, target, ant.WithoutAutoTx())
		first, clash := user("first"), user("first")
		require.Error(t, db.Users.Insert(ctx, first, clash))
		require.Equal(t, 1, count(t, db), "the first insert stays")
	})
}
