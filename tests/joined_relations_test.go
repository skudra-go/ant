package tests

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/skudra-go/ant"
	"github.com/skudra-go/ant/internal/testdb"
)

// JrEssay owns notes through a polymorphic key, as JrVideo does.
type JrEssay struct {
	ID       int64
	Title    string
	LastNote *JrNote
	Heavy    *JrNote
	Top      *JrNote
	LastTag  *JrTag
}

func (JrEssay) Configure(b *ant.ModelBuilder[JrEssay]) {
	b.Table("jr_essays")

	b.HasOne(&JrEssays.LastNote).
		ForeignKey(&JrNotes.OwnerID).
		Morph(&JrNotes.OwnerType, "essay").
		Latest(&JrNotes.ID)

	b.HasOne(&JrEssays.Heavy).
		ForeignKey(&JrNotes.OwnerID).
		Morph(&JrNotes.OwnerType, "essay").
		Where(ant.Gt(&JrNotes.Weight, int64(5)))

	b.HasOne(&JrEssays.Top).
		ForeignKey(&JrNotes.OwnerID).
		Morph(&JrNotes.OwnerType, "essay").
		Latest(&JrNotes.Weight)

	b.HasOne(&JrEssays.LastTag).ForeignKey(&JrTags.EssayID).Latest(&JrTags.Name)
}

// JrTag has a composite key, so a picked has-one of it can't name one row.
type JrTag struct {
	EssayID int64
	Name    string
}

func (JrTag) Configure(b *ant.ModelBuilder[JrTag]) {
	b.Table("jr_tags")
	b.CompositeKey(ant.F(&JrTags.EssayID), ant.F(&JrTags.Name))
}

// JrVideo is the other kind of owner.
type JrVideo struct {
	ID    int64
	Title string
}

func (JrVideo) Configure(b *ant.ModelBuilder[JrVideo]) { b.Table("jr_videos") }

// JrNote belongs to an essay or a video, which OwnerType says.
type JrNote struct {
	ID         int64
	OwnerType  string
	OwnerID    int64
	Weight     int64
	ParentType string
	ParentID   *int64
	Essay      *JrEssay
	Video      *JrVideo
	Parent     *JrNote
}

func (JrNote) Configure(b *ant.ModelBuilder[JrNote]) {
	b.Table("jr_notes")
	b.BelongsTo(&JrNotes.Essay).ForeignKey(&JrNotes.OwnerID).Morph(&JrNotes.OwnerType, "essay")
	b.BelongsTo(&JrNotes.Video).ForeignKey(&JrNotes.OwnerID).Morph(&JrNotes.OwnerType, "video")
	b.BelongsTo(&JrNotes.Parent).ForeignKey(&JrNotes.ParentID).Morph(&JrNotes.ParentType, "note")
}

// JrDB holds the models of the joined relation tests.
type JrDB struct {
	ant.Context
	JrEssays ant.Table[JrEssay]
	JrVideos ant.Table[JrVideo]
	JrNotes  ant.Table[JrNote]
	JrTags   ant.Table[JrTag]
}

var (
	JrEssays = ant.Fields[JrEssay]()
	JrVideos = ant.Fields[JrVideo]()
	JrNotes  = ant.Fields[JrNote]()
	JrTags   = ant.Fields[JrTag]()
)

