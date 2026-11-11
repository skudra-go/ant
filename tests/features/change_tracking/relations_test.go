package change_tracking

import (
	"slices"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/skudra-go/ant"
	"github.com/skudra-go/ant/tests/features/fixture"
)

// Changing a loaded relation changes the rows. Post.UserID is required, so a
// post removed from its user is deleted.
func TestChangingRelations(t *testing.T) {
	ctx := t.Context

	t.Run("edit an element in place", func(t *testing.T) {
		fixture.RunTracked(t, func(t *testing.T, db *AppDB) {
			session, alice := loaded(t, db)
			alice.Posts[1].Title = "B2"

			require.NoError(t, session.SaveChanges(ctx()))
			require.Equal(t, []string{"A", "B2", "C"}, storedTitles(t, db, alice.ID))
		})
	})

	t.Run("append a new element", func(t *testing.T) {
		fixture.RunTracked(t, func(t *testing.T, db *AppDB) {
			session, alice := loaded(t, db)
			alice.Posts = append(alice.Posts, Post{Title: "D"})

			require.NoError(t, session.SaveChanges(ctx()))
			require.Equal(t, []string{"A", "B", "C", "D"}, storedTitles(t, db, alice.ID))
			require.NotZero(t, alice.Posts[3].ID, "the new key is copied back")
			require.Equal(t, alice.ID, alice.Posts[3].UserID, "so is the foreign key")
		})
	})

	// Appending can move every element to a new array. Tracking follows keys,
	// not addresses, so edits made after the move are kept.
	t.Run("edit an element after append moved the slice", func(t *testing.T) {
		fixture.RunTracked(t, func(t *testing.T, db *AppDB) {
			session, alice := loaded(t, db)
			alice.Posts = slices.Clip(alice.Posts)
			alice.Posts = append(alice.Posts, Post{Title: "D"})
			alice.Posts[0].Title = "A2"

			require.NoError(t, session.SaveChanges(ctx()))
			require.Equal(t, []string{"A2", "B", "C", "D"}, storedTitles(t, db, alice.ID))
		})
	})

	t.Run("remove from the end", func(t *testing.T) {
		fixture.RunTracked(t, func(t *testing.T, db *AppDB) {
			session, alice := loaded(t, db)
			alice.Posts = alice.Posts[:2]

			require.NoError(t, session.SaveChanges(ctx()))
			require.Equal(t, []string{"A", "B"}, storedTitles(t, db, alice.ID))
		})
	})

	// slices.Delete shifts later elements down, so the memory of the removed
	// post now holds another row. The right row must still go.
	t.Run("remove from the front", func(t *testing.T) {
		fixture.RunTracked(t, func(t *testing.T, db *AppDB) {
			session, alice := loaded(t, db)
			alice.Posts = slices.Delete(alice.Posts, 0, 1)

			require.NoError(t, session.SaveChanges(ctx()))
			require.Equal(t, []string{"B", "C"}, storedTitles(t, db, alice.ID))
		})
	})

	t.Run("remove and edit together", func(t *testing.T) {
		fixture.RunTracked(t, func(t *testing.T, db *AppDB) {
			session, alice := loaded(t, db)
			alice.Posts = slices.Delete(alice.Posts, 1, 2)
			alice.Posts[1].Title = "C2"

			require.NoError(t, session.SaveChanges(ctx()))
			require.Equal(t, []string{"A", "C2"}, storedTitles(t, db, alice.ID))
		})
	})

	t.Run("clear the relation", func(t *testing.T) {
		fixture.RunTracked(t, func(t *testing.T, db *AppDB) {
			session, alice := loaded(t, db)
			alice.Posts = nil

			require.NoError(t, session.SaveChanges(ctx()))
			require.Empty(t, storedTitles(t, db, alice.ID))
		})
	})

	t.Run("reorder changes nothing", func(t *testing.T) {
		fixture.RunTracked(t, func(t *testing.T, db *AppDB) {
			session, alice := loaded(t, db)
			slices.Reverse(alice.Posts)
			require.NoError(t, session.SaveChanges(ctx()))
			require.Equal(t, []string{"A", "B", "C"}, storedTitles(t, db, alice.ID))
			require.False(t, fixture.HasChanges(t, session))
		})
	})

	t.Run("later changes work after a save", func(t *testing.T) {
		fixture.RunTracked(t, func(t *testing.T, db *AppDB) {
			session, alice := loaded(t, db)
			alice.Posts = append(alice.Posts, Post{Title: "D"})

			require.NoError(t, session.SaveChanges(ctx()))

			alice.Posts = slices.Delete(alice.Posts, 3, 4)

			require.NoError(t, session.SaveChanges(ctx()))
			require.Equal(t, []string{"A", "B", "C"}, storedTitles(t, db, alice.ID))
		})
	})

	t.Run("move an element to another parent", func(t *testing.T) {
		fixture.RunTracked(t, func(t *testing.T, db *AppDB) {
			session, alice := loaded(t, db)
			bob := &User{Name: "Target", Email: "target@example.com"}
			require.NoError(t, session.Users.Insert(ctx(), bob))
			require.NoError(t, session.Users.Load(ctx(), &Users.Posts, bob))

			post := alice.Posts[0]
			post.UserID = bob.ID
			bob.Posts = append(bob.Posts, post)
			alice.Posts = alice.Posts[1:]

			require.NoError(t, session.SaveChanges(ctx()))
			require.Equal(t, []string{"B", "C"}, storedTitles(t, db, alice.ID))
			require.Equal(t, []string{"A"}, storedTitles(t, db, bob.ID))
		})
	})

	t.Run("a relation that was not loaded removes nothing", func(t *testing.T) {
		fixture.RunTracked(t, func(t *testing.T, db *AppDB) {
			alice, _, _ := fixture.Seed(t, db)
			require.NoError(t, db.Posts.InsertOnly(ctx(), &Post{UserID: alice.ID, Title: "kept"}))
			session := ant.Session(db)
			user, err := session.Users.Find(ctx(), alice.ID)
			require.NoError(t, err)

			user.Age = 31

			require.NoError(t, session.SaveChanges(ctx()))
			require.Equal(t, []string{"kept"}, storedTitles(t, db, alice.ID))
		})
	})
}

