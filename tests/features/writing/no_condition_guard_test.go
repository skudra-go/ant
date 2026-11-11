package writing

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/skudra-go/ant"
	"github.com/skudra-go/ant/tests/features/fixture"
)

// A bulk write with no condition is refused, because a missing filter would
// otherwise change the whole table. Unfiltered says it is on purpose.
func TestNoConditionGuard(t *testing.T) {
	fixture.Run(t, func(t *testing.T, db *fixture.AppDB) {
		fixture.Seed(t, db)
		ctx := t.Context()

		assertUntouched := func(t *testing.T) {
			t.Helper()

			users, err := db.Users.All(ctx)
			require.NoError(t, err)
			require.Len(t, users, 3)
		}

		t.Run("no Where", func(t *testing.T) {
			_, err := db.Users.OrderBy(&Users.Name).Delete(ctx)
			require.ErrorIs(t, err, ant.ErrNoCondition)
			_, err = db.Users.Limit(1).Update(ctx, ant.Set(&Users.Age, 0))
			require.ErrorIs(t, err, ant.ErrNoCondition)
			assertUntouched(t)
		})

		t.Run("an empty Where", func(t *testing.T) {
			_, err := db.Users.Where().Delete(ctx)
			require.ErrorIs(t, err, ant.ErrNoCondition)
			assertUntouched(t)
		})

		// Conditions built from an empty list are the dangerous case: the code
		// looks filtered, but nothing can exclude a row.
		t.Run("conditions that cannot exclude a row", func(t *testing.T) {
			var none []string
			for name, cond := range map[string]ant.Cond{
				"empty And":          ant.And(),
				"empty NotIn":        ant.NotIn(&Users.Name, none...),
				"And of empties":     ant.And(ant.And(), ant.NotIn(&Users.Name, none...)),
				"Or with an empty":   ant.Or(ant.Eq(&Users.Age, 1), ant.And()),
				"Not of an empty Or": ant.Not(ant.Or()),
			} {
				t.Run(name, func(t *testing.T) {
					_, err := db.Users.Where(cond).Delete(ctx)
					require.ErrorIs(t, err, ant.ErrNoCondition)
					assertUntouched(t)
				})
			}
		})

		t.Run("a condition that matches nothing is allowed", func(t *testing.T) {
			var none []string

			n, err := db.Users.Where(ant.In(&Users.Name, none...)).Delete(ctx)
			require.NoError(t, err)
			require.Zero(t, n)
		})

		t.Run("a skipped When leaves the guard on", func(t *testing.T) {
			_, err := db.Users.Limit(10).
				When(false, func(q *ant.Query[fixture.User]) *ant.Query[fixture.User] {
					return q.Where(ant.Eq(&Users.Age, 1))
				}).
				Delete(ctx)
			require.ErrorIs(t, err, ant.ErrNoCondition)
		})

		// A derived source counts as filtered when its rows are. A union has every
		// row unless both sides are filtered, and a replaced source no longer
		// counts.
		t.Run("a derived source carries its filter", func(t *testing.T) {
			adults := db.Users.Where(ant.Gt(&Users.Age, 18))

			_, err := adults.Union(db.Users.Query()).Delete(ctx)
			require.ErrorIs(t, err, ant.ErrNoCondition)
			_, err = db.Users.Query().From(db.Users.Query()).Delete(ctx)
			require.ErrorIs(t, err, ant.ErrNoCondition)
			_, err = db.Users.Query().From(adults).From(db.Users.Query()).Delete(ctx)
			require.ErrorIs(t, err, ant.ErrNoCondition)
			assertUntouched(t)

			n, err := db.Users.Query().From(adults).Update(ctx, ant.Inc(&Users.Age, 1))
			require.NoError(t, err)
			require.EqualValues(t, 2, n)
		})

		t.Run("Unfiltered allows it", func(t *testing.T) {
			n, err := db.Users.Unfiltered().Update(ctx, ant.Set(&Users.Age, 50))
			require.NoError(t, err)
			require.EqualValues(t, 3, n)

			n, err = db.Users.Unfiltered().Delete(ctx)
			require.NoError(t, err)
			require.EqualValues(t, 3, n)
		})
	})
}
