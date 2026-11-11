// Package benchmarks measures ant against plain database/sql on PostgreSQL. Run
// them with make bench, which needs the database from make db-up.
package benchmarks

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"testing"

	"github.com/skudra-go/ant"
	"github.com/skudra-go/ant/dialect/postgres"
	_ "github.com/skudra-go/ant/internal/testdb" // registers the drivers
)

type User struct {
	ant.Base
	Name  string
	Email string
	Age   int
	Bio   *string
	Posts []Post
}

type Post struct {
	ant.Base
	UserID int64
	Title  string
}

type AppDB struct {
	ant.Context
	Users ant.Table[User]
	Posts ant.Table[Post]
}

var (
	Users = ant.Fields[User]()
	Posts = ant.Fields[Post]()
)

const ddl = `
	DROP SCHEMA IF EXISTS ant_bench CASCADE;
	CREATE SCHEMA ant_bench;
	CREATE TABLE ant_bench.users (id BIGSERIAL PRIMARY KEY, created_at TIMESTAMPTZ NOT NULL, updated_at TIMESTAMPTZ NOT NULL,
		name TEXT NOT NULL, email TEXT NOT NULL, age INT NOT NULL, bio TEXT);
	CREATE TABLE ant_bench.posts (id BIGSERIAL PRIMARY KEY, created_at TIMESTAMPTZ NOT NULL, updated_at TIMESTAMPTZ NOT NULL,
		user_id BIGINT NOT NULL REFERENCES ant_bench.users, title TEXT NOT NULL);
	CREATE INDEX ON ant_bench.posts (user_id)`

// setup opens a database with 1000 users and 5000 posts.
func setup(b *testing.B, options ...ant.Option) (*AppDB, *sql.DB) {
	b.Helper()

	dsn := os.Getenv("ANT_PG_DSN")
	if dsn == "" {
		dsn = "postgres://ant:ant@127.0.0.1:55432/ant_test?sslmode=disable"
	}

	dsn += "&search_path=ant_bench"
	ctx := context.Background()

	raw, err := sql.Open("pgx", dsn)
	if err != nil {
		b.Skip(err)
	}

	b.Cleanup(func() { raw.Close() })

	if err := raw.PingContext(ctx); err != nil {
		b.Skip("PostgreSQL unreachable: ", err)
	}

	if _, err := raw.ExecContext(ctx, ddl); err != nil {
		b.Fatal(err)
	}

	db, err := ant.Open[AppDB](ctx, postgres.Dialect, dsn, options...)
	if err != nil {
		b.Fatal(err)
	}

	b.Cleanup(func() { db.Close() })

	users := make([]*User, 1000)
	for i := range users {
		users[i] = &User{
			Name:  fmt.Sprint("user", i),
			Email: fmt.Sprint(i, "@example.com"),
			Age:   i % 80,
		}
	}

	if _, err := db.Users.InsertMany(ctx, users); err != nil {
		b.Fatal(err)
	}

	posts := make([]*Post, 0, 5000)

	for _, u := range users {
		for i := range 5 {
			posts = append(posts, &Post{UserID: u.ID, Title: fmt.Sprint("post", i)})
		}
	}

	if _, err := db.Posts.InsertMany(ctx, posts); err != nil {
		b.Fatal(err)
	}

	return db, raw
}

func BenchmarkRead1000Rows(b *testing.B) {
	db, raw := setup(b, ant.WithTracking(false))
	ctx := context.Background()

	b.Run("database-sql", func(b *testing.B) {
		b.ReportAllocs()

		for b.Loop() {
			rows, err := raw.QueryContext(
				ctx,
				`SELECT id, created_at, updated_at, name, email, age, bio FROM users`,
			)
			if err != nil {
				b.Fatal(err)
			}

			var out []*User

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

			rows.Close()

			if len(out) != 1000 {
				b.Fatal(len(out))
			}
		}
	})

	b.Run("ant", func(b *testing.B) {
		b.ReportAllocs()

		for b.Loop() {
			out, err := db.Users.All(ctx)
			if err != nil || len(out) != 1000 {
				b.Fatal(err, len(out))
			}
		}
	})

	b.Run("ant-tracked", func(b *testing.B) {
		b.ReportAllocs()

		tracked, err := ant.Wrap[AppDB](postgres.Dialect, db.DB())
		if err != nil {
			b.Fatal(err)
		}

		for b.Loop() {
			session := ant.Session(tracked)

			out, err := session.Users.All(ctx)
			if err != nil || len(out) != 1000 {
				b.Fatal(err, len(out))
			}
		}
	})
}

