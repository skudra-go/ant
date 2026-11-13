---
name: ant
description: Writes, reviews and debugs Go code that uses ant (github.com/skudra-go/ant), a typed ORM where columns are field pointers such as &Users.Age instead of strings. Use it whenever a Go file imports github.com/skudra-go/ant, a go.mod requires it, or the user mentions ant models, ant.Table, ant.Fields, SaveChanges, ant migrations or anttest. ant is not GORM, Bun or ent, and their APIs do not work here.
---

# Writing Go with ant

ant is a Go ORM. Models are plain structs, columns are pointers to the fields of a
probe value, and there is no code generation. It needs Go 1.27 or later, for generic
methods.

Your training data probably has little or no ant in it. **Do not guess from GORM, Bun,
ent or sqlx.** Calls such as `db.Where("age > ?", 18)`, `Preload("Posts")`,
`AutoMigrate`, `db.Model(&u).Updates(...)` or `First(&u)` do not exist. When you are
unsure of a name, check before you write it:

- `go doc github.com/skudra-go/ant <Name>`, or `go doc github.com/skudra-go/ant.Query`
  for a type's methods. This matches the version in the user's `go.mod`.
- <https://skudra.net/llms.txt> lists every docs page as Markdown, and
  <https://skudra.net/llms-full.txt> is all of them in one file. Fetch the page for
  the feature you are using.
- The feature tests in the ant repo, `tests/features/<section>/`, are small,
  runnable examples of every feature.

## The shape of an ant program

```go
type User struct {
	ant.Base // ID int64, CreatedAt, UpdatedAt; optional
	Name     string
	Email    string `ant:"unique"`
	Age      int
	Bio      *string // nullable
	Posts    []Post  // a relation: one-to-many through Post.UserID
}

type Post struct {
	ant.Base
	UserID    int64
	Title     string
	Published bool
	User      *User // a relation: many-to-one through Post.UserID
}

// The database: embeds ant.Context and holds one ant.Table per model.
type AppDB struct {
	ant.Context
	Users ant.Table[User]
	Posts ant.Table[Post]
}

// Probes: one per model, at package level. &Users.Age names the users.age column.
var (
	Users = ant.Fields[User]()
	Posts = ant.Fields[Post]()
)
```

```go
db, err := ant.Open[AppDB](ctx, postgres.Dialect, dsn)
if err != nil {
	return err
}

defer db.Close()

adults, err := db.Users.
	Where(ant.Gt(&Users.Age, 18)).
	Include(&Users.Posts).
	OrderBy(&Users.Name).
	Limit(10).
	All(ctx)
```

- `ant.Open` fills in every `ant.Table` field and fails at startup if a model can't
  be mapped. Open once per process. Never construct `AppDB` yourself.
- The application imports the driver: `_ "github.com/jackc/pgx/v5/stdlib"` for
  PostgreSQL, `_ "modernc.org/sqlite"`, `_ "github.com/go-sql-driver/mysql"` or
  `_ "github.com/microsoft/go-mssqldb"`.
- Dialects: `postgres.Dialect`, `mysql.Dialect`, `mysql.MariaDB`, `sqlite.Dialect`,
  `sqlserver.Dialect`, from `github.com/skudra-go/ant/dialect/<name>`.
- DSN settings that matter: MySQL and MariaDB need `parseTime=true`. SQLite wants
  `_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)&_txlock=immediate`.

## Columns are field pointers

Every place that names a column takes `&Probe.Field`, never a string. The compiler
checks the field name and that the value has the field's type:

```go
db.Users.Where(ant.Eq(&Users.Name, "Ada"))     // fine
db.Users.Where(ant.Gt(&Users.Age, 18))         // fine: 18 is an int, like Age
db.Users.Where(ant.Eq(&Users.Bio, nil))        // IS NULL
db.Posts.Where(ant.Eq(&Posts.Published, true)) // each model has its own probe
```

Mistakes the compiler can't see, which return an error when the query runs:

- A pointer from another model's probe: `db.Posts.Where(ant.Gt(&Users.Age, 18))`
  gives `ant.ErrWrongModel`. In a join, conditions on either model are fine.
- A pointer that isn't from a probe, such as `&age` or `&user.Age`, gives
  `ant.ErrNotProbe`. Probes are only for naming columns; they never hold data.
