package lifecycle

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/skudra-go/ant"
	"github.com/skudra-go/ant/tests/features/fixture"
)

// CreatedAt and UpdatedAt fields are maintained for you, on any model.
func TestTimestamps(t *testing.T) {
	fixture.Each(t, func(t *testing.T, target fixture.Target) {
		ctx := t.Context()
		_, err := target.DB.ExecContext(ctx, notesDDL)
		require.NoError(t, err)

		now := time.Date(2026, 1, 1, 12, 0, 0, 123456789, time.UTC)
		db := fixture.Open[NotesDB](
			t,
			target,
			ant.WithTracking(true),
			ant.WithNow(func() time.Time { return now }),
		)
		stored := func(id int64) *Author {
			a, err := db.Authors.NoTracking().Find(ctx, id)
			require.NoError(t, err)

			return a
		}

		tick := func() { now = now.Add(time.Hour) }

		ann := &Author{Name: "Ann"}
		require.NoError(t, db.Authors.Insert(ctx, ann))

		t.Run("insert sets both, at database precision", func(t *testing.T) {
			require.True(t, ann.CreatedAt.Equal(now.Truncate(time.Microsecond)))
			require.True(t, ann.UpdatedAt.Equal(ann.CreatedAt))

			require.True(
				t,
				stored(ann.ID).CreatedAt.Equal(ann.CreatedAt),
				"what is stored matches what you hold",
			)
		})

		t.Run("SaveChanges moves UpdatedAt only", func(t *testing.T) {
			created := ann.CreatedAt

			tick()

			ann.Name = "Anna"

			require.NoError(t, db.SaveChanges(ctx))

			got := stored(ann.ID)
			require.True(t, got.CreatedAt.Equal(created))
			require.True(t, got.UpdatedAt.Equal(now.Truncate(time.Microsecond)))
			require.True(t, ann.UpdatedAt.Equal(got.UpdatedAt))
		})

		t.Run("no change leaves it alone", func(t *testing.T) {
			before := stored(ann.ID).UpdatedAt

			tick()
			require.NoError(t, db.SaveChanges(ctx))
			require.True(t, stored(ann.ID).UpdatedAt.Equal(before))
		})

		t.Run("a bulk update moves it", func(t *testing.T) {
			tick()

			_, err := db.Authors.Where(ant.Eq(&A.ID, ann.ID)).Update(ctx, ant.Set(&A.Name, "Ann"))
			require.NoError(t, err)
			require.True(t, stored(ann.ID).UpdatedAt.Equal(now.Truncate(time.Microsecond)))
		})

		t.Run("an explicit assignment wins", func(t *testing.T) {
			at := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
			_, err := db.Authors.Where(ant.Eq(&A.ID, ann.ID)).Update(ctx, ant.Set(&A.UpdatedAt, at))
			require.NoError(t, err)
			require.True(t, stored(ann.ID).UpdatedAt.Equal(at))
		})

		t.Run("InsertMany and Upsert set them", func(t *testing.T) {
			a := &Author{Name: "a"}
			_, err := db.Authors.InsertMany(ctx, []*Author{a})
			require.NoError(t, err)
			require.False(t, stored(a.ID).CreatedAt.IsZero())
		})

		// Merge keeps them as Insert and Update do, and a value it is given wins.
		t.Run("Merge sets them", func(t *testing.T) {
			tick()

			owner := &Author{Name: "owner"}
			require.NoError(t, db.Authors.Insert(ctx, owner))
			require.NoError(t, db.Notes.Insert(ctx, &Note{AuthorID: owner.ID, Text: "Ann"}))
			require.NoError(t, db.Notes.Insert(ctx, &Note{AuthorID: owner.ID, Text: "Bo"}))

			tick()

			n, err := ant.Merge(&db.Authors, db.Notes.Query(), ant.EqCol(&A.Name, &N.Text)).
				WhenMatched(ant.SetExpr(&A.Name, ant.Col(&N.Text))).
				WhenNotMatched(ant.Into(&A.Name, &N.Text)).
				Exec(ctx)
			require.NoError(t, err)
			require.EqualValues(t, 2, n)

			at := now.Truncate(time.Microsecond)
			require.True(t, stored(ann.ID).UpdatedAt.Equal(at), "an update moves UpdatedAt")

			bo, err := db.Authors.NoTracking().Where(ant.Eq(&A.Name, "Bo")).Single(ctx)
			require.NoError(t, err)
			require.True(t, bo.CreatedAt.Equal(at), "an insert sets CreatedAt")
			require.True(t, bo.UpdatedAt.Equal(at), "and UpdatedAt")

			given := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
			_, err = ant.Merge(&db.Authors, db.Notes.Query(), ant.EqCol(&A.Name, &N.Text)).
				WhenMatched(ant.Set(&A.UpdatedAt, given)).
				Exec(ctx)
			require.NoError(t, err)
			require.True(t, stored(ann.ID).UpdatedAt.Equal(given))

			_, err = ant.Merge(&db.Authors, db.Notes.Query(), ant.EqCol(&A.Name, &N.Text)).
				WhenMatched(ant.Set(&A.Name, "x"), ant.Set(&A.Name, "y")).
				Exec(ctx)
			require.ErrorContains(t, err, "Author.Name has two assignments")

			_, err = ant.Merge(&db.Authors, db.Notes.Query(), ant.EqCol(&A.Name, &N.Text)).
				WhenNotMatched(ant.Into(&A.Name, &N.Text), ant.Into(&A.Name, &N.Text)).
				Exec(ctx)
			require.ErrorContains(t, err, "Author.Name has two mappings")
		})

		t.Run("a failed save restores UpdatedAt", func(t *testing.T) {
			session := ant.Session(db)
			got, err := session.Authors.Find(ctx, ann.ID)
			require.NoError(t, err)

			before := got.UpdatedAt
			got.Name = "x"
			_, err = db.Authors.Where(ant.Eq(&A.ID, ann.ID)).Delete(ctx)
			require.NoError(t, err)
			require.ErrorIs(t, session.SaveChanges(ctx), ant.ErrConcurrency)
			require.True(t, got.UpdatedAt.Equal(before))
		})
	})
}

type Tagged struct {
	ID        int64
	Name      string
	CreatedAt time.Time
}

func (Tagged) Configure(b *ant.ModelBuilder[Tagged]) { b.NoTimestamps() }

type TaggedDB struct {
	ant.Context
	Rows ant.Table[Tagged]
}

// NoTimestamps switches this off for a model.
func TestNoTimestamps(t *testing.T) {
	ddl := `CREATE TABLE taggeds (id BIGSERIAL PRIMARY KEY, name TEXT NOT NULL, created_at TIMESTAMPTZ NOT NULL)`
	fixture.RunWith(t, ddl, func(t *testing.T, db *TaggedDB) {
		row := &Tagged{Name: "a"}
		require.NoError(t, db.Rows.Insert(t.Context(), row))
		require.True(t, row.CreatedAt.IsZero())
	})
}
