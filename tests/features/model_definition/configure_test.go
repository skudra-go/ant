package model_definition

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/skudra-go/ant"
	"github.com/skudra-go/ant/internal/testdb"
	"github.com/skudra-go/ant/tests/features/fixture"
)

// Configure is optional and overrides conventions in typed Go. It runs once,
// when a context containing the model opens.
type Plan struct {
	Slug  string
	Title string
}

func (Plan) Configure(b *ant.ModelBuilder[Plan]) {
	b.Table("pricing_plans")
	b.Key(&Plans.Slug)
}

var Plans = ant.Fields[Plan]()

type PlanDB struct {
	ant.Context
	Plans ant.Table[Plan]
}

// Mistakes in Configure stop Open, so they surface at startup.
type EmptyTable struct{ ID int64 }

func (EmptyTable) Configure(b *ant.ModelBuilder[EmptyTable]) { b.Table("") }

type BadKey struct {
	ID    int64
	Owner *Plan
}

func (BadKey) Configure(b *ant.ModelBuilder[BadKey]) { b.Key(&BadKeys.Owner) }

var BadKeys = ant.Fields[BadKey]()

type WrongModelKey struct{ ID int64 }

func (WrongModelKey) Configure(b *ant.ModelBuilder[WrongModelKey]) { b.Key(&Plans.Slug) }

type NotAProbeKey struct{ ID int64 }

func (NotAProbeKey) Configure(b *ant.ModelBuilder[NotAProbeKey]) {
	var id int64
	b.Key(&id)
}

func openErr[DB any](t *testing.T, target testdb.Target) error {
	_, err := ant.Open[DB](t.Context(), target.Dialect, target.DSN)

	return err
}

func TestConfigure(t *testing.T) {
	ddl := `CREATE TABLE pricing_plans (slug TEXT PRIMARY KEY, title TEXT NOT NULL)`
	fixture.RunWith(t, ddl, func(t *testing.T, db *PlanDB) {
		require.NoError(t, db.Plans.Insert(t.Context(), &Plan{Slug: "pro", Title: "Pro"}))
		found, err := db.Plans.Find(t.Context(), "pro")
		require.NoError(t, err)
		require.Equal(t, "Pro", found.Title)
	})
}

func TestConfigureErrors(t *testing.T) {
	fixture.Each(t, func(t *testing.T, target testdb.Target) {
		t.Run("empty table name", func(t *testing.T) {
			err := openErr[struct {
				ant.Context
				Rows ant.Table[EmptyTable]
			}](t, target)
			require.ErrorContains(t, err, "empty table name")
		})

		t.Run("a relation as the key", func(t *testing.T) {
			err := openErr[struct {
				ant.Context
				Rows ant.Table[BadKey]
			}](t, target)
			require.ErrorIs(t, err, ant.ErrNotColumn)
		})

		t.Run("a key from another model", func(t *testing.T) {
			err := openErr[struct {
				ant.Context
				Rows ant.Table[WrongModelKey]
			}](t, target)
			require.ErrorIs(t, err, ant.ErrWrongModel)
		})

		t.Run("a key that is not a probe", func(t *testing.T) {
			err := openErr[struct {
				ant.Context
				Rows ant.Table[NotAProbeKey]
			}](t, target)
			require.ErrorIs(t, err, ant.ErrNotProbe)
		})
	})
}