- A key of the wrong type: `db.Users.Find(ctx, "1")` gives `ant.ErrKeyType`.

Calls that take columns of mixed types take `ant.FieldRef`, made with
`ant.F(&Users.Name)`.

## Models

- Table names are the struct name in snake case, plural: `OrderItem` is
  `order_items`. Columns are snake case: `UserID` is `user_id`.
- A field named `ID` is the primary key. A zero integer key means "generate one",
  and the database's key is copied back after an insert.
- `time.Time` fields named `CreatedAt` and `UpdatedAt` are maintained by ant, on any
  model. `ant.Base` is just `ID`, `CreatedAt` and `UpdatedAt`.
- A nullable column is a pointer or `sql.Null[T]` field. A plain field can't read
  `NULL`. Zero values are always written, never skipped.
- Tags: `ant:"-"` (not a column), `column=name`, `unique`, `default=expr` (a zero
  value means "use the database default"), `readonly`, `writeonly`, `immutable`,
  `computed` (filled by `Query.With`), `version` (optimistic concurrency),
  `embedded` / `embedded,prefix=x_`. Combine with commas.
- A field of type `ant.DeletedAt` turns on soft deletes. `ant.JSON[T]`
  stores a value as JSON.
- Everything else is configured in typed Go, with a `Configure` method on the model:

```go
func (Post) Configure(b *ant.ModelBuilder[Post]) {
	b.Table("articles")
	b.Index(&Posts.Title)
	b.BelongsTo(&Posts.User).OnDelete(ant.Cascade)
}
```

Relations are found by name: `[]Post` on `User` with `UserID` on `Post` is
one-to-many, `*User` on `Post` is many-to-one. Relation slices hold values (`[]Post`),
not pointers. When the names don't fit, declare it with `b.HasMany`, `b.HasOne`,
`b.BelongsTo` or `b.ManyToMany` and `.ForeignKey(&Posts.AuthorID)`. A relation is not
a column, so it can't go in `Where` or `OrderBy`.

## Queries

A query is built from a table (`db.Users`) and runs at the call that takes `ctx`.
Builder methods never return errors; the terminal call returns everything collected.

| Call | Returns |
| --- | --- |
| `All(ctx)` | `[]*T`; no rows is an empty slice, not an error |
| `First(ctx)` | `*T` or `ant.ErrNotFound`; order the query first |
| `Single(ctx)` | `*T`, `ant.ErrNotFound` or `ant.ErrMultipleRows` |
| `Find(ctx, key)` | `*T` by primary key, or `ant.ErrNotFound` |
| `Count(ctx)`, `Exists(ctx)` | `int64`, `bool` |
| `Each(ctx)` | an `iter.Seq2[T, error]` that streams; no `Include`, not tracked |
| `Page(ctx, page, size)`, `After(ctx, cursor, size)` | numbered pages, cursor pages |
| `Chunk(ctx, size, fn)` | batches; `ChunkByKey` if `fn` changes the rows |
| `ant.Pluck(ctx, query, &Users.Name)` | one column as `[]string` |

Conditions: `ant.Eq`, `Ne`, `Gt`, `Gte`, `Lt`, `Lte`, `Between`, `In`, `NotIn`,
`Like`, `ILike`, `IsNull`, `IsNotNull`, `EqCol` (column to column), `And`, `Or`, `Not`,
`InTuples`, `AnyOf`. Several conditions in one `Where`, or several `Where` calls, are
joined with `AND`. `ant.In(&Users.Age)` with no values matches nothing.

```go
users, err := db.Users.
	Where(
		ant.Or(
			ant.Lt(&Users.Age, 18),
			ant.Like(&Users.Name, "A%"),
		),
		ant.Not(ant.Eq(&Users.Name, "Bob")),
	).
	OrderBy(&Users.Age).
	ThenByDesc(&Users.Name).
	All(ctx)
```

Relations load only when asked. An unloaded relation is `nil`; a loaded one with no
rows is an empty slice. There is no lazy loading. `Include` (for `[]T`) and
`IncludeOne` (for `*T`) add one query per relation, however many parents:

```go
users, err := db.Users.
	Include(&Users.Posts, func(q *ant.Query[Post]) *ant.Query[Post] {
		return q.Where(ant.Eq(&Posts.Published, true)).OrderByDesc(&Posts.CreatedAt)
	}).
	All(ctx)

posts, err := db.Posts.IncludeOne(&Posts.User).All(ctx)
```

