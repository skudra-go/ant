package writing

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/skudra-go/ant"
	"github.com/skudra-go/ant/tests/features/fixture"
)

// Inc, SetExpr, and UpdateMany change values in bulk.
func TestBulkChanges(t *testing.T) {
	fixture.Run(t, func(t *testing.T, db *fixture.AppDB) {
		alice, bob, _ := fixture.Seed(t, db)
		ctx := t.Context()
		age := func(id int64) int {
			u, err := db.Users.Find(ctx, id)
			require.NoError(t, err)

			return u.Age
		}

		t.Run("Inc and Dec run in the database", func(t *testing.T) {
			_, err := db.Users.Where(ant.Eq(&Users.ID, alice.ID)).
				Update(ctx, ant.Inc(&Users.Age, 5))
			require.NoError(t, err)
			require.Equal(t, 35, age(alice.ID))
			_, err = db.Users.Where(ant.Eq(&Users.ID, alice.ID)).Update(ctx, ant.Dec(&Users.Age, 5))
			require.NoError(t, err)
			require.Equal(t, 30, age(alice.ID))
		})

		t.Run("SetExpr", func(t *testing.T) {
			_, err := db.Users.Where(ant.Eq(&Users.ID, bob.ID)).
				Update(ctx, ant.SetExpr(&Users.Email, ant.Lower(&Users.Name)))
			require.NoError(t, err)
			u, err := db.Users.Find(ctx, bob.ID)
			require.NoError(t, err)
			require.Equal(t, "bob", u.Email)
		})

		t.Run("Update from a subquery", func(t *testing.T) {
			oldest := ant.SubExpr(db.Users.Where(), ant.Max(&Users.Age)).Expr()
			_, err := db.Users.Where(ant.Eq(&Users.ID, bob.ID)).
				Update(ctx, ant.SetExpr(&Users.Age, oldest))
			require.NoError(t, err)
			require.Equal(t, 40, age(bob.ID))
		})

		t.Run("UpdateMany writes a different value per row", func(t *testing.T) {
			alice.Age, bob.Age = 1, 2
			alice.Name = "not written"
			n, err := db.Users.UpdateMany(ctx, []*fixture.User{alice, bob}, ant.F(&Users.Age))
			require.NoError(t, err)
			require.EqualValues(t, 2, n)
			require.Equal(t, 1, age(alice.ID))
			require.Equal(t, 2, age(bob.ID))
			stored, err := db.Users.Find(ctx, alice.ID)
			require.NoError(t, err)
			require.Equal(t, "Alice", stored.Name)
		})

		t.Run("UpdateMany mistakes", func(t *testing.T) {
			_, err := db.Users.UpdateMany(ctx, []*fixture.User{alice})
			require.ErrorIs(t, err, ant.ErrNoAssignment)
			_, err = db.Users.UpdateMany(ctx, []*fixture.User{alice}, ant.F(&Posts.Title))
			require.ErrorIs(t, err, ant.ErrWrongModel)
			_, err = db.Users.UpdateMany(ctx, []*fixture.User{nil}, ant.F(&Users.Age))
			require.ErrorIs(t, err, ant.ErrNilEntity)
		})
	})
}

// Limit and Offset also bound bulk writes, oldest first with an ordering.
func TestLimitedBulkWrites(t *testing.T) {
	fixture.Run(t, func(t *testing.T, db *fixture.AppDB) {
		fixture.Seed(t, db)
		ctx := t.Context()
		all := ant.Gt(&Users.Age, 0)

		n, err := db.Users.Where(all).
			OrderBy(&Users.Name).
			Limit(1).
			Update(ctx, ant.Set(&Users.Age, 99))
		require.NoError(t, err)
		require.EqualValues(t, 1, n)

		first, err := db.Users.OrderBy(&Users.Name).First(ctx)
		require.NoError(t, err)
		require.Equal(t, 99, first.Age)

		n, err = db.Users.Where(all).OrderBy(&Users.Name).Offset(1).Limit(1).Delete(ctx)
		require.NoError(t, err)
		require.EqualValues(t, 1, n)

		left, err := db.Users.OrderBy(&Users.Name).All(ctx)
		require.NoError(t, err)
		require.Equal(t, []string{"Alice", "Carol"}, fixture.Names(left))
	})
}

// UpdateReturning and DeleteReturning give back the rows they touched.
func TestReturning(t *testing.T) {
	fixture.Run(t, func(t *testing.T, db *fixture.AppDB) {
		fixture.Seed(t, db)
		ctx := t.Context()

		changed, err := db.Users.Where(ant.Gt(&Users.Age, 18)).
			UpdateReturning(ctx, ant.Inc(&Users.Age, 1))
		require.NoError(t, err)
		require.Len(t, changed, 2)
		require.ElementsMatch(t, []int{31, 41}, []int{changed[0].Age, changed[1].Age})

		removed, err := db.Users.Where(ant.Eq(&Users.Name, "Bob")).DeleteReturning(ctx)
		require.NoError(t, err)
		require.Equal(t, []string{"Bob"}, fixture.Names(removed))

		_, err = db.Users.Query().DeleteReturning(ctx)
		require.ErrorIs(t, err, ant.ErrNoCondition)
	})
}

// Truncate empties a table.
func TestTruncate(t *testing.T) {
	fixture.Run(t, func(t *testing.T, db *fixture.AppDB) {
		alice, _, _ := fixture.Seed(t, db)
		ctx := t.Context()
		require.NoError(t, db.Posts.Insert(ctx, &fixture.Post{UserID: alice.ID, Title: "x"}))

		require.Error(t, db.Users.Truncate(ctx), "posts still refer to users")
		require.NoError(t, db.Posts.Truncate(ctx))
		n, err := db.Posts.Count(ctx)
		require.NoError(t, err)
		require.Zero(t, n)
	})
}

// Clone copies an entity for inserting as a new row.
func TestClone(t *testing.T) {
	fixture.Run(t, func(t *testing.T, db *fixture.AppDB) {
		alice, _, _ := fixture.Seed(t, db)
		ctx := t.Context()
		alice.Posts = []fixture.Post{{Title: "not copied"}}

		twin := ant.Clone(alice)
		require.Zero(t, twin.ID)
		require.Nil(t, twin.Posts)
		require.Equal(t, alice.Name, twin.Name)

		twin.Email = "twin@x"
		require.NoError(t, db.Users.Insert(ctx, twin))
		require.NotEqual(t, alice.ID, twin.ID)
	})
}