// IncludeJoined applies the relation's own conditions, as IncludeOne does: the
// polymorphic type column (#310), a Where scope, and a Latest pick, which
// otherwise returned each parent once per child.
func TestConformanceIncludeJoinedRelationConditions(t *testing.T) {
	testdb.ForEach(t, func(t *testing.T, target testdb.Target) {
		ctx := t.Context()

		jrTables(t, target)

		db, err := ant.Open[JrDB](ctx, target.Dialect, target.DSN, ant.WithTracking(false))
		require.NoError(t, err)
		t.Cleanup(func() { db.Close() })

		require.NoError(
			t,
			db.JrEssays.Insert(ctx, &JrEssay{ID: 1, Title: "e1"}, &JrEssay{ID: 2, Title: "e2"}),
		)

		require.NoError(t, db.JrVideos.Insert(ctx, &JrVideo{ID: 1, Title: "v1"}))

		one := int64(1)
		require.NoError(t, db.JrNotes.Insert(
			ctx,
			&JrNote{
				ID:        1,
				OwnerType: "essay",
				OwnerID:   1,
				Weight:    1,
			},
			&JrNote{
				ID:         3,
				OwnerType:  "essay",
				OwnerID:    1,
				Weight:     3,
				ParentType: "note",
				ParentID:   &one,
			},
			&JrNote{
				ID:         4,
				OwnerType:  "essay",
				OwnerID:    2,
				Weight:     7,
				ParentType: "essay",
				ParentID:   &one,
			},
			&JrNote{
				ID:        5,
				OwnerType: "video",
				OwnerID:   1,
				Weight:    9,
			},
		))

		t.Run("morph belongs-to", func(t *testing.T) {
			notes, err := db.JrNotes.OrderBy(&JrNotes.ID).
				IncludeJoined(&JrNotes.Essay).
				IncludeJoined(&JrNotes.Video).
				All(ctx)
			require.NoError(t, err)

			essays := map[int64]string{}
			videos := map[int64]string{}

			for _, n := range notes {
				if n.Essay != nil {
					essays[n.ID] = n.Essay.Title
				}

				if n.Video != nil {
					videos[n.ID] = n.Video.Title
				}
			}

			require.Equal(
				t,
				map[int64]string{1: "e1", 3: "e1", 4: "e2"},
				essays,
				"note 5 is on a video",
			)

			require.Equal(t, map[int64]string{5: "v1"}, videos, "notes 1, 3 and 4 are on essays")

			want, err := db.JrNotes.OrderBy(&JrNotes.ID).
				IncludeOne(&JrNotes.Essay).
				IncludeOne(&JrNotes.Video).
				All(ctx)
			require.NoError(t, err)
			require.Equal(t, want, notes, "as IncludeOne loads them")
		})

		t.Run("morph belongs-to of its own table", func(t *testing.T) {
			notes, err := db.JrNotes.OrderBy(&JrNotes.ID).IncludeJoined(&JrNotes.Parent).All(ctx)
			require.NoError(t, err)
			require.Len(t, notes, 4)

			require.NotNil(
				t,
				notes[1].Parent,
				"note 3's type column is on the note itself, not on its parent",
			)

			require.Equal(t, int64(1), notes[1].Parent.ID)
			require.Nil(t, notes[2].Parent, "note 4's parent is an essay")

			want, err := db.JrNotes.OrderBy(&JrNotes.ID).IncludeOne(&JrNotes.Parent).All(ctx)
			require.NoError(t, err)
			require.Equal(t, want, notes)
		})

		t.Run("picked has-one with a morph", func(t *testing.T) {
			essays, err := db.JrEssays.OrderBy(&JrEssays.ID).
				IncludeJoined(&JrEssays.LastNote).
				All(ctx)
			require.NoError(t, err)
			require.Len(t, essays, 2, "each essay once, not once per note")
			require.NotNil(t, essays[0].LastNote)

			require.Equal(
				t,
				int64(3),
				essays[0].LastNote.ID,
				"the newest essay note, not video note 5",
			)

			require.NotNil(t, essays[1].LastNote)
			require.Equal(t, int64(4), essays[1].LastNote.ID)

			want, err := db.JrEssays.OrderBy(&JrEssays.ID).IncludeOne(&JrEssays.LastNote).All(ctx)
			require.NoError(t, err)
			require.Equal(t, want, essays)
		})

		t.Run("has-one picked by another column", func(t *testing.T) {
			essays, err := db.JrEssays.OrderBy(&JrEssays.ID).IncludeJoined(&JrEssays.Top).All(ctx)
			require.NoError(t, err)
			require.Len(t, essays, 2)
			require.NotNil(t, essays[0].Top)

			require.Equal(
				t,
				int64(3),
				essays[0].Top.ID,
				"the heaviest essay note, not video note 5",
			)

			want, err := db.JrEssays.OrderBy(&JrEssays.ID).IncludeOne(&JrEssays.Top).All(ctx)
			require.NoError(t, err)
			require.Equal(t, want, essays)
		})

		t.Run("picked has-one of a composite key", func(t *testing.T) {
			_, err := db.JrEssays.IncludeJoined(&JrEssays.LastTag).All(ctx)
			require.ErrorIs(t, err, ant.ErrNotImplemented)
			require.ErrorContains(t, err, "JrEssay.LastTag")
			require.ErrorContains(t, err, "use IncludeOne")
		})

		t.Run("scoped has-one", func(t *testing.T) {
			essays, err := db.JrEssays.OrderBy(&JrEssays.ID).IncludeJoined(&JrEssays.Heavy).All(ctx)
			require.NoError(t, err)
			require.Len(t, essays, 2)
			require.Nil(t, essays[0].Heavy, "no essay note of essay 1 weighs over 5")
			require.NotNil(t, essays[1].Heavy)
			require.Equal(t, int64(4), essays[1].Heavy.ID)

			want, err := db.JrEssays.OrderBy(&JrEssays.ID).IncludeOne(&JrEssays.Heavy).All(ctx)
			require.NoError(t, err)
			require.Equal(t, want, essays)
		})
	})
}

// jrTables creates the tables of the Jr models, and drops them after the test.
func jrTables(t *testing.T, target testdb.Target) {
	t.Helper()

	ctx := t.Context()

	for _, ddl := range []string{
		`CREATE TABLE jr_essays (id BIGINT PRIMARY KEY, title VARCHAR(50) NOT NULL)`,
		`CREATE TABLE jr_videos (id BIGINT PRIMARY KEY, title VARCHAR(50) NOT NULL)`,
		`CREATE TABLE jr_notes (id BIGINT PRIMARY KEY, owner_type VARCHAR(20) NOT NULL, owner_id BIGINT NOT NULL, weight BIGINT NOT NULL, parent_type VARCHAR(20) NOT NULL, parent_id BIGINT NULL)`,
	} {
		_, err := target.DB.ExecContext(ctx, ddl)
		require.NoError(t, err)
	}

	t.Cleanup(func() {
		for _, table := range []string{"jr_notes", "jr_videos", "jr_essays"} {
			_, _ = target.DB.ExecContext(context.Background(), "DROP TABLE "+table)
		}
	})
}
