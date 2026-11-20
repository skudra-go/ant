package model_definition

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/skudra-go/ant"
	"github.com/skudra-go/ant/dialect/postgres"
)

// A model ant would misread is refused when the context opens, with an error
// that names the field, rather than giving a schema that is silently wrong or a
// failure on the first request that touches it.

// A misspelled tag key is no key at all: Email would have no unique constraint
// (#98).
type Typo struct {
	ID    int64
	Email string `ant:"uniqe"`
}

type ValuedFlag struct {
	ID    int64
	Email string `ant:"unique=false"`
}

type EmptyColumn struct {
	ID   int64
	Name string `ant:"column="`
}

type StrayPrefix struct {
	ID   int64
	Name string `ant:"prefix=x_"`
}

// Two fields that map to one column: one of them was never read or written
// (#99).
type TwoIDs struct {
	ID     int64
	UserID int64
	UserId int64 //nolint:revive // the point of the test
}

type Named struct{ Name string }

type Labelled struct{ Name string }

type TwoNames struct {
	ID int64
	Named
	Labelled
}

// A slice of pointers to a model is not a relation, and an embedded pointer is
// not an embedded struct (#204).
type Entry struct {
	ID     int64
	BlogID int64
	Title  string
}

type Blog struct {
	ID    int64
	Posts []*Entry
}

type Stamps struct {
	ID int64
}

type ByPointer struct {
	*Stamps
	Name string
}

// A field no database can store, and relations that can't be resolved: each
// passed Open and failed on first use (#203).
type Hooked struct {
	ID     int64
	OnSave func()
}

type Member struct {
	ID      int64
	SquadID int64
}

type Team struct {
	ID      int64
	Members []Member
}

type Group struct {
	ID      int64
	Members []Member
}

func (Group) Configure(b *ant.ModelBuilder[Group]) { b.ManyToMany(&Groups.Members) }

type Person struct {
	ID      int64
	Friends []Person
}

func (Person) Configure(b *ant.ModelBuilder[Person]) {
	b.ManyToMany(&Persons.Friends).Through("friendships")
}

var (
	Groups  = ant.Fields[Group]()
	Persons = ant.Fields[Person]()
)

func open[DB any](t *testing.T) error {
	t.Helper()

	_, err := ant.Wrap[DB](postgres.Dialect, nil)

	return err
}

func TestMisreadModelsAreRefused(t *testing.T) {
	for _, c := range []struct {
		name string
		open func(*testing.T) error
		is   error
		want string
	}{
		{
			"a misspelled tag key",
			open[struct {
				ant.Context
				T ant.Table[Typo]
			}],
			ant.ErrBadSchema,
			`Typo.Email: unknown tag key "uniqe"`,
		},
		{
			"a value on a key that takes none",
			open[struct {
				ant.Context
				T ant.Table[ValuedFlag]
			}],
			ant.ErrBadSchema,
			"ValuedFlag.Email: tag key unique takes no value",
		},
		{
			"a key with no value",
			open[struct {
				ant.Context
				T ant.Table[EmptyColumn]
			}],
			ant.ErrBadSchema,
			"EmptyColumn.Name: tag key column needs a value",
		},
		{
			"a prefix with nothing embedded",
			open[struct {
				ant.Context
				T ant.Table[StrayPrefix]
			}],
			ant.ErrBadSchema,
			"StrayPrefix.Name: prefix= goes with embedded",
		},
		{
			"two fields on one column",
			open[struct {
				ant.Context
				T ant.Table[TwoIDs]
			}],
			ant.ErrBadSchema,
			"TwoIDs.UserID and TwoIDs.UserId both map to column user_id",
		},
		{
			"two embedded structs with a field of the same name",
			open[struct {
				ant.Context
				T ant.Table[TwoNames]
			}],
			ant.ErrBadSchema,
			"TwoNames.Named.Name and TwoNames.Labelled.Name both map to column name",
		},
		{
			"a slice of pointers to a model",
			open[struct {
				ant.Context
				T ant.Table[Blog]
			}],
			ant.ErrBadSchema,
			"Blog.Posts: a relation slice holds values, as []Entry, not pointers",
		},
		{
			"a struct embedded by pointer",
			open[struct {
				ant.Context
				T ant.Table[ByPointer]
			}],
			ant.ErrBadSchema,
			"ByPointer.Stamps: an embedded struct must be a value: embed Stamps",
		},
		{
			"a func field",
			open[struct {
				ant.Context
				T ant.Table[Hooked]
			}],
			ant.ErrBadSchema,
			`Hooked.OnSave: a func is no column: tag it ant:"-"`,
		},
		{
			"a relation with no foreign key",
			open[struct {
				ant.Context
				T ant.Table[Team]
			}],
			ant.ErrNoForeignKey,
			"Team.Members",
		},
		{
			"a many-to-many with no join table",
			open[struct {
				ant.Context
				T ant.Table[Group]
			}],
			ant.ErrBadSchema,
			"Group.Members is many-to-many with no join table",
		},
		{
			"a many-to-many on its own model with one column for both sides",
			open[struct {
				ant.Context
				T ant.Table[Person]
			}],
			ant.ErrBadSchema,
			"Person.Friends uses person_id for both sides of its join table",
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			err := c.open(t)
			require.ErrorIs(t, err, c.is)
			require.ErrorContains(t, err, c.want)
		})
	}

	t.Run("every problem at once", func(t *testing.T) {
		err := open[struct {
			ant.Context
			A ant.Table[Typo]
			B ant.Table[Blog]
			C ant.Table[Team]
		}](t)
		require.ErrorContains(t, err, "Typo.Email")
		require.ErrorContains(t, err, "Blog.Posts")
		require.ErrorContains(t, err, "Team.Members")
	})
}

