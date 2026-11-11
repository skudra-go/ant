// Package compare measures ant against GORM, Bun, ent and plain database/sql on one
// PostgreSQL, doing the same work. It is a module of its own, so that ant does not
// depend on what it is compared with. Plain database/sql stands in for sqlc, whose
// generated code is exactly that: a query string and a Scan.
//
// Every library shares one connection pool. ant runs with change tracking on, as it
// does by default, and "ant-notrack" is the same query with NoTracking. Commits
// don't wait for the disk, so writes measure the library and not the disk.
// README.md has the latest results and how to read them.
//
//	make db-up DATABASES=postgres
//	cd tools/compare && go test -run '^$' -bench . -benchmem -count 6
package compare

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"testing"
	"time"

	entsql "entgo.io/ent/dialect/sql"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect/pgdialect"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/skudra-go/ant"
	antpg "github.com/skudra-go/ant/dialect/postgres"
	"github.com/skudra-go/ant/tools/compare/ent"
	entuser "github.com/skudra-go/ant/tools/compare/ent/user"
)

// User is the row every library but ent reads. ent reads the type it generates
// from ent/schema, which has the same columns.
type User struct {
	ID        int64     `gorm:"primaryKey" bun:",pk,autoincrement"`
	CreatedAt time.Time `bun:",nullzero,notnull,default:current_timestamp"`
	UpdatedAt time.Time `bun:",nullzero,notnull,default:current_timestamp"`
	Name      string
	Email     string
	Age       int
	Bio       *string
	Posts     []Post `bun:"rel:has-many,join:id=user_id"`

	bun.BaseModel `bun:"table:users" gorm:"-" ant:"-"`
}

// Post belongs to a User.
type Post struct {
	ID        int64     `gorm:"primaryKey" bun:",pk,autoincrement"`
	CreatedAt time.Time `bun:",nullzero,notnull,default:current_timestamp"`
	UpdatedAt time.Time `bun:",nullzero,notnull,default:current_timestamp"`
	UserID    int64
	Title     string

	bun.BaseModel `bun:"table:posts" gorm:"-" ant:"-"`
}

// AppDB is ant's context.
type AppDB struct {
	ant.Context
	Users ant.Table[User]
	Posts ant.Table[Post]
}

var Users = ant.Fields[User]()

// The table holds 1000 users with 5 posts each.
const ddl = `
	DROP SCHEMA IF EXISTS ant_compare CASCADE;
	CREATE SCHEMA ant_compare;
	CREATE TABLE ant_compare.users (id BIGSERIAL PRIMARY KEY, created_at TIMESTAMPTZ NOT NULL DEFAULT now(), updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
		name TEXT NOT NULL, email TEXT NOT NULL, age INT NOT NULL, bio TEXT);
	CREATE TABLE ant_compare.posts (id BIGSERIAL PRIMARY KEY, created_at TIMESTAMPTZ NOT NULL DEFAULT now(), updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
		user_id BIGINT NOT NULL REFERENCES ant_compare.users (id), title TEXT NOT NULL);
	CREATE INDEX ON ant_compare.posts (user_id);
	INSERT INTO ant_compare.users (created_at, updated_at, name, email, age)
	SELECT now(), now(), 'user ' || n, 'u' || n || '@example.com', n % 80 FROM generate_series(1, 1000) n;
	INSERT INTO ant_compare.posts (created_at, updated_at, user_id, title)
	SELECT now(), now(), u, 'post ' || p FROM generate_series(1, 1000) u, generate_series(1, 5) p;
	ANALYZE ant_compare.users;
	ANALYZE ant_compare.posts`

const columns = "id, created_at, updated_at, name, email, age, bio"

type libraries struct {
	plain *sql.DB
	ant   *AppDB
	gorm  *gorm.DB
	bun   *bun.DB
	ent   *ent.Client
}

