package relations

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/skudra-go/ant"
	"github.com/skudra-go/ant/dialect/postgres"
	"github.com/skudra-go/ant/tests/features/fixture"
)

// Two relations to one model cannot both be found by convention: both would
// take the same foreign key, so the second would load and delete the first
// one's rows. ant refuses to guess, and one of them names its key.
type Reporter struct {
	ID      int64
	Name    string
	Stories []Story
	Edited  []Story
}

func (Reporter) Configure(b *ant.ModelBuilder[Reporter]) {
	b.HasMany(&Reporters.Edited).ForeignKey(&Stories.EditorID)
}

type Story struct {
	ID         int64
	ReporterID int64
	EditorID   *int64
	Title      string
}

type NewsDB struct {
	ant.Context
	Reporters ant.Table[Reporter]
	Stories   ant.Table[Story]
}

var (
	Reporters = ant.Fields[Reporter]()
	Stories   = ant.Fields[Story]()
)

func TestTwoRelationsToOneModel(t *testing.T) {
	const ddl = `
		CREATE TABLE reporters (id BIGSERIAL PRIMARY KEY, name TEXT NOT NULL);
		CREATE TABLE stories (
			id BIGSERIAL PRIMARY KEY,
			reporter_id BIGINT NOT NULL REFERENCES reporters (id),
			editor_id BIGINT REFERENCES reporters (id),
			title TEXT NOT NULL
		)`

	fixture.RunWith(t, ddl, func(t *testing.T, db *NewsDB) {
		ctx := t.Context()
		ann, bob := &Reporter{Name: "ann"}, &Reporter{Name: "bob"}
		require.NoError(t, db.Reporters.Insert(ctx, ann))
		require.NoError(t, db.Reporters.Insert(ctx, bob))

		require.NoError(
			t,
			db.Stories.Insert(ctx, &Story{ReporterID: ann.ID, EditorID: &bob.ID, Title: "Tides"}),
		)

		s := ant.Session(db)

		t.Run("each relation reads its own key", func(t *testing.T) {
			found, err := s.Reporters.Include(&Reporters.Stories).
				Include(&Reporters.Edited).
				Where(ant.Eq(&Reporters.ID, ann.ID)).
				First(ctx)
			require.NoError(t, err)
			require.Len(t, found.Stories, 1)
			require.Empty(t, found.Edited)
		})

		t.Run("saving one leaves the other's rows", func(t *testing.T) {
			found, err := s.Reporters.Include(&Reporters.Edited).
				Where(ant.Eq(&Reporters.ID, ann.ID)).
				First(ctx)
			require.NoError(t, err)

			found.Edited = []Story{}

			require.NoError(t, s.SaveChanges(ctx))

			n, err := db.Stories.Count(ctx)
			require.NoError(t, err)
			require.EqualValues(t, 1, n)
		})
	})
}

// Neither Drafts nor Sent says which key it uses.
type Clerk struct {
	ID     int64
	Drafts []Memo
	Sent   []Memo
}

type Memo struct {
	ID      int64
	ClerkID int64
}

// A has-one and a has-many guessed onto one key.
type Curator struct {
	ID       int64
	Exhibits []Exhibit
	Featured *Exhibit
}

type Exhibit struct {
	ID        int64
	CuratorID int64
}

// The child names its key after the relation, and the parent has two slices
// that would both be found through it.
type Pilot struct {
	ID      int64
	Flights []Flight
	Reserve []Flight
}

type Flight struct {
	ID        int64
	CaptainID int64
	Captain   *Pilot
}

// A second relation that scopes or picks rows says on purpose that it shares
// the key.
type Shop struct {
	ID     int64
	Orders []Purchase
	Open   []Purchase
	Newest *Purchase
}

func (Shop) Configure(b *ant.ModelBuilder[Shop]) {
	b.HasMany(&Shops.Open).Where(ant.Eq(&Purchases.Open, true))
	b.HasOne(&Shops.Newest).Latest(&Purchases.ID)
}

type Purchase struct {
	ID     int64
	ShopID int64
	Open   bool
}

var (
	Clerks    = ant.Fields[Clerk]()
	Curators  = ant.Fields[Curator]()
	Pilots    = ant.Fields[Pilot]()
	Shops     = ant.Fields[Shop]()
	Purchases = ant.Fields[Purchase]()
)

func TestAmbiguousRelationsAreRefused(t *testing.T) {
	t.Run("two has-many", func(t *testing.T) {
		type DB struct {
			ant.Context
			Clerks ant.Table[Clerk]
		}

		_, err := ant.SchemaOf[DB](postgres.Dialect)
		require.ErrorIs(t, err, ant.ErrNoForeignKey)
		require.ErrorContains(t, err, "Clerk.Drafts")
		require.ErrorContains(t, err, "Clerk.Sent")
		require.ErrorContains(t, err, "Memo.ClerkID")

		// Every use fails, whichever relation it names.
		var table ant.Table[Clerk]

		_, _, err = table.Where(ant.Has(&Clerks.Drafts)).ToSQL()
		require.ErrorIs(t, err, ant.ErrNoForeignKey)
		_, _, err = table.Where(ant.Has(&Clerks.Sent)).ToSQL()
		require.ErrorIs(t, err, ant.ErrNoForeignKey)
	})

	t.Run("a has-one and a has-many", func(t *testing.T) {
		type DB struct {
			ant.Context
			Curators ant.Table[Curator]
		}

		_, err := ant.SchemaOf[DB](postgres.Dialect)
		require.ErrorIs(t, err, ant.ErrNoForeignKey)
		require.ErrorContains(t, err, "Curator.Featured")
	})

	t.Run("two has-many found through the child's relation", func(t *testing.T) {
		type DB struct {
			ant.Context
			Pilots ant.Table[Pilot]
		}

		_, err := ant.SchemaOf[DB](postgres.Dialect)
		require.ErrorIs(t, err, ant.ErrNoForeignKey)
		require.ErrorContains(t, err, "Flight.CaptainID")
	})

	t.Run("a scoped or picked relation shares the key on purpose", func(t *testing.T) {
		type DB struct {
			ant.Context
			Shops ant.Table[Shop]
		}

		_, err := ant.SchemaOf[DB](postgres.Dialect)
		require.NoError(t, err)
	})
}
