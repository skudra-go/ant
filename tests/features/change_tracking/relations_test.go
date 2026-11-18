package change_tracking

import (
	"database/sql"
	"slices"
	"testing"

	"github.com/google/uuid"
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

	// A post decoded from a request body has no UserID. Put in place of a loaded
	// one, it is still in alice's posts, so it keeps her as its user.
	t.Run("replace an element with one that has no foreign key", func(t *testing.T) {
		fixture.RunTracked(t, func(t *testing.T, db *AppDB) {
			session, alice := loaded(t, db)
			decoded := Post{Title: "A2"}
			decoded.ID = alice.Posts[0].ID
			alice.Posts[0] = decoded

			require.NoError(t, session.SaveChanges(ctx()))
			require.Equal(t, []string{"A2", "B", "C"}, storedTitles(t, db, alice.ID))
			require.Equal(t, alice.ID, alice.Posts[0].UserID)
		})
	})

	// A required foreign key at its zero value names no user, so a loaded user
	// it still points to sets it, as a moved post's is.
	t.Run("a zero foreign key under a loaded parent is the parent's", func(t *testing.T) {
		fixture.RunTracked(t, func(t *testing.T, db *AppDB) {
			alice, _, _ := fixture.Seed(t, db)
			post := &Post{UserID: alice.ID, Title: "mine"}
			require.NoError(t, db.Posts.InsertOnly(ctx(), post))

			session := ant.Session(db)
			loaded, err := session.Posts.IncludeOne(&Posts.User).Find(ctx(), post.ID)
			require.NoError(t, err)

			loaded.UserID = 0

			require.NoError(t, session.SaveChanges(ctx()))
			require.Equal(t, alice.ID, loaded.UserID)
			require.Equal(t, []string{"mine"}, storedTitles(t, db, alice.ID))
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

		// A nullable foreign key at its zero value is NULL, no parent, which is
		// the application's to choose, so it wins over the loaded parent.
		t.Run("clearing the foreign key directly wins over a loaded parent", func(t *testing.T) {
			session := ant.Session(db)
			member, err := session.Members.IncludeOne(&Members.Team).Find(ctx, ben.ID)
			require.NoError(t, err)
			require.NotNil(t, member.Team)
			member.TeamID = nil

			require.NoError(t, session.SaveChanges(ctx))
			require.Nil(t, teamOf(ben.ID))
		})
	})
}

// Whether a removed child is deleted or unlinked follows whether its foreign key
// can be written as NULL. A uuid.UUID cannot: its zero value is a real UUID, so
// the child is required and is deleted, as with an integer key.
type Guild struct {
	ID    uuid.UUID
	Name  string
	Seats []Seat
}

type Seat struct {
	ID      uuid.UUID
	GuildID uuid.UUID
	Name    string
	Guild   *Guild
}

// A sql.Null foreign key writes its zero value as NULL, so the child is
// optional.
type Crew struct {
	ID    int64
	Name  string
	Hands []Hand
}

type Hand struct {
	ID     int64
	CrewID sql.Null[int64]
	Name   string
}

// NotNull makes a pointer foreign key required.
type Fleet struct {
	ID    int64
	Name  string
	Ships []Ship
}

type Ship struct {
	ID      int64
	FleetID *int64
	Name    string
	Fleet   *Fleet
}

var Ships = ant.Fields[Ship]()

func (Ship) Configure(b *ant.ModelBuilder[Ship]) {
	b.Column(&Ships.FleetID).NotNull()
}

type RequiredDB struct {
	ant.Context
	Guilds ant.Table[Guild]
	Seats  ant.Table[Seat]
	Crews  ant.Table[Crew]
	Hands  ant.Table[Hand]
	Fleets ant.Table[Fleet]
	Ships  ant.Table[Ship]
}

const requiredDDL = `
	CREATE TABLE guilds (id UUID PRIMARY KEY, name TEXT NOT NULL);
	CREATE TABLE seats (id UUID PRIMARY KEY, guild_id UUID NOT NULL REFERENCES guilds, name TEXT NOT NULL);
	CREATE TABLE crews (id BIGSERIAL PRIMARY KEY, name TEXT NOT NULL);
	CREATE TABLE hands (id BIGSERIAL PRIMARY KEY, crew_id BIGINT REFERENCES crews, name TEXT NOT NULL);
	CREATE TABLE fleets (id BIGSERIAL PRIMARY KEY, name TEXT NOT NULL);
	CREATE TABLE ships (id BIGSERIAL PRIMARY KEY, fleet_id BIGINT NOT NULL REFERENCES fleets, name TEXT NOT NULL)`

func TestRequiredAndOptionalForeignKeyTypes(t *testing.T) {
	fixture.RunTrackedWith(t, requiredDDL, func(t *testing.T, db *RequiredDB) {
		ctx := t.Context()

		var (
			Guilds = ant.Fields[Guild]()
			Seats  = ant.Fields[Seat]()
			Crews  = ant.Fields[Crew]()
			Hands  = ant.Fields[Hand]()
			Fleets = ant.Fields[Fleet]()
		)

		t.Run("a uuid.UUID key is required: the removed child is deleted", func(t *testing.T) {
			guild := &Guild{ID: uuid.New(), Name: "Smiths"}
			require.NoError(t, db.Guilds.Insert(ctx, guild))

			require.NoError(t, db.Seats.Insert(
				ctx,
				&Seat{ID: uuid.New(), GuildID: guild.ID, Name: "a"},
				&Seat{ID: uuid.New(), GuildID: guild.ID, Name: "b"},
			))

			session := ant.Session(db)
			loaded, err := session.Guilds.Include(
				&Guilds.Seats,
				func(q *ant.Query[Seat]) *ant.Query[Seat] { return q.OrderBy(&Seats.Name) },
			).
				Find(ctx, guild.ID)
			require.NoError(t, err)
			require.Len(t, loaded.Seats, 2)

			loaded.Seats = loaded.Seats[:1]

			require.NoError(t, session.SaveChanges(ctx))

			names, err := ant.Pluck(ctx, db.Seats.OrderBy(&Seats.Name), &Seats.Name)
			require.NoError(t, err)
			require.Equal(t, []string{"a"}, names)
		})

		t.Run("a uuid.UUID key is kept when its parent pointer is emptied", func(t *testing.T) {
			guild := &Guild{ID: uuid.New(), Name: "Bakers"}
			require.NoError(t, db.Guilds.Insert(ctx, guild))

			seat := &Seat{ID: uuid.New(), GuildID: guild.ID, Name: "c"}
			require.NoError(t, db.Seats.Insert(ctx, seat))

			session := ant.Session(db)
			loaded, err := session.Seats.IncludeOne(&Seats.Guild).Find(ctx, seat.ID)
			require.NoError(t, err)
			require.NotNil(t, loaded.Guild)

			loaded.Guild = nil

			require.NoError(t, session.SaveChanges(ctx))

			stored, err := db.Seats.NoTracking().Find(ctx, seat.ID)
			require.NoError(t, err)
			require.Equal(t, guild.ID, stored.GuildID)
		})

		t.Run("a sql.Null key is optional: the removed child is unlinked", func(t *testing.T) {
			crew := &Crew{Name: "Deck"}
			require.NoError(t, db.Crews.Insert(ctx, crew))

			of := sql.Null[int64]{V: crew.ID, Valid: true}
			require.NoError(t, db.Hands.Insert(
				ctx,
				&Hand{CrewID: of, Name: "a"},
				&Hand{CrewID: of, Name: "b"},
			))

			session := ant.Session(db)
			loaded, err := session.Crews.Include(
				&Crews.Hands,
				func(q *ant.Query[Hand]) *ant.Query[Hand] { return q.OrderBy(&Hands.Name) },
			).
				Find(ctx, crew.ID)
			require.NoError(t, err)
			require.Len(t, loaded.Hands, 2)

			loaded.Hands = loaded.Hands[:1]

			require.NoError(t, session.SaveChanges(ctx))

			hands, err := db.Hands.NoTracking().OrderBy(&Hands.Name).All(ctx)
			require.NoError(t, err)
			require.Len(t, hands, 2)
			require.True(t, hands[0].CrewID.Valid)
			require.False(t, hands[1].CrewID.Valid)
		})

		t.Run("NotNull on a pointer key makes it required", func(t *testing.T) {
			fleet := &Fleet{Name: "North"}
			require.NoError(t, db.Fleets.Insert(ctx, fleet))

			require.NoError(t, db.Ships.Insert(
				ctx,
				&Ship{FleetID: &fleet.ID, Name: "a"},
				&Ship{FleetID: &fleet.ID, Name: "b"},
			))

			session := ant.Session(db)
			loaded, err := session.Fleets.Include(
				&Fleets.Ships,
				func(q *ant.Query[Ship]) *ant.Query[Ship] { return q.OrderBy(&Ships.Name) },
			).
				Find(ctx, fleet.ID)
			require.NoError(t, err)
			require.Len(t, loaded.Ships, 2)

			loaded.Ships = loaded.Ships[:1]

			require.NoError(t, session.SaveChanges(ctx))

			names, err := ant.Pluck(ctx, db.Ships.OrderBy(&Ships.Name), &Ships.Name)
			require.NoError(t, err)
			require.Equal(t, []string{"a"}, names)

			ship, err := session.Ships.IncludeOne(&Ships.Fleet).
				Where(ant.Eq(&Ships.Name, "a")).
				Single(ctx)
			require.NoError(t, err)

			ship.Fleet = nil

			require.NoError(t, session.SaveChanges(ctx))

			stored, err := db.Ships.NoTracking().Find(ctx, ship.ID)
			require.NoError(t, err)
			require.Equal(t, fleet.ID, *stored.FleetID)
		})
	})
}