func setup(tb testing.TB) *libraries {
	tb.Helper()

	dsn := os.Getenv("ANT_PG_DSN")
	if dsn == "" {
		dsn = "postgres://ant:ant@127.0.0.1:55432/ant_test?sslmode=disable"
	}

	// Without synchronous_commit, a commit doesn't wait for the disk, which would
	// otherwise take most of the time of every write and hide the libraries' cost.
	dsn += "&search_path=ant_compare&synchronous_commit=off"
	ctx := context.Background()

	plain, err := sql.Open("pgx", dsn)
	if err != nil {
		tb.Skip(err)
	}

	if err := plain.PingContext(ctx); err != nil {
		tb.Skip("no database: ", err)
	}

	if _, err := plain.ExecContext(ctx, ddl); err != nil {
		tb.Fatal(err)
	}

	db, err := ant.Wrap[AppDB](antpg.Dialect, plain)
	if err != nil {
		tb.Fatal(err)
	}

	orm, err := gorm.Open(
		postgres.New(postgres.Config{Conn: plain}),
		&gorm.Config{
			Logger:                 logger.Discard,
			SkipDefaultTransaction: true,
		},
	)
	if err != nil {
		tb.Fatal(err)
	}

	client := ent.NewClient(ent.Driver(entsql.OpenDB("postgres", plain)))

	tb.Cleanup(func() { plain.Close() })

	return &libraries{
		plain: plain,
		ant:   db,
		gorm:  orm,
		bun:   bun.NewDB(plain, pgdialect.New()),
		ent:   client,
	}
}

// scanUsers reads every row the way sqlc's generated code does.
func scanUsers(b *testing.B, rows *sql.Rows, capacity int) []*User {
	b.Helper()

	defer rows.Close()

	out := make([]*User, 0, capacity)

	for rows.Next() {
		u := new(User)
		if err := rows.Scan(
			&u.ID,
			&u.CreatedAt,
			&u.UpdatedAt,
			&u.Name,
			&u.Email,
			&u.Age,
			&u.Bio,
		); err != nil {
			b.Fatal(err)
		}

		out = append(out, u)
	}

	if err := rows.Err(); err != nil {
		b.Fatal(err)
	}

	return out
}

func check(b *testing.B, got, want int, err error) {
	b.Helper()

	if err != nil {
		b.Fatal(err)
	}

	if got != want {
		b.Fatalf("got %d rows, want %d", got, want)
	}
}

// TestSameQuery runs the query README.md shows in every library, the way it is
// written there, and checks that they all return the same users and posts.
func TestSameQuery(t *testing.T) {
	l := setup(t)
	ctx := context.Background()

	var got [4][]string

	{
		var users []User

		err := l.gorm.WithContext(ctx).
			Preload("Posts").
			Where("age > ?", 18).
			Order("name").
			Limit(10).
			Find(&users).
			Error
		if err != nil {
			t.Fatal(err)
		}

		for _, u := range users {
			got[0] = append(got[0], fmt.Sprint(u.Name, len(u.Posts)))
		}
	}

	{
		var users []User

		err := l.bun.NewSelect().
			Model(&users).
			Relation("Posts").
			Where("age > ?", 18).
			Order("name").
			Limit(10).
			Scan(ctx)
		if err != nil {
			t.Fatal(err)
		}

		for _, u := range users {
			got[1] = append(got[1], fmt.Sprint(u.Name, len(u.Posts)))
		}
	}

	{
		users, err := l.ent.User.Query().
			WithPosts().
			Where(entuser.AgeGT(18)).
			Order(entuser.ByName()).
			Limit(10).
			All(ctx)
		if err != nil {
			t.Fatal(err)
		}

		for _, u := range users {
			got[2] = append(got[2], fmt.Sprint(u.Name, len(u.Edges.Posts)))
		}
	}

	{
		users, err := l.ant.Users.
			Include(&Users.Posts).
			Where(ant.Gt(&Users.Age, 18)).
			OrderBy(&Users.Name).
			Limit(10).
			All(ctx)
		if err != nil {
			t.Fatal(err)
		}

		for _, u := range users {
			got[3] = append(got[3], fmt.Sprint(u.Name, len(u.Posts)))
		}
	}

	if len(got[3]) != 10 {
		t.Fatalf("ant returned %d users, want 10", len(got[3]))
	}

	for i, name := range []string{"gorm", "bun", "ent"} {
		if fmt.Sprint(got[i]) != fmt.Sprint(got[3]) {
			t.Errorf("%s returned %v, ant %v", name, got[i], got[3])
		}
	}
}

