package migrations

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/skudra-go/ant"
)

// The models below are a badge with a slug at three points in time. The slug
// has everything that can depend on a column: a unique constraint, two checks,
// one of which spells its name in Unicode escapes, a comment, a foreign key from
// another table, and a unique index that includes it, which a foreign key on the
// label depends on.

// SlugV1 computes its slug from the label.
type SlugV1 struct {
	ID    int64
	Label string
	Slug  string
}

func (SlugV1) Configure(b *ant.ModelBuilder[SlugV1]) {
	b.Table("badges")
	b.Column(&SlugV1s.Slug).Generated("lower(label)").Unique().Comment("for URLs")
	b.Index(&SlugV1s.Label).Unique().Include(&SlugV1s.Slug)
	b.Check("badges_slug_check", "slug <> ''")
	b.Check("badges_slug_length_check", `length(U&"sl\0075g") < 100`)
}

// SlugV2 trims the label too.
type SlugV2 struct {
	ID    int64
	Label string
	Slug  string
}

func (SlugV2) Configure(b *ant.ModelBuilder[SlugV2]) {
	b.Table("badges")
	b.Column(&SlugV2s.Slug).Generated("lower(trim(label))").Unique().Comment("for URLs")
	b.Index(&SlugV2s.Label).Unique().Include(&SlugV2s.Slug)
	b.Check("badges_slug_check", "slug <> ''")
	b.Check("badges_slug_length_check", `length(U&"sl\0075g") < 100`)
}

// SlugV3 stores its slug, so it can be edited.
type SlugV3 struct {
	ID    int64
	Label string
	Slug  string
}

func (SlugV3) Configure(b *ant.ModelBuilder[SlugV3]) {
	b.Table("badges")
	b.Column(&SlugV3s.Slug).Unique().Comment("for URLs")
	b.Index(&SlugV3s.Label).Unique().Include(&SlugV3s.Slug)
	b.Check("badges_slug_check", "slug <> ''")
	b.Check("badges_slug_length_check", `length(U&"sl\0075g") < 100`)
}

// Award refers to a badge by its slug, and by its label.
type Award struct {
	ID         int64
	BadgeSlug  string
	BadgeLabel string
}

func (Award) Configure(b *ant.ModelBuilder[Award]) {
	b.ForeignKey(ant.F(&Awards.BadgeSlug)).References(ant.F(&SlugV1s.Slug))
	b.ForeignKey(ant.F(&Awards.BadgeLabel)).References(ant.F(&SlugV1s.Label))
}

// SlugV1DB is the application with SlugV1.
type SlugV1DB struct {
	ant.Context
	Badges ant.Table[SlugV1]
	Awards ant.Table[Award]
}

// SlugV2DB is the application with SlugV2.
type SlugV2DB struct {
	ant.Context
	Badges ant.Table[SlugV2]
	Awards ant.Table[Award]
}

// SlugV3DB is the application with SlugV3.
type SlugV3DB struct {
	ant.Context
	Badges ant.Table[SlugV3]
	Awards ant.Table[Award]
}

// Probes for the models.
var (
	SlugV1s = ant.Fields[SlugV1]()
	SlugV2s = ant.Fields[SlugV2]()
	SlugV3s = ant.Fields[SlugV3]()
	Awards  = ant.Fields[Award]()
)

// A new expression for a generated column means dropping the column and adding
// it again, which takes its indexes and constraints along. The migration makes
// them again, so the database still matches the models. A column that stops
// being generated keeps its values.
func TestGeneratedColumnChanges(t *testing.T) {
	each(t, func(t *testing.T, p *project) {
		must[SlugV1DB](p, "diff", "start")
		must[SlugV1DB](p, "up")
		p.sql("INSERT INTO badges (label) VALUES ('Gold'), (' Silver')")
		p.sql("INSERT INTO awards (badge_slug, badge_label) VALUES ('gold', 'Gold')")

		slug := func(label string) string {
			return scalar[string](p, "SELECT slug FROM badges WHERE label = $1", label)
		}

		t.Run("a new expression rebuilds what uses the column", func(t *testing.T) {
			must[SlugV2DB](p, "diff", "trim_slug")
			text := p.read("versions/0002_trim_slug.up.sql")
			require.Contains(
				t,
				text,
				`ALTER TABLE "awards" DROP CONSTRAINT "awards_badge_slug_fkey";`,
			)

			require.Contains(
				t,
				text,
				`ALTER TABLE "awards" DROP CONSTRAINT "awards_badge_label_fkey";`,
			)

			require.Contains(t, text, `ALTER TABLE "badges" DROP COLUMN "slug";`)

			require.Contains(
				t,
				text,
				`CREATE UNIQUE INDEX "badges_label_idx" ON "badges" ("label") INCLUDE ("slug");`,
			)

			require.Contains(
				t,
				text,
				`ALTER TABLE "badges" ADD CONSTRAINT "badges_slug_length_check" CHECK (length(U&"sl\0075g") < 100);`,
			)

			require.Contains(t, text, `COMMENT ON COLUMN "badges"."slug"`)

			must[SlugV2DB](p, "up")
			require.Equal(t, "silver", slug(" Silver"))
			require.Contains(t, must[SlugV2DB](p, "drift"), "No drift")
		})

		t.Run("a stored column keeps the values it had", func(t *testing.T) {
			must[SlugV3DB](p, "diff", "store_slug")

			require.Contains(
				t,
				p.read("versions/0003_store_slug.up.sql"),
				`ALTER TABLE "badges" ALTER COLUMN "slug" DROP EXPRESSION;`,
			)

			must[SlugV3DB](p, "up")
			require.Equal(t, "silver", slug(" Silver"))
			require.Contains(t, must[SlugV3DB](p, "drift"), "No drift")
		})

		t.Run("a column that becomes generated is computed again", func(t *testing.T) {
			p.sql("UPDATE badges SET slug = 'edited' WHERE label = ' Silver'")
			must[SlugV1DB](p, "diff", "generate_slug")
			must[SlugV1DB](p, "up")
			require.Equal(t, " silver", slug(" Silver"))
			require.Contains(t, must[SlugV1DB](p, "drift"), "No drift")
		})
	})
}
