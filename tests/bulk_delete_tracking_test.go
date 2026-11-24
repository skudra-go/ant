package tests

import (
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/skudra-go/ant"
	"github.com/skudra-go/ant/dialect"
	"github.com/skudra-go/ant/internal/testdb"
)

// Find does not answer from the identity map with a row a bulk delete of the
// same context removed, as it does not after DeleteByID (#154). The deleted
// row's instance stays tracked, as Delete documents.
func TestConformanceFindAfterBulkDelete(t *testing.T) {
	deletes := map[string]func(t *testing.T, app *AppDB, target testdb.Target, id int64, name string){
		"Delete": func(t *testing.T, app *AppDB, _ testdb.Target, id int64, _ string) {
			n, err := app.Users.Where(ant.Eq(&Users.ID, id)).Delete(t.Context())
			require.NoError(t, err)
			require.EqualValues(t, 1, n)
		},
		"ForceDelete": func(t *testing.T, app *AppDB, _ testdb.Target, id int64, _ string) {
			n, err := app.Users.Where(ant.Eq(&Users.ID, id)).ForceDelete(t.Context())
			require.NoError(t, err)
			require.EqualValues(t, 1, n)
		},
		"Delete with Limit": func(t *testing.T, app *AppDB, _ testdb.Target, id int64, _ string) {
			n, err := app.Users.Where(ant.Eq(&Users.ID, id)).
				OrderBy(&Users.ID).
				Limit(1).
				Delete(t.Context())
			require.NoError(t, err)
			require.EqualValues(t, 1, n)
		},
		"DeleteReturning": func(t *testing.T, app *AppDB, target testdb.Target, id int64, _ string) {
			gone, err := app.Users.Where(ant.Eq(&Users.ID, id)).DeleteReturning(t.Context())

			if !has(target, dialect.ReturningDelete) {
				require.ErrorIs(t, err, ant.ErrUnsupported)
				t.Skip("no DELETE ... RETURNING on " + target.Name)
			}

			require.NoError(t, err)
			require.Len(t, gone, 1)
		},
		"Prune": func(t *testing.T, app *AppDB, _ testdb.Target, id int64, _ string) {
			n, err := app.Users.Prune(t.Context(), ant.Eq(&Users.ID, id))
			require.NoError(t, err)
			require.EqualValues(t, 1, n)
		},
		"Merge": func(t *testing.T, app *AppDB, target testdb.Target, _ int64, name string) {
			n, err := ant.Merge(&app.Users, ant.Values(app, Wanted{name, 1}), ant.EqCol(&Users.Name, &Wanteds.Name)).
				WhenMatched().
				Delete().
				Exec(t.Context())

			if !has(target, dialect.Merge) {
				require.ErrorIs(t, err, ant.ErrUnsupported)
				t.Skip("no MERGE on " + target.Name)
			}

			if target.Name == "postgres" {
				version, err := ant.Raw[int](
					app,
					"SELECT current_setting('server_version_num')::int",
				).First(t.Context())
				require.NoError(t, err)

				if *version < 150000 {
					t.Skip("MERGE needs PostgreSQL 15")
				}
			}

			require.NoError(t, err)
			require.EqualValues(t, 1, n)
		},
	}

	testdb.ForEach(t, func(t *testing.T, target testdb.Target) {
		for name, remove := range deletes {
			t.Run(name, func(t *testing.T) {
				app := openApp(t, target)
				ctx := t.Context()
				prefix := strings.ReplaceAll(name, " ", "-")
				users := seedUsers(t, app, prefix+"-alice", 30, prefix+"-bob", 25)
				alice, err := app.Users.Find(ctx, users[0].ID)
				require.NoError(t, err)
				bob, err := app.Users.Find(ctx, users[1].ID)
				require.NoError(t, err)

				remove(t, app, target, alice.ID, alice.Name)

				_, err = app.Users.Find(ctx, alice.ID)
				require.ErrorIs(t, err, ant.ErrNotFound, "the row is gone")

				again, err := app.Users.Find(ctx, bob.ID)
				require.NoError(t, err)
				require.Same(t, bob, again, "a row still there is still the tracked instance")
			})
		}
	})
}

var errRollback = errors.New("roll back")

// A bulk delete that a transaction's rollback undoes leaves the row to Find.
func TestConformanceFindAfterBulkDeleteRolledBack(t *testing.T) {
	testdb.ForEach(t, func(t *testing.T, target testdb.Target) {
		app := openApp(t, target)
		ctx := t.Context()
		alice := seedUsers(t, app, "alice", 30)[0]

		tracked, err := app.Users.Find(ctx, alice.ID)
		require.NoError(t, err)

		err = app.Tx(ctx, func(tx *AppDB) error {
			n, err := tx.Users.Where(ant.Eq(&Users.ID, alice.ID)).Delete(ctx)
			require.NoError(t, err)
			require.EqualValues(t, 1, n)

			_, err = tx.Users.Find(ctx, alice.ID)
			require.ErrorIs(t, err, ant.ErrNotFound)

			return errRollback
		})
		require.ErrorIs(t, err, errRollback)

		again, err := app.Users.Find(ctx, alice.ID)
		require.NoError(t, err)
		require.Same(t, tracked, again)
	})
}

// An instance added and not yet saved has no row a bulk delete could remove, so
// Find still answers with it.
func TestConformanceFindAddedAfterBulkDelete(t *testing.T) {
	testdb.ForEach(t, func(t *testing.T, target testdb.Target) {
		app := openApp(t, target)
		ctx := t.Context()
		bob := seedUsers(t, app, "added-bob", 25)[0]
		added := &User{
			Base:  ant.Base{ID: bob.ID + 1000},
			Name:  "added-new",
			Email: "added-new@example.com",
			Age:   1,
		}
		require.NoError(t, ant.Add(ctx, app, added))

		n, err := app.Users.Where(ant.Eq(&Users.ID, bob.ID)).Delete(ctx)
		require.NoError(t, err)
		require.EqualValues(t, 1, n)

		got, err := app.Users.Find(ctx, added.ID)
		require.NoError(t, err)
		require.Same(t, added, got)
	})
}