func BenchmarkRead1000(b *testing.B) {
	l := setup(b)
	ctx := context.Background()

	b.Run("sql", func(b *testing.B) {
		b.ReportAllocs()

		for b.Loop() {
			rows, err := l.plain.QueryContext(ctx, "SELECT "+columns+" FROM users")
			if err != nil {
				b.Fatal(err)
			}

			check(b, len(scanUsers(b, rows, 1000)), 1000, nil)
		}
	})

	b.Run("ant", func(b *testing.B) {
		b.ReportAllocs()

		for b.Loop() {
			out, err := ant.Session(l.ant).Users.All(ctx)
			check(b, len(out), 1000, err)
		}
	})

	b.Run("ant-notrack", func(b *testing.B) {
		b.ReportAllocs()

		for b.Loop() {
			out, err := l.ant.Users.NoTracking().All(ctx)
			check(b, len(out), 1000, err)
		}
	})

	b.Run("gorm", func(b *testing.B) {
		b.ReportAllocs()

		for b.Loop() {
			var out []*User

			err := l.gorm.WithContext(ctx).Find(&out).Error
			check(b, len(out), 1000, err)
		}
	})

	b.Run("bun", func(b *testing.B) {
		b.ReportAllocs()

		for b.Loop() {
			var out []*User

			err := l.bun.NewSelect().Model(&out).Scan(ctx)
			check(b, len(out), 1000, err)
		}
	})

	b.Run("ent", func(b *testing.B) {
		b.ReportAllocs()

		for b.Loop() {
			out, err := l.ent.User.Query().All(ctx)
			check(b, len(out), 1000, err)
		}
	})
}

func BenchmarkFindOne(b *testing.B) {
	l := setup(b)
	ctx := context.Background()

	b.Run("sql", func(b *testing.B) {
		b.ReportAllocs()

		for b.Loop() {
			u := new(User)

			err := l.plain.QueryRowContext(ctx, "SELECT "+columns+" FROM users WHERE id = $1", 500).
				Scan(&u.ID, &u.CreatedAt, &u.UpdatedAt, &u.Name, &u.Email, &u.Age, &u.Bio)
			if err != nil {
				b.Fatal(err)
			}
		}
	})

	b.Run("ant", func(b *testing.B) {
		b.ReportAllocs()

		for b.Loop() {
			if _, err := ant.Session(l.ant).Users.Find(ctx, int64(500)); err != nil {
				b.Fatal(err)
			}
		}
	})

	b.Run("ant-notrack", func(b *testing.B) {
		b.ReportAllocs()

		for b.Loop() {
			if _, err := l.ant.Users.NoTracking().
				Where(ant.Eq(&Users.ID, 500)).
				First(ctx); err != nil {
				b.Fatal(err)
			}
		}
	})

	b.Run("gorm", func(b *testing.B) {
		b.ReportAllocs()

		for b.Loop() {
			var u User
			if err := l.gorm.WithContext(ctx).First(&u, 500).Error; err != nil {
				b.Fatal(err)
			}
		}
	})

	b.Run("bun", func(b *testing.B) {
		b.ReportAllocs()

		for b.Loop() {
			var u User
			if err := l.bun.NewSelect().Model(&u).Where("id = ?", 500).Scan(ctx); err != nil {
				b.Fatal(err)
			}
		}
	})

	b.Run("ent", func(b *testing.B) {
		b.ReportAllocs()

		for b.Loop() {
			if _, err := l.ent.User.Get(ctx, 500); err != nil {
				b.Fatal(err)
			}
		}
	})
}

