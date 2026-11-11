package relations

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/skudra-go/ant"
	"github.com/skudra-go/ant/tests/features/fixture"
)

// Has filters by what a row relates to, without loading it.
func TestHas(t *testing.T) {
	fixture.Run(t, func(t *testing.T, db *fixture.AppDB) {
		seed(t, db)
		ctx := t.Context()
		names := func(c ...ant.Cond) []string {
			got, err := db.Users.Where(c...).OrderBy(&Users.Name).All(ctx)
			require.NoError(t, err)

			return fixture.Names(got)
		}

		t.Run("any related row", func(t *testing.T) {
			require.Equal(t, []string{"Alice", "Bob"}, names(ant.Has(&Users.Posts)))
		})

		t.Run("a related row that meets conditions", func(t *testing.T) {
			require.Equal(
				t,
				[]string{"Alice"},
				names(ant.Has(&Users.Posts, ant.Eq(&Posts.Title, "Draft"))),
			)
		})

		t.Run("HasNone", func(t *testing.T) {
			require.Equal(t, []string{"Carol"}, names(ant.HasNone(&Users.Posts)))

			require.Equal(
				t,
				[]string{"Bob", "Carol"},
				names(ant.HasNone(&Users.Posts, ant.Eq(&Posts.Title, "Draft"))),
			)
		})

		t.Run("HasOne follows a many-to-one relation", func(t *testing.T) {
			got := titles(
				t,
				db,
				db.Posts.Where(ant.HasOne(&Posts.User, ant.Gt(&Users.Age, 18))).
					OrderBy(&Posts.Title),
			)
			require.Equal(t, []string{"Draft", "Hello"}, got)
		})

		t.Run("combines with other conditions", func(t *testing.T) {
			require.Equal(t, []string{"Bob"}, names(ant.Has(&Users.Posts), ant.Lt(&Users.Age, 18)))
			require.Equal(t, []string{"Carol"}, names(ant.Not(ant.Has(&Users.Posts))))
		})

		t.Run("an error in a nested condition is reported", func(t *testing.T) {
			x := 1
			//antvet:ignore not a probe, on purpose: the test checks the error
			_, err := db.Users.Where(ant.Has(&Users.Posts, ant.Gt(&x, 0))).All(ctx)
			require.ErrorIs(t, err, ant.ErrNotProbe)
		})

		t.Run("a relation of another model", func(t *testing.T) {
			_, err := db.Posts.Where(ant.Has(&Users.Posts)).All(ctx)
			require.ErrorIs(t, err, ant.ErrWrongModel)
		})
	})
}
