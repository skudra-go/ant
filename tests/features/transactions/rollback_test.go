package transactions

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/skudra-go/ant"
	"github.com/skudra-go/ant/tests/features/fixture"
)

type Post = fixture.Post

var Posts = fixture.Posts

// A rolled back transaction leaves nothing in the tracker that it began
// tracking: what it loaded, added, or reached through a relation is forgotten,
// so the next SaveChanges on the same context does not write what the rollback
// took back.
func TestRollbackForgetsWhatItTracked(t *testing.T) {
	fixture.Run(t, func(t *testing.T, db *AppDB) {
		ctx := t.Context()

		stored := func(id int64) *User {
			t.Helper()

			u, err := db.Users.Find(ctx, id)
			require.NoError(t, err)

			return u
		}

		t.Run("an error returned on purpose", func(t *testing.T) {
			account := user("refused")
			account.Age = 100
			require.NoError(t, db.Users.Insert(ctx, account))

			s := ant.Session(db)

			err := s.Tx(ctx, func(tx *AppDB) error {
				u, err := tx.Users.Find(ctx, account.ID)
				if err != nil {
					return err
				}

				u.Age -= 500

				if err := tx.SaveChanges(ctx); err != nil {
					return err
				}

				return errStop // the transfer is refused
			})
			require.ErrorIs(t, err, errStop)

			// The handler goes on to write an audit row on the same session.
			require.NoError(t, ant.Add(ctx, s, &Post{UserID: account.ID, Title: "transfer refused"}))
			require.NoError(t, s.SaveChanges(ctx))
			require.Equal(t, 100, stored(account.ID).Age, "the refused transfer stays rolled back")
		})

		t.Run("a panic", func(t *testing.T) {
			account := user("panicked")
			require.NoError(t, db.Users.Insert(ctx, account))

			s := ant.Session(db)

			require.Panics(t, func() {
				_ = s.Tx(ctx, func(tx *AppDB) error {
					u, err := tx.Users.Find(ctx, account.ID)
					if err != nil {
						return err
					}

					u.Age = 1
					require.NoError(t, tx.SaveChanges(ctx))

					panic("boom")
				})
			})

			require.False(t, fixture.HasChanges(t, s))
			require.Zero(t, stored(account.ID).Age)
		})

		t.Run("Begin and Rollback, with a nested Tx that loads relations", func(t *testing.T) {
			s := ant.Session(db)

			tx, err := ant.Begin(ctx, s)
			require.NoError(t, err)

			author := user("rolled-back")
			author.Posts = []Post{{Title: "rolled back"}}
			require.NoError(t, tx.Users.Insert(ctx, author))

			// The savepoint is released, so what it tracked passes to the
			// outer transaction, and goes with that one's rollback.
			err = tx.Tx(ctx, func(inner *AppDB) error {
				u, err := inner.Users.Include(&Users.Posts).
					Where(ant.Eq(&Users.ID, author.ID)).
					First(ctx)
				if err != nil {
					return err
				}

				u.Age = 5
				u.Posts[0].Title = "edited"

				return inner.SaveChanges(ctx)
			})
			require.NoError(t, err)
			require.NoError(t, tx.Rollback())

			require.False(t, fixture.HasChanges(t, s), "nothing the rollback took back is pending")
			require.Empty(t, fixture.Tracked[User](t, s))
			require.Empty(t, fixture.Tracked[Post](t, s))
			require.NoError(t, s.SaveChanges(ctx))

			exists, err := db.Users.Where(ant.Eq(&Users.Name, "rolled-back")).Exists(ctx)
			require.NoError(t, err)
			require.False(t, exists, "the rolled back insert is not made again")
		})

		t.Run("a savepoint forgets only its own", func(t *testing.T) {
			outer := user("outer")
			inner := user("inner")
			require.NoError(t, db.Users.Insert(ctx, outer, inner))

			s := ant.Session(db)

			err := s.Tx(ctx, func(tx *AppDB) error {
				o, err := tx.Users.Find(ctx, outer.ID)
				if err != nil {
					return err
				}

				o.Age = 7

				err = tx.Tx(ctx, func(sp *AppDB) error {
					i, err := sp.Users.Find(ctx, inner.ID)
					if err != nil {
						return err
					}

					i.Age = 9

					if err := sp.SaveChanges(ctx); err != nil {
						return err
					}

					return errStop
				})
				require.ErrorIs(t, err, errStop)

				return tx.SaveChanges(ctx)
			})
			require.NoError(t, err)

			require.Equal(t, 7, stored(outer.ID).Age, "the outer edit was saved")
			require.Zero(t, stored(inner.ID).Age, "the savepoint's edit was rolled back")
			require.Equal(
				t,
				[]string{"outer"},
				fixture.Names(fixture.Tracked[User](t, s)),
				"the savepoint's entity is forgotten",
			)
		})

		// The context the transaction came from is not in it: what it loads
		// while the transaction is open is its own, and stays.
		t.Run("what the context loads outside the transaction stays", func(t *testing.T) {
			outside := user("outside")
			require.NoError(t, db.Users.Insert(ctx, outside))

			s := ant.Session(db)

			err := s.Tx(ctx, func(tx *AppDB) error {
				if _, err := s.Users.Find(ctx, outside.ID); err != nil {
					return err
				}

				return errStop
			})
			require.ErrorIs(t, err, errStop)
			require.Equal(t, []string{"outside"}, fixture.Names(fixture.Tracked[User](t, s)))
		})

		// The tracker cannot tell an edit made inside the transaction from one
		// made before it, so an entity loaded before keeps its changes, and the
		// rollback makes what it saved pending again. Load inside the
		// transaction what it changes.
		t.Run("an entity loaded before the transaction keeps its changes", func(t *testing.T) {
			account := user("loaded-before")
			require.NoError(t, db.Users.Insert(ctx, account))

			s := ant.Session(db)
			u, err := s.Users.Find(ctx, account.ID)
			require.NoError(t, err)

			err = s.Tx(ctx, func(tx *AppDB) error {
				u.Age = 3

				if err := tx.SaveChanges(ctx); err != nil {
					return err
				}

				return errStop
			})
			require.ErrorIs(t, err, errStop)
			require.True(t, fixture.HasChanges(t, s))
			require.Equal(t, ant.Modified, fixture.Entry(t, s, u).State())
		})
	})
}