func BenchmarkFilteredQuery(b *testing.B) {
	l := setup(b)
	ctx := context.Background()

	b.Run("sql", func(b *testing.B) {
		b.ReportAllocs()

		for b.Loop() {
			rows, err := l.plain.QueryContext(
				ctx,
				"SELECT "+columns+" FROM users WHERE age > $1 AND name LIKE $2 ORDER BY name LIMIT 10",
				30,
				"user 1%",
			)
			if err != nil {
				b.Fatal(err)
			}

			check(b, len(scanUsers(b, rows, 10)), 10, nil)
		}
	})

	b.Run("ant", func(b *testing.B) {
		b.ReportAllocs()

		for b.Loop() {
			out, err := ant.Session(l.ant).
				Users.
				Where(ant.Gt(&Users.Age, 30), ant.Like(&Users.Name, "user 1%")).
				OrderBy(&Users.Name).
				Limit(10).
				All(ctx)
			check(b, len(out), 10, err)
		}
	})

	b.Run("ant-notrack", func(b *testing.B) {
		b.ReportAllocs()

		for b.Loop() {
			out, err := l.ant.Users.
				NoTracking().
				Where(ant.Gt(&Users.Age, 30), ant.Like(&Users.Name, "user 1%")).
				OrderBy(&Users.Name).
				Limit(10).
				All(ctx)
			check(b, len(out), 10, err)
		}
	})

	b.Run("gorm", func(b *testing.B) {
		b.ReportAllocs()

		for b.Loop() {
			var out []*User

			err := l.gorm.WithContext(ctx).
				Where("age > ? AND name LIKE ?", 30, "user 1%").
				Order("name").
				Limit(10).
				Find(&out).
				Error
			check(b, len(out), 10, err)
		}
	})

	b.Run("bun", func(b *testing.B) {
		b.ReportAllocs()

		for b.Loop() {
			var out []*User

			err := l.bun.NewSelect().
				Model(&out).
				Where("age > ?", 30).
				Where("name LIKE ?", "user 1%").
				Order("name").
				Limit(10).
				Scan(ctx)
			check(b, len(out), 10, err)
		}
	})

	b.Run("ent", func(b *testing.B) {
		b.ReportAllocs()

		for b.Loop() {
			out, err := l.ent.User.Query().
				Where(entuser.AgeGT(30), entuser.NameHasPrefix("user 1")).
				Order(entuser.ByName()).
				Limit(10).
				All(ctx)
			check(b, len(out), 10, err)
		}
	})
}

// BenchmarkInclude100 loads 100 users and their 500 posts. Every library sends two
// queries: the users, then the posts of all of them.
func BenchmarkInclude100(b *testing.B) {
	l := setup(b)
	ctx := context.Background()

	b.Run("sql", func(b *testing.B) {
		b.ReportAllocs()

		for b.Loop() {
			rows, err := l.plain.QueryContext(
				ctx,
				"SELECT "+columns+" FROM users ORDER BY id LIMIT 100",
			)
			if err != nil {
				b.Fatal(err)
			}

			users := scanUsers(b, rows, 100)
			ids := make([]int64, len(users))
			byID := make(map[int64]*User, len(users))

			for i, u := range users {
				ids[i] = u.ID
				byID[u.ID] = u
			}

			rows, err = l.plain.QueryContext(
				ctx,
				"SELECT id, created_at, updated_at, user_id, title FROM posts WHERE user_id = ANY($1)",
				ids,
			)
			if err != nil {
				b.Fatal(err)
			}

			n := 0

			for rows.Next() {
				var p Post
				if err := rows.Scan(
					&p.ID,
					&p.CreatedAt,
					&p.UpdatedAt,
					&p.UserID,
					&p.Title,
				); err != nil {
					b.Fatal(err)
				}

				u := byID[p.UserID]
				u.Posts = append(u.Posts, p)
				n++
			}

			rows.Close()
			check(b, n, 500, rows.Err())
		}
	})

	b.Run("ant", func(b *testing.B) {
		b.ReportAllocs()

		for b.Loop() {
			out, err := ant.Session(l.ant).
				Users.
				Include(&Users.Posts).
				OrderBy(&Users.ID).
				Limit(100).
				All(ctx)
			check(b, len(out)*len(out[0].Posts), 500, err)
		}
	})

	b.Run("ant-notrack", func(b *testing.B) {
		b.ReportAllocs()

		for b.Loop() {
			out, err := l.ant.Users.
				NoTracking().
				Include(&Users.Posts).
				OrderBy(&Users.ID).
				Limit(100).
				All(ctx)
			check(b, len(out)*len(out[0].Posts), 500, err)
		}
	})

	b.Run("gorm", func(b *testing.B) {
		b.ReportAllocs()

		for b.Loop() {
			var out []*User

			err := l.gorm.WithContext(ctx).
				Preload("Posts").
				Order("id").
				Limit(100).
				Find(&out).
				Error
			check(b, len(out)*len(out[0].Posts), 500, err)
		}
	})

	b.Run("bun", func(b *testing.B) {
		b.ReportAllocs()

		for b.Loop() {
			var out []*User

			err := l.bun.NewSelect().
				Model(&out).
				Relation("Posts").
				Order("id").
				Limit(100).
				Scan(ctx)
			check(b, len(out)*len(out[0].Posts), 500, err)
		}
	})

	b.Run("ent", func(b *testing.B) {
		b.ReportAllocs()

		for b.Loop() {
			out, err := l.ent.User.Query().
				WithPosts().
				Order(entuser.ByID()).
				Limit(100).
				All(ctx)
			check(b, len(out)*len(out[0].Edges.Posts), 500, err)
		}
	})
}

