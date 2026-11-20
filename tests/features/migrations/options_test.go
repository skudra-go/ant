package migrations

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/skudra-go/ant"
	"github.com/skudra-go/ant/dialect/postgres"
	"github.com/skudra-go/ant/tests/features/fixture"
)

// NamedItem is used by this test alone, so the naming it gets stays with it.
type NamedItem struct {
	ID       int64
	FullName string
}

// NamedItems is NamedItem's probe.
var NamedItems = ant.Fields[NamedItem]()

// NamedDB holds it.
type NamedDB struct {
	ant.Context
	Items ant.Table[NamedItem]
}

// The migrate program takes the options the application opens with, so a naming
// strategy names the tables and columns of migrations as the application queries
// them.
func TestMigrateOptions(t *testing.T) {
	fixture.Serial(t) // the naming is process-wide while it is set

	naming := ant.WithNaming(ant.Naming{
		Table:  func(name string) string { return "tbl_" + strings.ToLower(name) },
		Column: func(name string) string { return "col_" + strings.ToLower(name) },
	})

	// Metadata is global, so the test leaves things as it found them.
	t.Cleanup(func() {
		_, err := ant.SchemaOf[NamedDB](postgres.Dialect, ant.WithNaming(ant.Naming{}))
		require.NoError(t, err)
	})

	each(t, func(t *testing.T, p *project) {
		p.config.Options = []ant.Option{naming}

		out := must[NamedDB](p, "diff", "create_items")
		require.Contains(t, out, "0001_create_items")

		file := p.read("versions/0001_create_items.up.sql")
		require.Contains(t, file, `CREATE TABLE "tbl_nameditem"`)
		require.Contains(t, file, `"col_fullname"`)

		must[NamedDB](p, "up")
		require.Contains(t, must[NamedDB](p, "drift"), "No drift")

		db := fixture.Open[NamedDB](t, p.target, naming)
		item := &NamedItem{FullName: "Ada"}
		require.NoError(t, db.Items.Insert(t.Context(), item))

		loaded, err := db.Items.Where(ant.Eq(&NamedItems.FullName, "Ada")).First(t.Context())
		require.NoError(t, err)
		require.Equal(t, item.ID, loaded.ID)
	})
}
