package relations

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/skudra-go/ant"
	"github.com/skudra-go/ant/tests/features/fixture"
)

// Column and Omit load part of a row. Other fields keep their zero value.
func TestColumns(t *testing.T) {
	fixture.Run(t, func(t *testing.T, db *fixture.AppDB) {
		seed(t, db)
		ctx := t.Context()
		carol := ant.Eq(&Users.Name, "Carol")

		t.Run("Column", func(t *testing.T) {
			got, err := db.Users.Where(carol).Column(&Users.ID).Column(&Users.Name).Single(ctx)
			require.NoError(t, err)
			require.NotZero(t, got.ID)
			require.Equal(t, "Carol", got.Name)
			require.Zero(t, got.Age)
			require.Nil(t, got.Bio)
		})

		t.Run("Omit", func(t *testing.T) {
			got, err := db.Users.Where(carol).Omit(&Users.Bio).Single(ctx)
			require.NoError(t, err)
			require.Equal(t, 40, got.Age)
			require.Nil(t, got.Bio)
		})

		t.Run("Column and Omit combine", func(t *testing.T) {
			got, err := db.Users.Where(carol).
				Column(&Users.Name).
				Column(&Users.Age).
				Omit(&Users.Age).
				Single(ctx)
			require.NoError(t, err)
			require.Equal(t, "Carol", got.Name)
			require.Zero(t, got.Age)
		})

		t.Run("omitting everything", func(t *testing.T) {
			_, err := db.Users.Column(&Users.Name).Omit(&Users.Name).All(ctx)
			require.ErrorIs(t, err, ant.ErrNoColumns)
		})

		t.Run("a column of another model", func(t *testing.T) {
			//antvet:ignore another model's field, on purpose: the test checks the error
			_, err := db.Users.Column(&Posts.Title).All(ctx)
			require.ErrorIs(t, err, ant.ErrWrongModel)
		})

		t.Run("a relation is not a column", func(t *testing.T) {
			_, err := db.Users.Column(&Users.Posts).All(ctx)
			require.ErrorIs(t, err, ant.ErrNotColumn)
		})
	})
}

// Distinct removes duplicate rows.
func TestDistinct(t *testing.T) {
	fixture.Run(t, func(t *testing.T, db *fixture.AppDB) {
		seed(t, db)
		ctx := t.Context()

		t.Run("on selected columns", func(t *testing.T) {
			got, err := db.Posts.Column(&Posts.Published).Distinct().All(ctx)
			require.NoError(t, err)
			require.Len(t, got, 2)
		})

		t.Run("on a join", func(t *testing.T) {
			got, err := ant.Project[Summary](
				db.Posts.Join(&Posts.User).Distinct(),
				ant.Into(&Summaries.Author, &Users.Name),
			).All(ctx)
			require.NoError(t, err)
			require.Len(t, got, 2)
		})
	})
}
