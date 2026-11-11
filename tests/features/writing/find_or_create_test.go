package writing

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/skudra-go/ant"
	"github.com/skudra-go/ant/tests/features/fixture"
)

// FirstOrCreate and FirstOrNew find a row or make one.
func TestFirstOrCreate(t *testing.T) {
	fixture.Run(t, func(t *testing.T, db *fixture.AppDB) {
		ctx := t.Context()
		byEmail := ant.Eq(&Users.Email, "a@x")
		user := func() *fixture.User { return &fixture.User{Name: "a", Email: "a@x"} }

		t.Run("creates when missing", func(t *testing.T) {
			got, created, err := db.Users.FirstOrCreate(ctx, user(), byEmail)
			require.NoError(t, err)
			require.True(t, created)
			require.NotZero(t, got.ID)
		})

		t.Run("finds when present", func(t *testing.T) {
			got, created, err := db.Users.FirstOrCreate(ctx, user(), byEmail)
			require.NoError(t, err)
			require.False(t, created)
			require.NotZero(t, got.ID)
		})

		// Another writer got there between the check and the insert: the unique
		// constraint stops this insert, and the winner's row comes back.
		t.Run("loses a race gracefully", func(t *testing.T) {
			got, created, err := db.Users.FirstOrCreate(
				ctx,
				user(),
				ant.Eq(&Users.Email, "a@x"),
				ant.Eq(&Users.Name, "a"),
			)
			require.NoError(t, err)
			require.False(t, created)
			require.Equal(t, "a", got.Name)

			mismatch := ant.Eq(&Users.Name, "not there")
			_, _, err = db.Users.FirstOrCreate(ctx, user(), mismatch)
			require.ErrorContains(t, err, "duplicate key", "the conflict is not a matching row")
		})

		t.Run("inside a transaction", func(t *testing.T) {
			err := db.Tx(ctx, func(tx *fixture.AppDB) error {
				_, _, err := tx.Users.FirstOrCreate(ctx, user(), ant.Eq(&Users.Name, "nope"))
				require.Error(t, err)
				n, err := tx.Users.Count(ctx)
				require.NoError(t, err, "the transaction survives the failed insert")
				require.EqualValues(t, 1, n)

				return nil
			})
			require.NoError(t, err)
		})

		t.Run("FirstOrNew does not save", func(t *testing.T) {
			fresh := &fixture.User{Name: "new", Email: "new@x"}
			got, found, err := db.Users.FirstOrNew(ctx, fresh, ant.Eq(&Users.Email, "new@x"))
			require.NoError(t, err)
			require.False(t, found)
			require.Same(t, fresh, got)
			require.Zero(t, got.ID)

			got, found, err = db.Users.FirstOrNew(ctx, fresh, byEmail)
			require.NoError(t, err)
			require.True(t, found)
			require.NotSame(t, fresh, got)
		})

		t.Run("a nil entity", func(t *testing.T) {
			_, _, err := db.Users.FirstOrCreate(ctx, nil, byEmail)
			require.ErrorIs(t, err, ant.ErrNilEntity)
		})
	})
}
