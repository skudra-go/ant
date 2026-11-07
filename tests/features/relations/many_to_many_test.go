package relations

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/skudra-go/ant"
	"github.com/skudra-go/ant/tests/features/fixture"
)

// A many-to-many relation goes through a join table. Its columns default to
// student_id and course_id here.
type Student struct {
	ID      int64
	Name    string
	Courses []Course
}

func (Student) Configure(b *ant.ModelBuilder[Student]) {
	b.ManyToMany(&St.Courses).Through("enrollments")
}

type Course struct {
	ID       int64
	Title    string
	Students []Student
}

func (Course) Configure(b *ant.ModelBuilder[Course]) {
	b.ManyToMany(&Cs.Students).Through("enrollments")
}

type SchoolDB struct {
	ant.Context
	Students ant.Table[Student]
	Courses  ant.Table[Course]
}

var (
	St = ant.Fields[Student]()
	Cs = ant.Fields[Course]()
)

const schoolDDL = `
	CREATE TABLE students (id BIGSERIAL PRIMARY KEY, name TEXT NOT NULL);
	CREATE TABLE courses (id BIGSERIAL PRIMARY KEY, title TEXT NOT NULL);
	CREATE TABLE enrollments (
		student_id BIGINT NOT NULL REFERENCES students ON DELETE CASCADE,
		course_id BIGINT NOT NULL REFERENCES courses ON DELETE CASCADE,
		PRIMARY KEY (student_id, course_id))`

func titlesOf(courses []Course) []string {
	out := make([]string, len(courses))

	for i, c := range courses {
		out[i] = c.Title
	}

	return out
}

// Keys need not be integers.
type Doc struct {
	ID     string
	Topics []Topic
}

func (Doc) Configure(b *ant.ModelBuilder[Doc]) {
	b.ManyToMany(&Dc.Topics).Through("doc_topics")
}

type Topic struct {
	ID   string
	Name string
}

type DocsDB struct {
	ant.Context
	Docs   ant.Table[Doc]
	Topics ant.Table[Topic]
}

var Dc = ant.Fields[Doc]()

func TestManyToManyWithUUIDKeys(t *testing.T) {
	ddl := `
		CREATE TABLE docs (id UUID PRIMARY KEY);
		CREATE TABLE topics (id UUID PRIMARY KEY, name TEXT NOT NULL);
		CREATE TABLE doc_topics (doc_id UUID NOT NULL REFERENCES docs, topic_id UUID NOT NULL REFERENCES topics)`
	fixture.RunWith(t, ddl, func(t *testing.T, db *DocsDB) {
		ctx := t.Context()
		doc := &Doc{ID: "0192f3a4-7c3d-7b1e-8f00-000000000001"}
		topic := &Topic{ID: "0192f3a4-7c3d-7b1e-8f00-000000000002", Name: "Go"}

		require.NoError(t, db.Docs.Insert(ctx, doc))
		require.NoError(t, db.Topics.Insert(ctx, topic))
		require.NoError(t, ant.Link(ctx, db, doc, &Dc.Topics, topic))

		got, err := db.Docs.Include(&Dc.Topics).Single(ctx)
		require.NoError(t, err)
		require.Len(t, got.Topics, 1)
		require.Equal(t, "Go", got.Topics[0].Name)
	})
}

