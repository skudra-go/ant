package tests

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/skudra-go/ant"
	"github.com/skudra-go/ant/internal/testdb"
)

var errRule = errors.New("business rule failed")

// A ctx bound to a transaction with ant.InTx takes every context on the same
// pool into it: code built with the root, such as a repository, a session made
// from the root, and a Tx or Begin of theirs, which becomes a savepoint. Code
// that held the root inside Tx committed on its own, and waited on the locks of
// its own transaction until the ctx ended (#146).
func TestConformanceBoundContextRunsInTheTransaction(t *testing.T) {
	testdb.ForEach(t, func(t *testing.T, target testdb.Target) {
		db := openSignups(t, target)
		repo := db // held by code built at startup

		count := func(t *testing.T) int64 {
			t.Helper()

			n, err := db.Signups.Count(t.Context())
			require.NoError(t, err)

			return n
		}

		clear := func(t *testing.T) {
			t.Helper()

			_, err := db.Signups.Unfiltered().Delete(t.Context())
			require.NoError(t, err)
		}

		t.Run("a write through the root rolls back with the transaction", func(t *testing.T) {
			clear(t)
			ctx := t.Context()

			err := db.Tx(ctx, func(tx *SignupDB) error {
				ctx := ant.InTx(ctx, tx)

				if err := repo.Signups.Insert(
					ctx,
					&Signup{Email: "leaked@x", root: repo},
				); err != nil {
					return err
				}

				return errRule
			})
			require.ErrorIs(t, err, errRule)
			require.Zero(t, count(t))
		})

		t.Run("a write through the root waits on no lock of its transaction", func(t *testing.T) {
			clear(t)

			ctx, cancel := context.WithTimeout(t.Context(), 4*time.Second)
			defer cancel()

			row := &Signup{Email: "a@x", root: db}
			require.NoError(t, db.Signups.Insert(ctx, row))

			err := db.Tx(ctx, func(tx *SignupDB) error {
				ctx := ant.InTx(ctx, tx)

				if _, err := tx.Signups.Where(ant.Eq(&Signups.ID, row.ID)).
					Update(ctx, ant.Set(&Signups.Email, "tx@x")); err != nil {
					return err
				}

				_, err := repo.Signups.Where(ant.Eq(&Signups.ID, row.ID)).
					Update(ctx, ant.Inc(&Signups.Edits, 5))

				return err
			})
			require.NoError(t, err)

			saved, err := db.Signups.Find(ctx, row.ID)
			require.NoError(t, err)
			require.Equal(t, "tx@x", saved.Email)
			require.Equal(t, int64(5), saved.Edits)
		})

		t.Run("a session's save is part of the transaction and its rollback", func(t *testing.T) {
			clear(t)
			ctx := t.Context()

			require.NoError(t, db.Signups.Insert(ctx, &Signup{Email: "s@x", root: db}))

			s := ant.Session(db)

			m, err := s.Signups.Where(ant.Eq(&Signups.Email, "s@x")).First(ctx)
			require.NoError(t, err)

			m.root = db
			m.Email = "saved@x"

			err = db.Tx(ctx, func(tx *SignupDB) error {
				if err := s.SaveChanges(ant.InTx(ctx, tx)); err != nil {
					return err
				}

				return errRule
			})
			require.ErrorIs(t, err, errRule)

			stored, err := db.Signups.Find(ctx, m.ID)
			require.NoError(t, err)
			require.Equal(t, "s@x", stored.Email, "the rollback took the save back")

			// The rollback settled the session's tracker: the change is pending
			// again, and the next save writes it.
			pending, err := s.HasChanges(ctx)
			require.NoError(t, err)
			require.True(t, pending)
			require.NoError(t, s.SaveChanges(ctx))

			stored, err = db.Signups.Find(ctx, m.ID)
			require.NoError(t, err)
			require.Equal(t, "saved@x", stored.Email)
		})

		t.Run("a new entity's key is taken back with the rollback", func(t *testing.T) {
			clear(t)
			ctx := t.Context()

			s := ant.Session(db)
			fresh := &Signup{Email: "new@x", root: db}

			err := db.Tx(ctx, func(tx *SignupDB) error {
				if err := s.Signups.Insert(ant.InTx(ctx, tx), fresh); err != nil {
					return err
				}

				require.NotZero(t, fresh.ID)

				return errRule
			})
			require.ErrorIs(t, err, errRule)
			require.Zero(t, fresh.ID, "the rollback took the generated key back")
			require.Zero(t, count(t))
		})

		t.Run("a Tx given the bound ctx is a savepoint", func(t *testing.T) {
			clear(t)
			ctx := t.Context()

			err := db.Tx(ctx, func(tx *SignupDB) error {
				ctx := ant.InTx(ctx, tx)

				if err := tx.Signups.Insert(ctx, &Signup{Email: "outer@x", root: db}); err != nil {
					return err
				}

				inner := repo.Tx(ctx, func(nested *SignupDB) error {
					if err := nested.Signups.Insert(
						ctx,
						&Signup{Email: "inner@x", root: db},
					); err != nil {
						return err
					}

					return errRule
				})
				require.ErrorIs(t, inner, errRule)

				return nil
			})
			require.NoError(t, err)

			left, err := db.Signups.All(ctx)
			require.NoError(t, err)
			require.Len(t, left, 1)
			require.Equal(t, "outer@x", left[0].Email)
		})

		t.Run("a ctx kept after its transaction ended runs nothing", func(t *testing.T) {
			clear(t)
			ctx := t.Context()

			var kept context.Context

			require.NoError(t, db.Tx(ctx, func(tx *SignupDB) error {
				kept = ant.InTx(ctx, tx)

				return nil
			}))

			err := repo.Signups.Insert(kept, &Signup{Email: "late@x", root: db})
			require.True(
				t,
				errors.Is(err, sql.ErrTxDone) || errors.Is(err, ant.ErrTxDone),
				"got %v",
				err,
			)

			require.Zero(t, count(t))
		})
	})
}
