package developer_tooling

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/skudra-go/ant"
	"github.com/skudra-go/ant/dialect/postgres"
	"github.com/skudra-go/ant/internal/testdb"
	"github.com/skudra-go/ant/tests/features/fixture"
)

// GORM's tags are not ant's: ant takes the key from a field named ID, sets
// column types in Configure, and writes its tag values with =.
type GormKey struct {
	Code string `ant:"primaryKey"`
}

type GormSize struct {
	ID   int64
	Name string `ant:"size:100"`
}

type GormColumn struct {
	ID   int64
	Name string `ant:"column:full_name"`
}

// GORM separates its keys with semicolons, ant with commas.
type GormKeys struct {
	ID   int64
	Name string `ant:"size:100;not null"`
}

// ant's keys are lower case.
type CasedKey struct {
	ID   int64
	Name string `ant:"readOnly"`
}

// A Writer's novels name their writer AuthorID, which convention does not find.
type Novel struct {
	ID       int64
	AuthorID int64
}

type Writer struct {
	ID     int64
	Novels []Novel
}

// A Pilot's plane names its owner OwnerID.
type Plane struct {
	ID      int64
	OwnerID int64
}

type Pilot struct {
	ID    int64
	Plane *Plane
}

// Badge spells its key Id, which is not taken as the key.
type Badge struct {
	Id    int64 //nolint:revive // the point of the test
	Label string
}

type BadgeDB struct {
	ant.Context
	Badges ant.Table[Badge]
}

func opened[DB any](t *testing.T) error {
	t.Helper()

	_, err := ant.Wrap[DB](postgres.Dialect, nil)

	return err
}

// The mistakes of a first day with ant, many of them habits from GORM, get an
// error that says what ant looked for and what to write instead, not only what
// went wrong (#301).
func TestFirstDayMistakesSayWhatToDo(t *testing.T) {
	for _, c := range []struct {
		name string
		err  func(*testing.T) error
		is   error
		want []string
	}{
		{
			"GORM's primaryKey tag",
			opened[struct {
				ant.Context
				T ant.Table[GormKey]
			}],
			ant.ErrBadSchema,
			[]string{
				`GormKey.Code: unknown tag key "primaryKey"`,
				"a field named ID is the key",
				"b.Key",
			},
		},
		{
			"GORM's size tag",
			opened[struct {
				ant.Context
				T ant.Table[GormSize]
			}],
			ant.ErrBadSchema,
			[]string{`unknown tag key "size:100"`, "b.Column(&...Name).Length(100)"},
		},
		{
			"a tag value given with a colon",
			opened[struct {
				ant.Context
				T ant.Table[GormColumn]
			}],
			ant.ErrBadSchema,
			[]string{`unknown tag key "column:full_name"`, "column=full_name"},
		},
		{
			"GORM's semicolons",
			opened[struct {
				ant.Context
				T ant.Table[GormKeys]
			}],
			ant.ErrBadSchema,
			[]string{"Length(100);", "ant separates tag keys with commas"},
		},
		{
			"a key in the wrong case",
			opened[struct {
				ant.Context
				T ant.Table[CasedKey]
			}],
			ant.ErrBadSchema,
			[]string{`unknown tag key "readOnly"`, "write readonly, in lower case"},
		},
		{
			"an unknown tag key lists the keys there are",
			opened[struct {
				ant.Context
				T ant.Table[GormColumn]
			}],
			ant.ErrBadSchema,
			[]string{"unique", "readonly", "default=", "column="},
		},
		{
			"a has-many whose key convention does not find",
			opened[struct {
				ant.Context
				T ant.Table[Writer]
			}],
			ant.ErrNoForeignKey,
			[]string{"Writer.Novels", "looked for Novel.WriterID", ".ForeignKey("},
		},
		{
			"a has-one whose key convention does not find",
			opened[struct {
				ant.Context
				T ant.Table[Pilot]
			}],
			ant.ErrNoForeignKey,
			[]string{"Pilot.Plane", "looked for Pilot.PlaneID and Plane.PilotID", ".ForeignKey("},
		},
		{
			"a pointer to the context's type",
			opened[*BadgeDB],
			ant.ErrNoContext,
			[]string{"*developer_tooling.BadgeDB is a pointer", "Open[developer_tooling.BadgeDB]"},
		},
		{
			"a type that is no context",
			opened[Badge],
			ant.ErrNoContext,
			[]string{"developer_tooling.Badge: Open takes a struct type that embeds ant.Context"},
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			err := c.err(t)
			require.ErrorIs(t, err, c.is)

			for _, want := range c.want {
				require.ErrorContains(t, err, want)
			}
		})
	}

	t.Run("a key spelled Id", func(t *testing.T) {
		fixture.Each(t, func(t *testing.T, target testdb.Target) {
			db := fixture.Open[BadgeDB](t, target)

			_, err := db.Badges.Find(t.Context(), 1)
			require.ErrorIs(t, err, ant.ErrNoKey)
			require.ErrorContains(t, err, "Badge.Id is not the key")
			require.ErrorContains(t, err, "name it ID")
		})
	})

	t.Run("a write with nothing to write", func(t *testing.T) {
		fixture.Run(t, func(t *testing.T, db *fixture.AppDB) {
			ctx := t.Context()
			alice, _, _ := fixture.Seed(t, db)

			_, err := db.Users.UpdateMany(ctx, []*fixture.User{alice})
			require.ErrorIs(t, err, ant.ErrNoAssignment)
			require.ErrorContains(t, err, "name the fields to write")

			_, err = db.Users.Where(ant.Eq(&Users.ID, alice.ID)).Update(ctx)
			require.ErrorIs(t, err, ant.ErrNoAssignment)
			require.ErrorContains(t, err, "ant.Set")
		})
	})
}
