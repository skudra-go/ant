package lifecycle

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/skudra-go/ant"
	"github.com/skudra-go/ant/tests/features/fixture"
	"github.com/skudra-go/ant/validate"
)

// Signup has rules on its fields.
type Signup struct {
	ID    int64
	Name  string
	Email string
	Age   int
}

func (Signup) Configure(b *ant.ModelBuilder[Signup]) {
	b.Validate(&Signups.Name, validate.All(validate.Required[string](), validate.MaxLength(5)))
	b.Validate(&Signups.Email, validate.Email)
	b.Validate(&Signups.Age, validate.Range(0, 150))
}

// Signups is Signup's probe.
var Signups = ant.Fields[Signup]()

// ValidDB holds it.
type ValidDB struct {
	ant.Context
	Signups ant.Table[Signup]
}

const validDDL = `CREATE TABLE signups (id BIGSERIAL PRIMARY KEY, name TEXT NOT NULL, email TEXT NOT NULL, age INT NOT NULL)`

// Rules check a field before a write, with an error that names it.
func TestFieldRules(t *testing.T) {
	fixture.RunTrackedWith(t, validDDL, func(t *testing.T, db *ValidDB) {
		ctx := t.Context()

		t.Run("a good row goes in", func(t *testing.T) {
			require.NoError(
				t,
				db.Signups.Insert(ctx, &Signup{Name: "Ada", Email: "a@example.com", Age: 30}),
			)
		})

		t.Run("a bad row does not, and the error names the field", func(t *testing.T) {
			err := db.Signups.Insert(ctx, &Signup{Name: "", Email: "a@example.com"})

			var invalid *ant.ValidationError
			require.ErrorAs(t, err, &invalid)
			require.Equal(t, "Name", invalid.Field)
			require.ErrorIs(t, err, validate.ErrRequired)
		})

		t.Run("updates are checked too", func(t *testing.T) {
			row, err := db.Signups.First(ctx)
			require.NoError(t, err)

			row.Age = 999
			err = db.SaveChanges(ctx)
			require.ErrorContains(t, err, "Age")

			row.Age = 31

			require.NoError(t, db.SaveChanges(ctx))
		})

		t.Run("ant.WithoutHooks skips them, as seeding wants", func(t *testing.T) {
			require.NoError(
				t,
				db.Signups.Insert(ant.WithoutHooks(ctx), &Signup{Name: "toolongname", Email: "x"}),
			)
		})
	})
}

// Enriched computes something from its relations, so it needs them loaded.
type Enriched struct {
	fixture.User
	Summary string `ant:"-"`
}

func (Enriched) Configure(b *ant.ModelBuilder[Enriched]) { b.Table("users") }

// AfterInclude runs once relations are in.
func (e *Enriched) AfterInclude(context.Context) error {
	e.Summary = e.Name + " is ready"

	if e.Name == "refuse" {
		return errors.New("refused")
	}

	return nil
}

// HookDB holds it.
type HookDB struct {
	ant.Context
	Users ant.Table[Enriched]
}

// AfterInclude runs after relations load, where AfterLoad runs before.
func TestAfterInclude(t *testing.T) {
	fixture.Each(t, func(t *testing.T, target fixture.Target) {
		ctx := t.Context()
		seed := fixture.Open[fixture.AppDB](t, target)
		fixture.Seed(t, seed)
		db := fixture.Open[HookDB](t, target)
		row, err := db.Users.First(ctx)
		require.NoError(t, err)
		require.Contains(t, row.Summary, "is ready")
	})
}

// Prune removes old rows for good, in batches, soft-deleted or not.
func TestPrune(t *testing.T) {
	fixture.Run(t, func(t *testing.T, db *fixture.AppDB) {
		ctx := t.Context()
		fixture.Seed(t, db)

		cutoff := time.Now().Add(time.Hour)
		removed, err := db.Users.Prune(ctx, ant.Lt(&fixture.Users.CreatedAt, cutoff))
		require.NoError(t, err)
		require.EqualValues(t, 3, removed)

		count, err := db.Users.Count(ctx)
		require.NoError(t, err)
		require.Zero(t, count)
	})
}