func BenchmarkFindOne(b *testing.B) {
	db, raw := setup(b, ant.WithTracking(false))
	ctx := context.Background()

	b.Run("database-sql", func(b *testing.B) {
		b.ReportAllocs()

		for b.Loop() {
			var u User

			err := raw.QueryRowContext(ctx, `SELECT id, created_at, updated_at, name, email, age, bio FROM users WHERE id = $1`, 500).
				Scan(&u.ID, &u.CreatedAt, &u.UpdatedAt, &u.Name, &u.Email, &u.Age, &u.Bio)
			if err != nil {
				b.Fatal(err)
			}
		}
	})

	b.Run("ant", func(b *testing.B) {
		b.ReportAllocs()

		for b.Loop() {
			if _, err := db.Users.Find(ctx, int64(500)); err != nil {
				b.Fatal(err)
			}
		}
	})

	b.Run("ant-where", func(b *testing.B) {
		b.ReportAllocs()

		for b.Loop() {
			if _, err := db.Users.Where(ant.Eq(&Users.Age, 30), ant.Like(&Users.Name, "user%")).
				OrderBy(&Users.Name).
				First(ctx); err != nil {
				b.Fatal(err)
			}
		}
	})
}

func BenchmarkInclude(b *testing.B) {
	db, _ := setup(b, ant.WithTracking(false))
	ctx := context.Background()

	b.ReportAllocs()

	for b.Loop() {
		out, err := db.Users.Limit(100).Include(&Users.Posts).All(ctx)
		if err != nil || len(out[0].Posts) != 5 {
			b.Fatal(err)
		}
	}
}

func BenchmarkSaveChanges(b *testing.B) {
	db, _ := setup(b)
	ctx := context.Background()
	session := ant.Session(db)

	users, err := session.Users.All(ctx)
	if err != nil {
		b.Fatal(err)
	}

	b.Run("nothing-changed-1000-tracked", func(b *testing.B) {
		b.ReportAllocs()

		for b.Loop() {
			if err := session.SaveChanges(ctx); err != nil {
				b.Fatal(err)
			}
		}
	})

	b.Run("one-changed-1000-tracked", func(b *testing.B) {
		b.ReportAllocs()

		i := 0

		for b.Loop() {
			i++
			users[0].Age = i

			if err := session.SaveChanges(ctx); err != nil {
				b.Fatal(err)
			}
		}
	})
}

func BenchmarkInsert(b *testing.B) {
	db, raw := setup(b, ant.WithTracking(false))
	ctx := context.Background()

	b.Run("database-sql", func(b *testing.B) {
		b.ReportAllocs()

		for b.Loop() {
			var id int64

			err := raw.QueryRowContext(
				ctx,
				`INSERT INTO users (created_at, updated_at, name, email, age) VALUES (now(), now(), $1, $2, $3) RETURNING id`,
				"x",
				"x",
				1,
			).Scan(&id)
			if err != nil {
				b.Fatal(err)
			}
		}
	})

	b.Run("ant", func(b *testing.B) {
		b.ReportAllocs()

		for b.Loop() {
			if err := db.Users.Insert(ctx, &User{Name: "x", Email: "x", Age: 1}); err != nil {
				b.Fatal(err)
			}
		}
	})

	b.Run("ant-many-1000", func(b *testing.B) {
		b.ReportAllocs()

		for b.Loop() {
			users := make([]*User, 1000)
			for i := range users {
				users[i] = &User{Name: "x", Email: "x", Age: 1}
			}

			if _, err := db.Users.InsertMany(ctx, users); err != nil {
				b.Fatal(err)
			}
		}
	})
}
