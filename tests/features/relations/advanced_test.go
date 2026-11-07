package relations

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/skudra-go/ant"
	"github.com/skudra-go/ant/tests/features/fixture"
)

// Country has posts through its users, with no key of its own on Article.
type Country struct {
	ID       int64
	Name     string
	Members  []Member
	Articles []Article
	Top      *Article
}

func (Country) Configure(b *ant.ModelBuilder[Country]) {
	b.HasManyThrough(&Cn.Articles, &Mb.CountryID, &Ar.MemberID)
	b.HasOneThrough(&Cn.Top, &Mb.CountryID, &Ar.MemberID)
}

// Member belongs to a country.
type Member struct {
	ID        int64
	CountryID int64
	Name      string
	Articles  []Article
}

// Article belongs to a member.
type Article struct {
	ID       int64
	MemberID int64
	Title    string
}

// ThroughDB holds them.
type ThroughDB struct {
	ant.Context
	Countries ant.Table[Country]
	Members   ant.Table[Member]
	Articles  ant.Table[Article]
}

// Probes.
var (
	Cn = ant.Fields[Country]()
	Mb = ant.Fields[Member]()
	Ar = ant.Fields[Article]()
)

const throughDDL = `
	CREATE TABLE countries (id BIGSERIAL PRIMARY KEY, name TEXT NOT NULL);
	CREATE TABLE members (id BIGSERIAL PRIMARY KEY, country_id BIGINT NOT NULL, name TEXT NOT NULL);
	CREATE TABLE articles (id BIGSERIAL PRIMARY KEY, member_id BIGINT NOT NULL, title TEXT NOT NULL)`

// A through relation crosses a table in between. It reads like any relation, and
// is never written, since the rows belong to the models in the middle.
func TestThrough(t *testing.T) {
	fixture.RunTrackedWith(t, throughDDL, func(t *testing.T, db *ThroughDB) {
		ctx := t.Context()

		latvia := &Country{
			Name: "Latvia",
			Members: []Member{
				{Name: "A", Articles: []Article{{Title: "one"}, {Title: "two"}}},
				{Name: "B", Articles: []Article{{Title: "three"}}},
			},
		}

		empty := &Country{Name: "Nowhere"}
		require.NoError(t, db.Countries.Insert(ctx, latvia, empty))
		require.NoError(t, db.ClearTracked(ctx))

		t.Run("Include loads across the middle table", func(t *testing.T) {
			countries, err := db.Countries.OrderBy(&Cn.Name).
				Include(&Cn.Articles, func(q *ant.Query[Article]) *ant.Query[Article] {
					return q.OrderBy(&Ar.Title)
				}).
				NoTracking().
				All(ctx)
			require.NoError(t, err)

			titles := []string{}

			for _, a := range countries[0].Articles {
				titles = append(titles, a.Title)
			}

			require.Equal(t, []string{"one", "three", "two"}, titles)
			require.Empty(t, countries[1].Articles)
			require.NotNil(t, countries[1].Articles, "loaded, and empty")
		})

		t.Run("Has and counts work", func(t *testing.T) {
			countries, err := db.Countries.Where(ant.Has(&Cn.Articles, ant.Eq(&Ar.Title, "three"))).
				NoTracking().
				All(ctx)
			require.NoError(t, err)
			require.Len(t, countries, 1)
			require.Equal(t, "Latvia", countries[0].Name)
		})

		t.Run("IncludeOne reads one row", func(t *testing.T) {
			country, err := db.Countries.IncludeOne(&Cn.Top).
				Where(ant.Eq(&Cn.Name, "Latvia")).
				NoTracking().
				First(ctx)
			require.NoError(t, err)
			require.NotNil(t, country.Top)
		})

		t.Run("saving does not touch the relation", func(t *testing.T) {
			country, err := db.Countries.Include(&Cn.Articles).
				Where(ant.Eq(&Cn.Name, "Latvia")).
				First(ctx)
			require.NoError(t, err)

			country.Articles = nil

			require.NoError(t, db.SaveChanges(ctx))
			count, err := db.Articles.Count(ctx)
			require.NoError(t, err)
			require.EqualValues(t, 3, count)
		})
	})
}

// Remark attaches to a post or a video. OwnerType says which, and the same field
// holds the key for both.
type Remark struct {
	ID        int64
	OwnerID   int64
	OwnerType string
	Text      string
	Post      *Essay
	Video     *Video
}

func (Remark) Configure(b *ant.ModelBuilder[Remark]) {
	b.BelongsTo(&Rk.Post).ForeignKey(&Rk.OwnerID).Morph(&Rk.OwnerType, "post")
	b.BelongsTo(&Rk.Video).ForeignKey(&Rk.OwnerID).Morph(&Rk.OwnerType, "video")
}