Filter by a relation without loading it with `ant.Has(&Users.Posts, conds...)`,
`ant.HasNone` and `ant.HasOne`. Joins return pairs through `Select`; results in your
own struct come from `ant.Project`:

```go
titles, err := db.Posts.
	Join(&Posts.User).
	Where(ant.Eq(&Users.Name, "Alice")).
	Select(func(p *Post, u *User) string { return u.Name + ": " + p.Title }).
	All(ctx)
```

Raw SQL scans into models, any struct or a single value. Use `?` placeholders on
every database, and pass values as arguments, never in the text:

```go
rows, err := ant.Raw[User](db, `SELECT * FROM users WHERE age > ?`, 18).All(ctx) // []User
count, err := ant.Raw[int](db, `SELECT count(*) FROM users`).First(ctx)
```

## Writes

```go
alice := &User{Name: "Alice", Email: "alice@example.com"}
err := db.Users.Insert(ctx, alice) // fills alice.ID, CreatedAt, UpdatedAt

// Bulk update: explicit assignments, so zeros and nil are written.
n, err := db.Users.
	Where(ant.Eq(&Users.ID, alice.ID)).
	Update(ctx, ant.Set(&Users.Bio, nil), ant.Inc(&Users.Age, 1))

n, err = db.Users.Where(ant.Lt(&Users.Age, 18)).Delete(ctx)
err = db.Users.DeleteByID(ctx, alice.ID)

// Insert, or update on a unique conflict.
wrote, err := db.Users.Upsert(ctx, alice, ant.OnConflict(&Users.Email))
```

- `Update` and `Delete` hang off a query, not the table: `db.Users.Where(...).Update`.
  With no condition they return `ant.ErrNoCondition`; `db.Users.Unfiltered()` opts out
  on purpose. One assignment per field: `Set` and `Inc` on the same field is an error.
- `Insert` also inserts the new entities a struct points at, parents first, and fills
  in foreign keys. `InsertOnly` leaves relations alone. `InsertMany` is for
  thousands of rows.
- Bulk `Update` and `Delete` run no hooks and don't change tracked structs.
- `FirstOrCreate(ctx, entity, conds...)` reads or inserts, safely under races when
  the condition is on a unique column.
- Constraint errors are the same on every database: `ant.IsUniqueViolation(err)`,
  `ant.IsForeignKeyViolation(err)`, `ant.IsRetryable(err)`.

## Change tracking and sessions

Entities returned by `All`, `First`, `Single`, `Find`, `Include` and `Insert` are
tracked. Change the struct and call `SaveChanges`, which writes only the changed
columns, in one transaction:

```go
session := ant.Session(db) // one per request, job or goroutine

user, err := session.Users.Find(ctx, id)
if err != nil {
	return err
}

user.Age++

err = session.SaveChanges(ctx)
// UPDATE "users" SET "age" = $1, "updated_at" = $2 WHERE "users"."id" = $3
```

- A tracking context serves **one operation at a time**. Two goroutines or two
  requests on it get `ant.ErrConcurrentUse`. Call `ant.Session(db)` per request, or
  open the shared root with `ant.WithTracking(false)` and use
  `ant.Session(db, ant.WithTracking(true))` where you save.
- Close only the root from `Open`. A session needs no closing; closing it closes the
  shared pool.
- `ant.Add(ctx, db, entity)` queues an insert and `ant.Remove(ctx, db, entity)` a
  delete, for the next `SaveChanges`.
- **On a loaded relation, the slice is the truth.** Removing a post from
  `user.Posts` and calling `SaveChanges` deletes that post (or clears its foreign key
  if the key is nullable). Appending inserts. A relation that was never loaded (`nil`)
  is left alone.
- `NoTracking()` on a query skips tracking for read-only work and is faster. `Each`,
  `Column`, `Omit`, `Raw`, `Select` and `Project` results are never tracked, so
  changing them saves nothing.
- A context holds one instance per row: a second query for the same row returns the
  same pointer, unsaved changes included. `Reload(ctx, e)` rereads it.
- `SaveChanges` returns `ant.ErrConcurrency` when the row was deleted, or its
  version changed, since it was loaded.