// An optional child has a nullable foreign key, so it is unlinked instead.
type Team struct {
	ID      int64
	Name    string
	Members []Member
}

type Member struct {
	ID     int64
	TeamID *int64
	Name   string
	Team   *Team
}

type TeamDB struct {
	ant.Context
	Teams   ant.Table[Team]
	Members ant.Table[Member]
}

const teamDDL = `
	CREATE TABLE teams (id BIGSERIAL PRIMARY KEY, name TEXT NOT NULL);
	CREATE TABLE members (id BIGSERIAL PRIMARY KEY, team_id BIGINT REFERENCES teams, name TEXT NOT NULL)`

var (
	Teams   = ant.Fields[Team]()
	Members = ant.Fields[Member]()
)

func TestOptionalRelations(t *testing.T) {
	fixture.RunTrackedWith(t, teamDDL, func(t *testing.T, db *TeamDB) {
		ctx := t.Context()
		red := &Team{Name: "Red"}
		require.NoError(t, db.Teams.Insert(ctx, red))
		ann, ben := &Member{Name: "Ann", TeamID: &red.ID}, &Member{Name: "Ben", TeamID: &red.ID}
		require.NoError(t, db.Members.Insert(ctx, ann, ben))

		teamOf := func(id int64) *int64 {
			m, err := db.Members.NoTracking().Find(ctx, id)
			require.NoError(t, err)

			return m.TeamID
		}

		t.Run("removed from the parent, the child stays with no parent", func(t *testing.T) {
			session := ant.Session(db)
			team, err := session.Teams.Include(
				&Teams.Members,
				func(q *ant.Query[Member]) *ant.Query[Member] {
					return q.OrderBy(&Members.Name)
				},
			).
				Find(ctx, red.ID)
			require.NoError(t, err)

			team.Members = slices.Delete(team.Members, 0, 1)

			require.NoError(t, session.SaveChanges(ctx))
			require.Nil(t, teamOf(ann.ID))
			require.NotNil(t, teamOf(ben.ID))
		})

		t.Run("a parent set to nil clears the foreign key", func(t *testing.T) {
			session := ant.Session(db)
			member, err := session.Members.IncludeOne(&Members.Team).Find(ctx, ben.ID)
			require.NoError(t, err)
			require.NotNil(t, member.Team)
			member.Team = nil

			require.NoError(t, session.SaveChanges(ctx))
			require.Nil(t, teamOf(ben.ID))
		})

		t.Run("a new parent is inserted and linked", func(t *testing.T) {
			session := ant.Session(db)
			member, err := session.Members.Find(ctx, ann.ID)
			require.NoError(t, err)

			member.Team = &Team{Name: "Blue"}

			require.NoError(t, session.SaveChanges(ctx))
			require.NotZero(t, member.Team.ID)
			require.Equal(t, member.Team.ID, *teamOf(ann.ID))
		})

		t.Run("an existing parent is linked, not inserted", func(t *testing.T) {
			session := ant.Session(db)
			member, err := session.Members.Find(ctx, ben.ID)
			require.NoError(t, err)

			member.Team = &Team{ID: red.ID, Name: "ignored"}

			require.NoError(t, session.SaveChanges(ctx))
			require.Equal(t, red.ID, *teamOf(ben.ID))

			stored, err := db.Teams.NoTracking().Find(ctx, red.ID)
			require.NoError(t, err)
			require.Equal(t, "Red", stored.Name)
		})

		t.Run("changing the foreign key directly wins over a loaded parent", func(t *testing.T) {
			session := ant.Session(db)
			blue := &Team{Name: "Green"}
			require.NoError(t, session.Teams.Insert(ctx, blue))
			member, err := session.Members.IncludeOne(&Members.Team).Find(ctx, ben.ID)
			require.NoError(t, err)
			require.NotNil(t, member.Team)
			member.TeamID = &blue.ID

			require.NoError(t, session.SaveChanges(ctx))
			require.Equal(t, blue.ID, *teamOf(ben.ID))
		})
	})
}