// A relation a model inherits from one it embeds keeps the key its own model's
// conventions give it, so the embedding model opens and loads it.
type Squad struct {
	ID      int64
	Members []Member
}

type NamedSquad struct {
	Squad
	Motto string
}

func (NamedSquad) Configure(b *ant.ModelBuilder[NamedSquad]) { b.Table("squads") }

var NamedSquads = ant.Fields[NamedSquad]()

func TestInheritedRelationKeepsItsKey(t *testing.T) {
	db, err := ant.Wrap[struct {
		ant.Context
		Squads ant.Table[NamedSquad]
	}](postgres.Dialect, nil)
	require.NoError(t, err)

	text, _, err := db.Squads.Where(ant.Has(&NamedSquads.Members)).ToSQL()
	require.NoError(t, err)
	require.Contains(t, text, `"squad_id"`)
}

// Default names join columns with underscores, so an index on a_b and one on
// (a, b) were both t_a_b_idx, and the second CREATE INDEX failed when the
// migration ran (#112). Naming one of them settles it.
type Pair struct {
	ID int64
	A  string
	B  string
	AB string `ant:"column=a_b"`
}

func (Pair) Configure(b *ant.ModelBuilder[Pair]) {
	b.Index(&Pairs.AB)
	b.Index(&Pairs.A).And(&Pairs.B)
}

type NamedPair struct {
	ID int64
	A  string
	B  string
	AB string `ant:"column=a_b"`
}

func (NamedPair) Configure(b *ant.ModelBuilder[NamedPair]) {
	b.Table("pairs")
	b.Index(&NamedPairs.AB)
	b.Index(&NamedPairs.A).And(&NamedPairs.B).Name("pairs_a_then_b_idx")
}

var (
	Pairs      = ant.Fields[Pair]()
	NamedPairs = ant.Fields[NamedPair]()
)

func TestDefaultNamesThatCollideAreRefused(t *testing.T) {
	_, err := ant.SchemaOf[struct {
		ant.Context
		Pairs ant.Table[Pair]
	}](postgres.Dialect)
	require.ErrorIs(t, err, ant.ErrBadSchema)
	require.ErrorContains(
		t,
		err,
		"the index on pairs (a_b) and the index on pairs (a, b) are both named pairs_a_b_idx",
	)

	_, err = ant.SchemaOf[struct {
		ant.Context
		Pairs ant.Table[NamedPair]
	}](postgres.Dialect)
	require.NoError(t, err)
}