- Hooks are methods on the model: `BeforeInsert(ctx context.Context) error`,
  `AfterLoad`, `BeforeUpdate`, `BeforeDelete` and the rest. A query made in a hook
  must use the hook's `ctx`, or it fails with `ErrConcurrentUse`.

## Transactions

```go
err := db.Tx(ctx, func(tx *AppDB) error {
	bob := &User{Name: "Bob", Email: "bob@example.com"}
	if err := tx.Users.Insert(ctx, bob); err != nil {
		return err
	}

	return tx.Posts.Insert(ctx, &Post{UserID: bob.ID, Title: "First"})
})
```

- Inside the function use `tx`, not `db`; work on `db` runs outside the transaction.
  `tx` is your own `*AppDB` type.
- It commits on `nil` and rolls back on an error or panic. A `Tx` inside a `Tx` is a
  savepoint. `SaveChanges` inside one uses a savepoint, and a rollback undoes the
  tracker's view of the save too.
- Options go after the function: `ant.Isolation(sql.LevelSerializable)`,
  `ant.ReadOnly()`, `ant.Attempts(3)` to rerun after a deadlock or serialization
  failure.
- `ForUpdate()` on a query locks the rows it reads. SQLite and SQL Server return
  `ant.ErrUnsupported`.
- A transaction is one connection: use it from one goroutine at a time.

## Errors

Match with `errors.Is` and `errors.As`, never by text. The common ones:
`ErrNotFound`, `ErrMultipleRows`, `ErrNoCondition`, `ErrConcurrency`,
`ErrConcurrentUse`, `ErrUnsupported` (the database can't do it; ant refuses before
sending SQL), `ErrNotProbe`, `ErrWrongModel`, `ErrKeyType`, `ErrNoForeignKey`.
`ant.FailedSQL(err)` returns the statement behind an error, without values.

## Databases

PostgreSQL has everything. MySQL, MariaDB, SQLite and SQL Server have queries,
writes, relations, tracking and transactions, but **no migrations and no
`EnsureCreated`**: create their tables with your own SQL or tool. Some features vary:
SQL Server has no upsert (use `ant.Merge`), `MERGE` needs PostgreSQL 15 or SQL Server,
`UpdateReturning` is not on MySQL or MariaDB. When unsure, fetch
<https://skudra.net/reference/databases.md>.

## Migrations (PostgreSQL)

Structs are the source of truth. `diff` writes a plain SQL file from what changed;
`up` applies it. There are no down migrations: undo with a new migration. The tool is
a small program in the user's module:

```go
package main

import (
	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/skudra-go/ant/dialect/postgres"
	"github.com/skudra-go/ant/migrate"

	"example.com/app/models"
)

func main() {
	migrate.Main[models.AppDB](postgres.Dialect)
}
```

```bash
go run ./cmd/migrate diff add_bio   # writes migrations/versions/0002_add_bio.up.sql
go run ./cmd/migrate up             # needs DATABASE_URL or --dsn
go run ./cmd/migrate check          # in CI: fails if a struct changed without a migration
```

After changing a model, run `diff` and show the user the SQL; don't hand-write the
migration it would generate. `db.EnsureCreated(ctx)` creates tables straight from the
structs, for experiments and tests only.

## Tests

Test against a real database; ant has no fake. `anttest.Isolate` gives each test a
context in a transaction that rolls back when the test ends:

```go
var shared *AppDB // opened once, in TestMain

func TestSignup(t *testing.T) {
	db := anttest.Isolate(t, shared)

	err := db.Users.Insert(t.Context(), &User{Name: "Ann", Email: "ann@example.com"})
	if err != nil {
		t.Fatal(err)
	}
}
```

`anttest.NewFactory` builds valid entities, and `q.ToSQL()` returns a query's SQL
without a database.

## Before you finish

- Every column is a `&Probe.Field` from that model's probe, declared with
  `ant.Fields[T]()`.
- Every query ends in a terminal call that takes `ctx`, and its error is checked.
- Each request or goroutine that tracks has its own `ant.Session`.
- Code inside `db.Tx` uses `tx`.
- Names you weren't sure of were checked with `go doc` or the docs.
- If the project uses antvet, run it: `go vet -vettool=$(which antvet) ./...`. It
  catches wrong-model and non-probe pointers and wrong key types. Install it with
  `go install github.com/skudra-go/ant/tools/antvet/cmd/antvet@latest`.
