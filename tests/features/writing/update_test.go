package writing

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/skudra-go/ant"
	"github.com/skudra-go/ant/tests/features/fixture"
)

// Update changes every matching row and reports how many it changed.
func TestUpdate(t *testing.T) {
	fixture.Run(t, func(t *testing.T, db *fixture.AppDB) {
		alice, bob, carol := fixture.Seed(t, db)
		ctx := t.Context()

		t.Run("returns the rows changed", func(t *testing.T) {
			n, err := db.Users.Where(ant.Gt(&Users.Age, 18)).
				Update(ctx, ant.Set(&Users.Bio, new("Adult")))
			require.NoError(t, err)
			require.EqualValues(t, 2, n)
		})

		t.Run("no match changes nothing", func(t *testing.T) {
			n, err := db.Users.Where(ant.Eq(&Users.Name, "Nobody")).
				Update(ctx, ant.Set(&Users.Age, 1))
			require.NoError(t, err)
			require.Zero(t, n)
		})

		t.Run("several assignments", func(t *testing.T) {
			_, err := db.Users.Where(ant.Eq(&Users.ID, bob.ID)).Update(ctx,
				ant.Set(&Users.Name, "Robert"),
				ant.Set(&Users.Age, 18))
			require.NoError(t, err)
			require.NoError(t, db.Users.Reload(ctx, bob))
			require.Equal(t, "Robert", bob.Name)
			require.Equal(t, 18, bob.Age)
		})

		t.Run("zero values are written", func(t *testing.T) {
			_, err := db.Users.Where(ant.Eq(&Users.ID, alice.ID)).
				Update(ctx, ant.Set(&Users.Age, 0), ant.Set(&Users.Email, ""))
			require.NoError(t, err)
			require.NoError(t, db.Users.Reload(ctx, alice))
			require.Zero(t, alice.Age)
			require.Empty(t, alice.Email)
		})

		t.Run("nil writes NULL", func(t *testing.T) {
			_, err := db.Users.Where(ant.Eq(&Users.ID, carol.ID)).
				Update(ctx, ant.Set(&Users.Bio, nil))
			require.NoError(t, err)
			require.NoError(t, db.Users.Reload(ctx, carol))
			require.Nil(t, carol.Bio)
		})

		t.Run("nothing to set", func(t *testing.T) {
			_, err := db.Users.Where(ant.Eq(&Users.ID, alice.ID)).Update(ctx)
			require.ErrorIs(t, err, ant.ErrNoAssignment)
		})

		t.Run("a field of another model", func(t *testing.T) {
			_, err := db.Users.Where(ant.Eq(&Users.ID, alice.ID)).
				//antvet:ignore another model's field, on purpose: the test checks the error
				Update(ctx, ant.Set(&Posts.Title, "x"))
			require.ErrorIs(t, err, ant.ErrWrongModel)
		})

		t.Run("a pointer that is not a probe", func(t *testing.T) {
			age := 1
			//antvet:ignore not a probe, on purpose: the test checks the error
			_, err := db.Users.Where(ant.Eq(&Users.ID, alice.ID)).Update(ctx, ant.Set(&age, 2))
			require.ErrorIs(t, err, ant.ErrNotProbe)
		})

		t.Run("a relation cannot be set", func(t *testing.T) {
			_, err := db.Users.Where(ant.Eq(&Users.ID, alice.ID)).
				Update(ctx, ant.Set(&Users.Posts, nil))
			require.ErrorIs(t, err, ant.ErrNotColumn)
		})

		t.Run("a constraint failure is the database's error", func(t *testing.T) {
			_, err := db.Users.Where(ant.Eq(&Users.ID, alice.ID)).
				Update(ctx, ant.Set(&Users.Email, "Carol@example.com"))
			require.ErrorContains(t, err, "duplicate key")
		})

		// One column with two values is refused before it runs, the same on
		// every database: PostgreSQL refuses it, and MySQL would keep the last.
		t.Run("a column set twice", func(t *testing.T) {
			_, err := db.Users.Where(ant.Eq(&Users.ID, alice.ID)).
				Update(ctx, ant.Set(&Users.Age, 1), ant.Inc(&Users.Age, 2))
			require.ErrorContains(t, err, "User.Age has two assignments")

			_, err = db.Users.UpdateMany(
				ctx,
				[]*fixture.User{alice},
				ant.F(&Users.Age),
				ant.F(&Users.Age),
			)
			require.ErrorContains(t, err, "User.Age has two assignments")

			got, err := db.Users.Find(ctx, alice.ID)
			require.NoError(t, err)
			require.Equal(t, alice.Age, got.Age, "nothing ran")
		})
	})
}
