package lifecycle

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/skudra-go/ant"
	"github.com/skudra-go/ant/tests/features/fixture"
)

// A field of type ant.DeletedAt makes deletes reversible: rows are marked, and
// queries skip them.
type Note struct {
	ant.Base
	AuthorID  int64
	Text      string
	DeletedAt ant.DeletedAt
	Author    *Author
}

type Author struct {
	ant.Base
	Name  string
	Notes []Note
}

type NotesDB struct {
	ant.Context
	Notes   ant.Table[Note]
	Authors ant.Table[Author]
}

var (
	Notes   = ant.Fields[Note]()
	Authors = ant.Fields[Author]()
)

const notesDDL = `
	CREATE TABLE authors (id BIGSERIAL PRIMARY KEY, created_at TIMESTAMPTZ NOT NULL, updated_at TIMESTAMPTZ NOT NULL, name TEXT NOT NULL);
	CREATE TABLE notes (id BIGSERIAL PRIMARY KEY, created_at TIMESTAMPTZ NOT NULL, updated_at TIMESTAMPTZ NOT NULL,
		author_id BIGINT NOT NULL REFERENCES authors, text TEXT NOT NULL, deleted_at TIMESTAMPTZ)`

func TestSoftDelete(t *testing.T) {
	fixture.RunTrackedWith(t, notesDDL, func(t *testing.T, db *NotesDB) {
		ctx := t.Context()
		ann := &Author{Name: "Ann"}
		require.NoError(t, db.Authors.Insert(ctx, ann))
		kept, gone := &Note{AuthorID: ann.ID, Text: "kept"}, &Note{AuthorID: ann.ID, Text: "gone"}
		require.NoError(t, db.Notes.Insert(ctx, kept, gone))

		textsOf := func(notes []*Note) []string {
			out := make([]string, len(notes))

			for i, n := range notes {
				out[i] = n.Text
			}

			return out
		}

		rawCount := func() int {
			n, err := ant.Raw[int](db, `SELECT count(*) FROM notes`).First(ctx)
			require.NoError(t, err)

			return *n
		}

		n, err := db.Notes.Where(ant.Eq(&Notes.Text, "gone")).Delete(ctx)
		require.NoError(t, err)
		require.EqualValues(t, 1, n)

		t.Run("the row stays but queries skip it", func(t *testing.T) {
			require.Equal(t, 2, rawCount())

			got, err := db.Notes.NoTracking().All(ctx)
			require.NoError(t, err)
			require.Equal(t, []string{"kept"}, textsOf(got))
		})

		t.Run("Find, Count, and Exists skip it", func(t *testing.T) {
			_, err := db.Notes.NoTracking().Find(ctx, gone.ID)
			require.ErrorIs(t, err, ant.ErrNotFound)
			_, err = db.Notes.Find(ctx, gone.ID)
			require.ErrorIs(t, err, ant.ErrNotFound, "even when a stale instance is tracked")
			c, err := db.Notes.Count(ctx)
			require.NoError(t, err)
			require.EqualValues(t, 1, c)
		})

		t.Run("Include, Has, and joins skip it", func(t *testing.T) {
			got, err := db.Authors.NoTracking().Include(&Authors.Notes).Single(ctx)
			require.NoError(t, err)
			require.Len(t, got.Notes, 1)

			c, err := db.Authors.Where(ant.Has(&Authors.Notes, ant.Eq(&Notes.Text, "gone"))).
				Count(ctx)
			require.NoError(t, err)
			require.Zero(t, c)

			rows, err := db.Authors.LeftJoinMany(&Authors.Notes).
				Select(func(a *Author, n *Note) string { return n.Text }).
				All(ctx)
			require.NoError(t, err)
			require.Equal(t, []string{"kept"}, rows)
		})

		t.Run("WithDeleted and OnlyDeleted see it", func(t *testing.T) {
			got, err := db.Notes.WithDeleted().NoTracking().OrderBy(&Notes.Text).All(ctx)
			require.NoError(t, err)
			require.Equal(t, []string{"gone", "kept"}, textsOf(got))

			got, err = db.Notes.OnlyDeleted().NoTracking().All(ctx)
			require.NoError(t, err)
			require.Equal(t, []string{"gone"}, textsOf(got))
			require.True(t, got[0].DeletedAt.Valid)
		})

		t.Run("a second delete changes nothing", func(t *testing.T) {
			n, err := db.Notes.Where(ant.Eq(&Notes.Text, "gone")).Delete(ctx)
			require.NoError(t, err)
			require.Zero(t, n)
		})

		t.Run("Restore brings it back", func(t *testing.T) {
			n, err := db.Notes.Where(ant.Eq(&Notes.Text, "gone")).Restore(ctx)
			require.NoError(t, err)
			require.EqualValues(t, 1, n)

			c, err := db.Notes.Count(ctx)
			require.NoError(t, err)
			require.EqualValues(t, 2, c)
		})

		t.Run("Remove with SaveChanges soft-deletes", func(t *testing.T) {
			session := ant.Session(db)
			note, err := session.Notes.Where(ant.Eq(&Notes.Text, "gone")).Single(ctx)
			require.NoError(t, err)
			require.NoError(t, ant.Remove(ctx, session, note))
			require.NoError(t, session.SaveChanges(ctx))
			require.Equal(t, 2, rawCount())
		})

		t.Run("DeleteByID soft-deletes", func(t *testing.T) {
			require.NoError(t, db.Notes.DeleteByID(ctx, kept.ID))
			require.Equal(t, 2, rawCount())

			_, err := db.Notes.Where(ant.Gt(&Notes.ID, 0)).Restore(ctx)
			require.NoError(t, err)
		})

		t.Run("ForceDelete removes it for real", func(t *testing.T) {
			n, err := db.Notes.Where(ant.Eq(&Notes.Text, "gone")).ForceDelete(ctx)
			require.NoError(t, err)
			require.EqualValues(t, 1, n)
			require.Equal(t, 1, rawCount())
		})

		t.Run("DeletedAt.V compares when a row was deleted", func(t *testing.T) {
			author := &Author{Name: "Prue"}
			require.NoError(t, db.Authors.Insert(ctx, author))
			require.NoError(t, db.Notes.Insert(ctx, &Note{AuthorID: author.ID, Text: "stale"}))
			_, err := db.Notes.Where(ant.Eq(&Notes.Text, "stale")).Delete(ctx)
			require.NoError(t, err)

			early, err := db.Notes.OnlyDeleted().
				Where(ant.Lt(&Notes.DeletedAt.V, time.Now().Add(-time.Hour))).
				Count(ctx)
			require.NoError(t, err)
			require.Zero(t, early)

			removed, err := db.Notes.Prune(
				ctx,
				ant.Eq(&Notes.Text, "stale"),
				ant.Lt(&Notes.DeletedAt.V, time.Now().Add(time.Hour)),
			)
			require.NoError(t, err)
			require.EqualValues(t, 1, removed)
		})

		t.Run("ForceDelete reaches deleted rows with WithDeleted", func(t *testing.T) {
			_, err := db.Notes.Where(ant.Eq(&Notes.Text, "kept")).Delete(ctx)
			require.NoError(t, err)
			n, err := db.Notes.WithDeleted().Where(ant.Eq(&Notes.Text, "kept")).ForceDelete(ctx)
			require.NoError(t, err)
			require.EqualValues(t, 1, n)
		})

		t.Run("Restore on a plain model", func(t *testing.T) {
			_, err := db.Authors.Where(ant.Gt(&Authors.ID, 0)).Restore(ctx)
			require.ErrorIs(t, err, ant.ErrNotSoftDeleted)
		})
	})
}