// Essay has comments, recognised by their OwnerType.
type Essay struct {
	ID      int64
	Title   string
	Remarks []Remark
}

func (Essay) Configure(b *ant.ModelBuilder[Essay]) {
	b.HasMany(&Es.Remarks).ForeignKey(&Rk.OwnerID).Morph(&Rk.OwnerType, "post")
}

// Video has comments too, with the same keys as some blog.
type Video struct {
	ID      int64
	Name    string
	Remarks []Remark
}

func (Video) Configure(b *ant.ModelBuilder[Video]) {
	b.HasMany(&Vd.Remarks).ForeignKey(&Rk.OwnerID).Morph(&Rk.OwnerType, "video")
}

// MorphDB holds them.
type MorphDB struct {
	ant.Context
	Essays  ant.Table[Essay]
	Videos  ant.Table[Video]
	Remarks ant.Table[Remark]
}

// Probes.
var (
	Rk = ant.Fields[Remark]()
	Es = ant.Fields[Essay]()
	Vd = ant.Fields[Video]()
)

const morphDDL = `
	CREATE TABLE essays (id BIGSERIAL PRIMARY KEY, title TEXT NOT NULL);
	CREATE TABLE videos (id BIGSERIAL PRIMARY KEY, name TEXT NOT NULL);
	CREATE TABLE remarks (id BIGSERIAL PRIMARY KEY, owner_id BIGINT NOT NULL, owner_type TEXT NOT NULL, text TEXT NOT NULL)`

// A polymorphic relation keeps one foreign key for several tables. Essay 1 and
// Video 1 share a key, and each sees only its own comments.
func TestPolymorphicRelations(t *testing.T) {
	fixture.RunTrackedWith(t, morphDDL, func(t *testing.T, db *MorphDB) {
		ctx := t.Context()
		blog := &Essay{Title: "b", Remarks: []Remark{{Text: "on the blog"}}}
		video := &Video{Name: "v", Remarks: []Remark{{Text: "on the video"}, {Text: "also"}}}

		require.NoError(t, db.Essays.Insert(ctx, blog))
		require.NoError(t, db.Videos.Insert(ctx, video))
		require.Equal(t, blog.ID, video.ID, "the point of the test")
		require.NoError(t, db.ClearTracked(ctx))

		t.Run("saving through the relation sets the type", func(t *testing.T) {
			count, err := db.Remarks.Where(ant.Eq(&Rk.OwnerType, "video")).Count(ctx)
			require.NoError(t, err)
			require.EqualValues(t, 2, count)
		})

		t.Run("Include sees only its own type", func(t *testing.T) {
			loaded, err := db.Essays.Include(&Es.Remarks).NoTracking().First(ctx)
			require.NoError(t, err)
			require.Len(t, loaded.Remarks, 1)
			require.Equal(t, "on the blog", loaded.Remarks[0].Text)
		})

		t.Run("the other direction loads the right parent", func(t *testing.T) {
			comments, err := db.Remarks.IncludeOne(&Rk.Post).
				IncludeOne(&Rk.Video).
				OrderBy(&Rk.ID).
				NoTracking().
				All(ctx)
			require.NoError(t, err)
			require.NotNil(t, comments[0].Post)
			require.Nil(t, comments[0].Video)
			require.Nil(t, comments[1].Post)
			require.NotNil(t, comments[1].Video)
		})

		t.Run("Has and joins are scoped", func(t *testing.T) {
			blogs, err := db.Essays.Where(ant.Has(&Es.Remarks, ant.Eq(&Rk.Text, "on the video"))).
				NoTracking().
				All(ctx)
			require.NoError(t, err)
			require.Empty(t, blogs)

			pairs, err := db.Essays.JoinMany(&Es.Remarks).
				Select(func(b *Essay, c *Remark) string { return c.Text }).
				All(ctx)
			require.NoError(t, err)
			require.Equal(t, []string{"on the blog"}, pairs)
		})
	})
}

// Novelist shows scoped relations, latest-of-many, and inverse relations.
type Novelist struct {
	ID       int64
	Name     string
	Novels   []Novel
	Live     []Novel
	Newest   *Novel
	Earliest *Novel
}

func (Novelist) Configure(b *ant.ModelBuilder[Novelist]) {
	b.HasMany(&Nv.Novels).ForeignKey(&Nl.NovelistID).Inverse(&Nl.Novelist)
	b.HasMany(&Nv.Live).ForeignKey(&Nl.NovelistID).Where(ant.Eq(&Nl.Published, true))
	b.HasOne(&Nv.Newest).ForeignKey(&Nl.NovelistID).Latest(&Nl.Year)
	b.HasOne(&Nv.Earliest).ForeignKey(&Nl.NovelistID).Oldest(&Nl.Year)
}

