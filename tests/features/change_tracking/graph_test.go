package change_tracking

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/skudra-go/ant"
	"github.com/skudra-go/ant/tests/features/fixture"
)

// Insert saves a whole graph of new entities, parents first.
func TestInsertGraph(t *testing.T) {
	fixture.RunTracked(t, func(t *testing.T, db *AppDB) {
		ctx := t.Context()

		t.Run("children", func(t *testing.T) {
			alice := &User{
				Name:  "Alice",
				Email: "alice@example.com",
				Posts: []Post{{Title: "A"}, {Title: "B"}},
			}
			require.NoError(t, db.Users.Insert(ctx, alice))
			require.NotZero(t, alice.ID)
			require.NotZero(t, alice.Posts[1].ID)
			require.Equal(t, alice.ID, alice.Posts[1].UserID)
			require.Equal(t, []string{"A", "B"}, storedTitles(t, db, alice.ID))
		})

		t.Run("a parent through the child", func(t *testing.T) {
			post := &Post{Title: "C", User: &User{Name: "Bob", Email: "bob@example.com"}}
			require.NoError(t, db.Posts.Insert(ctx, post))
			require.NotZero(t, post.User.ID)
			require.Equal(t, post.User.ID, post.UserID)
		})

		t.Run("an existing parent is linked", func(t *testing.T) {
			alice, err := db.Users.Where(ant.Eq(&Users.Name, "Alice")).Single(ctx)
			require.NoError(t, err)

			post := &Post{Title: "D", User: alice}
			require.NoError(t, db.Posts.Insert(ctx, post))
			require.Equal(t, alice.ID, post.UserID)
		})

		t.Run("both directions at once", func(t *testing.T) {
			carol := &User{Name: "Carol", Email: "carol@example.com"}
			carol.Posts = []Post{{Title: "E", User: carol}}
			require.NoError(t, db.Users.Insert(ctx, carol))
			require.Equal(t, carol.ID, carol.Posts[0].UserID)
		})

		t.Run("all or nothing", func(t *testing.T) {
			dan := &User{Name: "Dan", Email: "Alice@example.com", Posts: []Post{{Title: "F"}}}
			dan.Email = "alice@example.com" // clashes with Alice
			require.Error(t, db.Users.Insert(ctx, dan))
			require.Zero(t, dan.ID)
			require.Zero(t, dan.Posts[0].ID)
			require.Empty(t, storedTitles(t, db, 0))
		})

		t.Run("InsertOnly ignores relations", func(t *testing.T) {
			eve := &User{Name: "Eve", Email: "eve@example.com", Posts: []Post{{Title: "G"}}}
			require.NoError(t, db.Users.InsertOnly(ctx, eve))
			require.Empty(t, storedTitles(t, db, eve.ID))
		})

		t.Run("children of a tracked parent, then more", func(t *testing.T) {
			frank := &User{Name: "Frank", Email: "frank@example.com", Posts: []Post{{Title: "H"}}}
			require.NoError(t, db.Users.Insert(ctx, frank))
			frank.Posts[0].Title = "H2"

			require.NoError(t, db.SaveChanges(ctx))
			require.Equal(t, []string{"H2"}, storedTitles(t, db, frank.ID))
		})
	})
}

// A new row can depend on another only one way round.
type Chicken struct {
	ID    int64
	EggID *int64
	Egg   *Egg
}

type Egg struct {
	ID        int64
	ChickenID *int64
	Chicken   *Chicken
}

type FarmDB struct {
	ant.Context
	Chickens ant.Table[Chicken]
	Eggs     ant.Table[Egg]
}

func TestCycle(t *testing.T) {
	ddl := `
		CREATE TABLE chickens (id BIGSERIAL PRIMARY KEY, egg_id BIGINT);
		CREATE TABLE eggs (id BIGSERIAL PRIMARY KEY, chicken_id BIGINT REFERENCES chickens)`
	fixture.RunTrackedWith(t, ddl, func(t *testing.T, db *FarmDB) {
		ctx := t.Context()

		t.Run("new rows in a cycle are refused", func(t *testing.T) {
			chicken := &Chicken{}
			chicken.Egg = &Egg{Chicken: chicken}
			require.ErrorIs(t, db.Chickens.Insert(ctx, chicken), ant.ErrCycle)
			require.Zero(t, chicken.ID)
		})

		t.Run("existing rows in a cycle are fine", func(t *testing.T) {
			chicken, egg := &Chicken{}, &Egg{}
			require.NoError(t, db.Chickens.Insert(ctx, chicken))
			require.NoError(t, db.Eggs.Insert(ctx, egg))
			chicken.EggID = &egg.ID
			egg.ChickenID = &chicken.ID

			require.NoError(t, db.SaveChanges(ctx))

			chicken.Egg = egg
			egg.Chicken = chicken

			require.NoError(t, db.SaveChanges(ctx))
		})
	})
}

// Deletes run children first, so foreign keys hold throughout.
func TestDeleteOrder(t *testing.T) {
	ddl := `
		CREATE TABLE teams (id BIGSERIAL PRIMARY KEY, name TEXT NOT NULL);
		CREATE TABLE members (id BIGSERIAL PRIMARY KEY, team_id BIGINT REFERENCES teams, name TEXT NOT NULL)`
	fixture.RunTrackedWith(t, ddl, func(t *testing.T, db *TeamDB) {
		ctx := t.Context()
		team := &Team{Name: "Red", Members: []Member{{Name: "Ann"}, {Name: "Ben"}}}
		require.NoError(t, db.Teams.Insert(ctx, team))

		// Removed parent first: the order of these calls must not matter.
		require.NoError(t, ant.Remove(ctx, db, team))

		for i := range team.Members {
			require.NoError(t, ant.Remove(ctx, db, &team.Members[i]))
		}

		team.Members = nil

		require.NoError(t, db.SaveChanges(ctx))

		left, err := db.Members.NoTracking().All(ctx)
		require.NoError(t, err)
		require.Empty(t, left)
	})
}