func BenchmarkInsert(b *testing.B) {
	l := setup(b)
	ctx := context.Background()
	n := 0
	fresh := func() *User {
		n++

		return &User{
			Name:  "new",
			Email: fmt.Sprint("new", n, "@example.com"),
			Age:   30,
		}
	}

	b.Run("sql", func(b *testing.B) {
		b.ReportAllocs()

		for b.Loop() {
			u := fresh()
			now := time.Now()

			err := l.plain.QueryRowContext(
				ctx,
				"INSERT INTO users (created_at, updated_at, name, email, age, bio) VALUES ($1, $2, $3, $4, $5, $6) RETURNING id",
				now,
				now,
				u.Name,
				u.Email,
				u.Age,
				u.Bio,
			).Scan(&u.ID)
			if err != nil {
				b.Fatal(err)
			}
		}
	})

	b.Run("ant", func(b *testing.B) {
		b.ReportAllocs()

		for b.Loop() {
			if err := l.ant.Users.Insert(ctx, fresh()); err != nil {
				b.Fatal(err)
			}
		}
	})

	b.Run("gorm", func(b *testing.B) {
		b.ReportAllocs()

		for b.Loop() {
			if err := l.gorm.WithContext(ctx).Create(fresh()).Error; err != nil {
				b.Fatal(err)
			}
		}
	})

	b.Run("bun", func(b *testing.B) {
		b.ReportAllocs()

		for b.Loop() {
			if _, err := l.bun.NewInsert().Model(fresh()).Exec(ctx); err != nil {
				b.Fatal(err)
			}
		}
	})

	b.Run("ent", func(b *testing.B) {
		b.ReportAllocs()

		for b.Loop() {
			u := fresh()
			if _, err := l.ent.User.Create().
				SetName(u.Name).
				SetEmail(u.Email).
				SetAge(u.Age).
				SetNillableBio(u.Bio).
				Save(ctx); err != nil {
				b.Fatal(err)
			}
		}
	})
}