func TestManyToMany(t *testing.T) {
	fixture.RunTrackedWith(t, schoolDDL, func(t *testing.T, db *SchoolDB) {
		ctx := t.Context()
		ann, ben := &Student{Name: "Ann"}, &Student{Name: "Ben"}
		math, art, music := &Course{Title: "Math"}, &Course{Title: "Art"}, &Course{Title: "Music"}

		require.NoError(t, db.Students.Insert(ctx, ann, ben))
		require.NoError(t, db.Courses.Insert(ctx, math, art, music))

		courses := func(s *Student) []string {
			got, err := ant.Session(db).
				Students.Include(&St.Courses, func(q *ant.Query[Course]) *ant.Query[Course] {
				return q.OrderBy(&Cs.Title)
			}).
				Find(ctx, s.ID)
			require.NoError(t, err)

			return titlesOf(got.Courses)
		}

		t.Run("Link adds, and repeating it is harmless", func(t *testing.T) {
			require.NoError(t, ant.Link(ctx, db, ann, &St.Courses, math, art))
			require.NoError(t, ant.Link(ctx, db, ann, &St.Courses, math))
			require.Equal(t, []string{"Art", "Math"}, courses(ann))
		})

		t.Run("Include loads through the join table", func(t *testing.T) {
			require.NoError(t, ant.Link(ctx, db, ben, &St.Courses, math))
			got, err := db.Students.NoTracking().OrderBy(&St.Name).Include(&St.Courses).All(ctx)
			require.NoError(t, err)
			require.Len(t, got[0].Courses, 2)
			require.Len(t, got[1].Courses, 1)
		})

		t.Run("the other side works too", func(t *testing.T) {
			got, err := db.Courses.NoTracking().
				Where(ant.Eq(&Cs.Title, "Math")).
				Include(&Cs.Students).
				Single(ctx)
			require.NoError(t, err)
			require.Len(t, got.Students, 2)
		})

		t.Run("Has looks through the join table", func(t *testing.T) {
			got, err := db.Students.NoTracking().
				Where(ant.Has(&St.Courses, ant.Eq(&Cs.Title, "Art"))).
				Single(ctx)
			require.NoError(t, err)
			require.Equal(t, "Ann", got.Name)
		})

		t.Run("JoinMany goes through it", func(t *testing.T) {
			got, err := db.Students.JoinMany(&St.Courses).
				Where(ant.Eq(&Cs.Title, "Math")).
				OrderBy(&St.Name).
				Select(func(s *Student, c *Course) string { return s.Name }).
				All(ctx)
			require.NoError(t, err)
			require.Equal(t, []string{"Ann", "Ben"}, got)
		})

		t.Run("CountRelated counts through it", func(t *testing.T) {
			type row struct {
				Name  string
				Count int64
			}

			R := ant.Fields[row]()
			got, err := ant.Project[row](db.Students.OrderBy(&St.Name),
				ant.Into(&R.Name, &St.Name),
				ant.IntoExpr(&R.Count, ant.CountRelated(&St.Courses))).All(ctx)
			require.NoError(t, err)
			require.Equal(t, []row{{"Ann", 2}, {"Ben", 1}}, got)
		})

		t.Run("Unlink removes the link, not the course", func(t *testing.T) {
			require.NoError(t, ant.Unlink(ctx, db, ann, &St.Courses, art))
			require.Equal(t, []string{"Math"}, courses(ann))

			n, err := db.Courses.Count(ctx)
			require.NoError(t, err)
			require.EqualValues(t, 3, n)
		})

		t.Run("SyncLinks makes the list exact", func(t *testing.T) {
			require.NoError(t, ant.SyncLinks(ctx, db, ann, &St.Courses, art, music))
			require.Equal(t, []string{"Art", "Music"}, courses(ann))
			require.NoError(t, ant.SyncLinks(ctx, db, ann, &St.Courses))
			require.Empty(t, courses(ann))
		})

		t.Run("Toggle flips each", func(t *testing.T) {
			require.NoError(t, ant.Link(ctx, db, ann, &St.Courses, math))
			require.NoError(t, ant.Toggle(ctx, db, ann, &St.Courses, math, art))
			require.Equal(t, []string{"Art"}, courses(ann))
		})

		t.Run("a tracked relation saves its changes", func(t *testing.T) {
			require.NoError(t, ant.SyncLinks(ctx, db, ann, &St.Courses, math, art))
			session := ant.Session(db)
			got, err := session.Students.Include(
				&St.Courses,
				func(q *ant.Query[Course]) *ant.Query[Course] {
					return q.OrderBy(&Cs.Title)
				},
			).
				Find(ctx, ann.ID)
			require.NoError(t, err)

			got.Courses = got.Courses[1:] // removes Art
			got.Courses = append(got.Courses, *music)

			require.NoError(t, session.SaveChanges(ctx))
			require.Equal(t, []string{"Math", "Music"}, courses(ann))

			n, err := db.Courses.Where(ant.Eq(&Cs.Title, "Art")).Count(ctx)
			require.NoError(t, err)
			require.EqualValues(t, 1, n, "removing a link never deletes the course")
			require.False(t, fixture.HasChanges(t, session))
		})

		t.Run("a new target is inserted and linked", func(t *testing.T) {
			session := ant.Session(db)
			got, err := session.Students.Include(&St.Courses).Find(ctx, ben.ID)
			require.NoError(t, err)

			got.Courses = append(got.Courses, Course{Title: "Drama"})

			require.NoError(t, session.SaveChanges(ctx))
			require.Contains(t, courses(ben), "Drama")
		})

		t.Run("a new owner with existing targets", func(t *testing.T) {
			cal := &Student{Name: "Cal", Courses: []Course{*math}}
			require.NoError(t, db.Students.Insert(ctx, cal))
			require.Equal(t, []string{"Math"}, courses(cal))
		})

		t.Run("mistakes", func(t *testing.T) {
			require.ErrorIs(t, ant.Link[Student](ctx, db, nil, &St.Courses, math), ant.ErrNilEntity)
			require.ErrorIs(t, ant.Link(ctx, db, ann, &St.Courses, nil), ant.ErrNilEntity)
			require.ErrorIs(t, ant.Link(ctx, db, ann, &St.Courses, &Course{}), ant.ErrNoKey)
			require.NoError(t, ant.Link(ctx, db, ann, &St.Courses), "no targets is nothing to do")
		})

		t.Run("a relation that is not many-to-many", func(t *testing.T) {
			u := &fixture.User{Base: ant.Base{ID: 1}}
			err := ant.Link(ctx, db, u, &U.Posts, &fixture.Post{Base: ant.Base{ID: 1}})
			require.ErrorIs(t, err, ant.ErrNotManyToMany)
		})
	})
}