// Novel belongs to an author, and touches it when written.
type Novel struct {
	ID         int64
	NovelistID int64
	Title      string
	Year       int
	Published  bool
	Novelist   *Novelist
}

// LibraryDB holds them.
type LibraryDB struct {
	ant.Context
	Novelists ant.Table[Novelist]
	Novels    ant.Table[Novel]
}

// Probes.
var (
	Nv = ant.Fields[Novelist]()
	Nl = ant.Fields[Novel]()
)

const libraryDDL = `
	CREATE TABLE novelists (id BIGSERIAL PRIMARY KEY, name TEXT NOT NULL);
	CREATE TABLE novels (id BIGSERIAL PRIMARY KEY, novelist_id BIGINT NOT NULL, title TEXT NOT NULL, year INT NOT NULL, published BOOLEAN NOT NULL)`

func TestScopedAndChosenRelations(t *testing.T) {
	fixture.RunWith(t, libraryDDL, func(t *testing.T, db *LibraryDB) {
		ctx := t.Context()
		author := &Novelist{Name: "A"}
		other := &Novelist{Name: "B"}
		require.NoError(t, db.Novelists.Insert(ctx, author, other))

		require.NoError(t, db.Novels.Insert(ctx,
			&Novel{
				NovelistID: author.ID,
				Title:      "first",
				Year:       1990,
				Published:  true,
			},
			&Novel{NovelistID: author.ID, Title: "second", Year: 2000},
			&Novel{
				NovelistID: author.ID,
				Title:      "third",
				Year:       2010,
				Published:  true,
			},
			&Novel{NovelistID: other.ID, Title: "only", Year: 2005}))

		t.Run("a scoped relation sees what its condition allows", func(t *testing.T) {
			loaded, err := db.Novelists.Include(&Nv.Live).Find(ctx, author.ID)
			require.NoError(t, err)
			require.Len(t, loaded.Live, 2)
		})

		t.Run("counts and Has follow the scope", func(t *testing.T) {
			authors, err := db.Novelists.Where(ant.Has(&Nv.Live)).All(ctx)
			require.NoError(t, err)
			require.Len(t, authors, 1)
			require.Equal(t, "A", authors[0].Name)
		})

		t.Run("Latest and Oldest choose one row per parent", func(t *testing.T) {
			authors, err := db.Novelists.IncludeOne(&Nv.Newest).
				IncludeOne(&Nv.Earliest).
				OrderBy(&Nv.Name).
				All(ctx)
			require.NoError(t, err)
			require.Equal(t, "third", authors[0].Newest.Title)
			require.Equal(t, "first", authors[0].Earliest.Title)
			require.Equal(t, "only", authors[1].Newest.Title)
		})

		t.Run("Inverse fills the pointer back with no query", func(t *testing.T) {
			loaded, err := db.Novelists.Include(&Nv.Novels).Find(ctx, author.ID)
			require.NoError(t, err)
			require.Same(t, loaded, loaded.Novels[0].Novelist)
		})
	})
}

// Discussion's UpdatedAt moves when a reply is written, with Touch.
type Discussion struct {
	ID        int64
	UpdatedAt time.Time
	Answers   []Answer
}

// Answer touches its thread.
type Answer struct {
	ID           int64
	DiscussionID int64
	Text         string
	Discussion   *Discussion
}

func (Answer) Configure(b *ant.ModelBuilder[Answer]) { b.BelongsTo(&An.Discussion).Touch() }

// BoardDB holds them.
type BoardDB struct {
	ant.Context
	Discussions ant.Table[Discussion]
	Answers     ant.Table[Answer]
}

// An is Answer's probe.
var An = ant.Fields[Answer]()

func TestTouch(t *testing.T) {
	const ddl = `
		CREATE TABLE discussions (id BIGSERIAL PRIMARY KEY, updated_at TIMESTAMPTZ NOT NULL);
		CREATE TABLE answers (id BIGSERIAL PRIMARY KEY, discussion_id BIGINT NOT NULL, text TEXT NOT NULL)`
	fixture.RunTrackedWith(t, ddl, func(t *testing.T, db *BoardDB) {
		ctx := t.Context()
		thread := &Discussion{}
		require.NoError(t, db.Discussions.Insert(ctx, thread))
		_, err := ant.Raw[any](db, "UPDATE discussions SET updated_at = '2000-01-01'").Exec(ctx)
		require.NoError(t, err)

		reply := &Answer{DiscussionID: thread.ID, Text: "hi"}
		require.NoError(t, db.Answers.Insert(ctx, reply))

		stored, err := ant.Raw[time.Time](db, "SELECT updated_at FROM discussions").First(ctx)
		require.NoError(t, err)
		require.Greater(t, stored.Year(), 2000)
	})
}