// BenchmarkUpdateOneColumn updates one row by key without loading it first.
func BenchmarkUpdateOneColumn(b *testing.B) {
	l := setup(b)
	ctx := context.Background()
	age := 0

	b.Run("sql", func(b *testing.B) {
		b.ReportAllocs()

		for b.Loop() {
			age++
			if _, err := l.plain.ExecContext(
				ctx,
				"UPDATE users SET age = $1, updated_at = $2 WHERE id = $3",
				age%90,
				time.Now(),
				500,
			); err != nil {
				b.Fatal(err)
			}
		}
	})

	b.Run("ant", func(b *testing.B) {
		b.ReportAllocs()

		for b.Loop() {
			age++
			if _, err := l.ant.Users.Where(ant.Eq(&Users.ID, 500)).
				Update(ctx, ant.Set(&Users.Age, age%90)); err != nil {
				b.Fatal(err)
			}
		}
	})

	b.Run("gorm", func(b *testing.B) {
		b.ReportAllocs()

		for b.Loop() {
			age++
			if err := l.gorm.WithContext(ctx).
				Model(&User{}).
				Where("id = ?", 500).
				Update("age", age%90).
				Error; err != nil {
				b.Fatal(err)
			}
		}
	})

	b.Run("bun", func(b *testing.B) {
		b.ReportAllocs()

		for b.Loop() {
			age++
			if _, err := l.bun.NewUpdate().
				Model((*User)(nil)).
				Set("age = ?", age%90).
				Set("updated_at = ?", time.Now()).
				Where("id = ?", 500).
				Exec(ctx); err != nil {
				b.Fatal(err)
			}
		}
	})

	b.Run("ent", func(b *testing.B) {
		b.ReportAllocs()

		for b.Loop() {
			age++
			if _, err := l.ent.User.Update().
				Where(entuser.ID(500)).
				SetAge(age % 90).
				Save(ctx); err != nil {
				b.Fatal(err)
			}
		}
	})
}

// BenchmarkLoadEditSave is a request that loads a user, changes one field and saves
// it: two statements in every library. ant finds the change by comparing with the
// copy it kept when loading; the others are told which column to write.
func BenchmarkLoadEditSave(b *testing.B) {
	l := setup(b)
	ctx := context.Background()
	age := 0

	b.Run("sql", func(b *testing.B) {
		b.ReportAllocs()

		for b.Loop() {
			u := new(User)

			err := l.plain.QueryRowContext(ctx, "SELECT "+columns+" FROM users WHERE id = $1", 500).
				Scan(&u.ID, &u.CreatedAt, &u.UpdatedAt, &u.Name, &u.Email, &u.Age, &u.Bio)
			if err != nil {
				b.Fatal(err)
			}

			age++
			u.Age = age % 90

			if _, err := l.plain.ExecContext(
				ctx,
				"UPDATE users SET age = $1, updated_at = $2 WHERE id = $3",
				u.Age,
				time.Now(),
				u.ID,
			); err != nil {
				b.Fatal(err)
			}
		}
	})

	b.Run("ant", func(b *testing.B) {
		b.ReportAllocs()

		for b.Loop() {
			session := ant.Session(l.ant)

			u, err := session.Users.Find(ctx, int64(500))
			if err != nil {
				b.Fatal(err)
			}

			age++
			u.Age = age % 90

			if err := session.SaveChanges(ctx); err != nil {
				b.Fatal(err)
			}
		}
	})

	b.Run("gorm", func(b *testing.B) {
		b.ReportAllocs()

		for b.Loop() {
			var u User
			if err := l.gorm.WithContext(ctx).First(&u, 500).Error; err != nil {
				b.Fatal(err)
			}

			age++
			if err := l.gorm.WithContext(ctx).
				Model(&u).
				Update("age", age%90).
				Error; err != nil {
				b.Fatal(err)
			}
		}
	})

	b.Run("bun", func(b *testing.B) {
		b.ReportAllocs()

		for b.Loop() {
			var u User
			if err := l.bun.NewSelect().Model(&u).Where("id = ?", 500).Scan(ctx); err != nil {
				b.Fatal(err)
			}

			age++
			u.Age = age % 90
			u.UpdatedAt = time.Now()

			if _, err := l.bun.NewUpdate().
				Model(&u).
				Column("age", "updated_at").
				WherePK().
				Exec(ctx); err != nil {
				b.Fatal(err)
			}
		}
	})

	b.Run("ent", func(b *testing.B) {
		b.ReportAllocs()

		for b.Loop() {
			u, err := l.ent.User.Get(ctx, 500)
			if err != nil {
				b.Fatal(err)
			}

			age++
			if _, err := u.Update().SetAge(age % 90).Save(ctx); err != nil {
				b.Fatal(err)
			}
		}
	})
}
