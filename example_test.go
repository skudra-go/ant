package ant_test

import (
	"context"
	"database/sql"
	"fmt"
	"log"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/skudra-go/ant"
	"github.com/skudra-go/ant/dialect/postgres"
)

// The examples print the SQL ant writes, so they run with no database: nothing
// here connects until a query runs. tests/features has one runnable example per
// feature against real PostgreSQL.

type User struct {
	ant.Base
	Name  string
	Email string `ant:"unique"`
	Age   int
	Bio   *string
	Posts []Post
}

type Post struct {
	ant.Base
	UserID    int64
	Title     string
	Published bool
	User      *User
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

func db() *AppDB {
	pool, err := sql.Open("pgx", "postgres://nobody@127.0.0.1:1/none")
	if err != nil {
		log.Fatal(err)
	}

	out, err := ant.Wrap[AppDB](postgres.Dialect, pool)
	if err != nil {
		log.Fatal(err)
	}

	return out
}

func ctx() context.Context { return context.Background() }

func show(text string, args []any, err error) {
	if err != nil {
		log.Fatal(err)
	}

	fmt.Println(text)
	fmt.Println(args)
}

func ExampleEq() {
	show(db().Users.Where(ant.Eq(&Users.Name, "Ada")).ToSQL())
	// Output:
	// SELECT "users"."id", "users"."created_at", "users"."updated_at", "users"."name", "users"."email", "users"."age", "users"."bio" FROM "users" WHERE "users"."name" = $1
	// [Ada]
}

func ExampleOr() {
	show(
		db().Users.Column(&Users.ID).
			Where(ant.Or(ant.Lt(&Users.Age, 18), ant.IsNull(&Users.Bio))).
			ToSQL(),
	)
	// Output:
	// SELECT "users"."id" FROM "users" WHERE ("users"."age" < $1 OR "users"."bio" IS NULL)
	// [18]
}

func ExampleIn() {
	show(db().Users.Column(&Users.ID).Where(ant.In(&Users.Age, 18, 21)).ToSQL())
	// Output:
	// SELECT "users"."id" FROM "users" WHERE "users"."age" IN ($1, $2)
	// [18 21]
}

func ExampleQuery_OrderByDesc() {
	show(db().Users.Column(&Users.ID).OrderByDesc(&Users.Age).ThenBy(&Users.Name).Limit(10).ToSQL())
	// Output:
	// SELECT "users"."id" FROM "users" ORDER BY "users"."age" DESC, "users"."name" LIMIT $1
	// [10]
}

func ExampleHas() {
	show(
		db().Users.Column(&Users.ID).
			Where(ant.Has(&Users.Posts, ant.Eq(&Posts.Published, true))).
			ToSQL(),
	)
	// Output:
	// SELECT "users"."id" FROM "users" WHERE EXISTS (SELECT 1 FROM "posts" AS "ant_related" WHERE "ant_related"."user_id" = "users"."id" AND ("ant_related"."published" = $1))
	// [true]
}

func ExampleQuery_DeleteSQL() {
	show(db().Posts.Where(ant.Eq(&Posts.Published, false)).DeleteSQL(ctx()))
	// Output:
	// DELETE FROM "posts" WHERE "posts"."published" = $1
	// [false]
}

func ExampleQuery_IncludeJoined() {
	text, _, err := db().Posts.Column(&Posts.ID).IncludeJoined(&Posts.User).ToSQL()
	if err != nil {
		log.Fatal(err)
	}

	fmt.Println(text[:len("SELECT \"posts\".\"id\"")])
	// Output:
	// SELECT "posts"."id"
}
