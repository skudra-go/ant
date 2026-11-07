# Vision

ant aims to bring the developer experience of Entity Framework Core and Laravel Eloquent to Go without code generation, stringly-typed queries, or hidden error handling. This document describes the intended result. The [roadmap](ROADMAP.md) shows what exists so far.

## Requirements

ant requires Go 1.27 or later because the API depends on these language features:

| Feature | Since | Used for |
| --- | --- | --- |
| `iter.Seq2` | [Go 1.23](https://go.dev/doc/go1.23) | Streaming with `Each` |
| `new(expr)` | [Go 1.26](https://go.dev/doc/go1.26) | Nullable values, as in `user.Bio = new("text")` |
| Self-referential generic constraints | [Go 1.26](https://go.dev/doc/go1.26) | `Entity[E Entity[E]]` |
| Generic methods | [Go 1.27](https://go.dev/doc/go1.27) | `Join[J]`, `Include[R]`, `Select[R]` in fluent chains |
| Field selectors as struct literal keys | [Go 1.27](https://go.dev/doc/go1.27) | `User{ID: 1}` for fields promoted from `ant.Base` |

Interface methods [cannot be generic](https://go.dev/doc/go1.27), and interface methods cannot be implemented by generic methods. That is why ant's builders are concrete structs.

## Core design decisions

1. **Plain structs.** Models are ordinary Go structs, with no required base type and no generated model code. An optional embeddable `ant.Base` provides `ID`, `CreatedAt`, and `UpdatedAt`.
2. **Field probes instead of strings.** `ant.Fields[T]()` returns a `*T` probe, and queries reference columns as `&U.Age`. Generic condition functions make value-type mismatches compile errors.
3. **Typed end to end.** Ordering, grouping, eager loading, joins, projections, and assignments take typed field pointers or typed closures. There is no `any` in the public query API except raw SQL arguments.
4. **Generic methods for fluent chains.** Type-changing operations are generic methods on concrete builder types.
5. **EF-style context.** You declare a struct that embeds `ant.Context` and has `ant.Table[T]` fields. `ant.Open[DB]` populates it, and transactions hand back the same typed context.
6. **Errors only at terminal calls.** Builder methods never return errors. They accumulate them, and terminal calls (`All`, `First`, `Find`, `Exec`, `Insert`, `SaveChanges`) return them. Every terminal call takes `context.Context` first.
7. **No hidden I/O.** There is no lazy loading through field access. Eager loading is explicit and batched.
8. **No zero-value guessing.** ant never infers "unset" from a zero value. Partial updates use change tracking or explicit `ant.Set` assignments. Nullable columns use pointers or `sql.Null[T]`.
9. **Opt-in change tracking.** Entities loaded through the context are snapshotted. `SaveChanges` diffs them and writes minimal `UPDATE`s. A `NoTracking()` builder option disables this.
10. **Escape hatches are first-class.** Raw SQL that scans into the same structs is simple and documented.
11. **Dialects are pluggable** behind one interface from the start. PostgreSQL is the reference dialect.
12. **Readability beats cleverness.** Public names read like English. Internals use `unsafe` only where documented and tested.

## Target API

The code in this section was compiled with Go 1.27.3 against a stub of these signatures. Exact signatures may still be refined. See [decisions](DECISIONS.md) for the changes already made.

### Models and context

```go
type User struct {
    ant.Base               // ID, CreatedAt, UpdatedAt
    Name  string
    Email string `ant:"unique"`
    Age   int
    Bio   *string          // nullable
    Posts []Post           // 1:N via Post.UserID (convention)
}

type Post struct {
    ant.Base
    UserID    int64
    Title     string
    Published bool
    User      *User        // N:1
}

type AppDB struct {
    ant.Context
    Users ant.Table[User]
    Posts ant.Table[Post]
}

// Field probes, declared once per model
var (
    U = ant.Fields[User]()
    P = ant.Fields[Post]()
)
```

### Optional fluent configuration

Conventions and a small set of tags cover most models. `Configure` overrides them.

```go
type Entity[E Entity[E]] interface {
    Configure(b *ant.ModelBuilder[E])
}

func (User) Configure(b *ant.ModelBuilder[User]) {
    b.Table("users")
    b.Index(&U.Email).Unique()
    b.HasMany(&U.Posts).ForeignKey(&P.UserID).OnDelete(ant.Cascade)
}
```

### Operations

```go
db, err := ant.Open[AppDB](ctx, postgres.Dialect, dsn)

// Insert a graph
alice := &User{Name: "Alice", Email: "alice@example.com", Age: 30,
    Posts: []Post{{Title: "Hello"}, {Title: "Draft ideas"}}}
err = db.Users.Insert(ctx, alice)

// Find by primary key
user, err := db.Users.Find(ctx, alice.ID)
if errors.Is(err, ant.ErrNotFound) { /* ... */ }

// Change tracking
user.Age++
user.Bio = new("Gopher since 2012")
err = db.SaveChanges(ctx)

// Typed query with eager loading
adults, err := db.Users.
    Where(ant.Gt(&U.Age, 18)).
    OrderBy(&U.Name).
    Include(&U.Posts).
    All(ctx)

// Bulk update and delete
_, err = db.Posts.Where(ant.Eq(&P.UserID, user.ID)).Update(ctx, ant.Set(&P.Published, true))
_, err = db.Posts.Where(ant.Like(&P.Title, "Draft%")).Delete(ctx)

// Join through a relation and project
type Summary struct{ Author, Title string }
summaries, err := db.Posts.
    Join(&P.User).
    Where(ant.Eq(&P.Published, true)).
    Select(func(p *Post, u *User) Summary { return Summary{u.Name, p.Title} }).
    All(ctx)

// Streaming
for post, err := range db.Posts.Where(ant.Eq(&P.Published, true)).Each(ctx) {
    if err != nil { return err }
    fmt.Println(post.Title)
}

// Transactions return the same typed context
err = db.Tx(ctx, func(tx *AppDB) error {
    bob := &User{Name: "Bob", Email: "bob@example.com"}
    if err := tx.Users.Insert(ctx, bob); err != nil { return err }
    return tx.Posts.Insert(ctx, &Post{UserID: bob.ID, Title: "First!"})
})

// Raw SQL into the same structs
rows, err := ant.Raw[User](db, "SELECT * FROM users WHERE age > $1", 18).All(ctx)
```

### Key generic signatures

```go
type Ordered interface{ cmp.Ordered | time.Time }

func Fields[T any]() *T
func Eq[V comparable](field *V, v V) Cond
func Gt[V Ordered](field *V, v V) Cond
func EqCol[V comparable](a, b *V) Cond
func Set[V any](field *V, v V) Assignment

func (c *Context) Tx[DB any](ctx context.Context, f func(*DB) error) error
func (t *Table[T]) Find[K comparable](ctx context.Context, key K) (*T, error)

func (q *Query[T]) Where(c ...Cond) *Query[T]
func (q *Query[T]) OrderBy[V Ordered](field *V) *Query[T]
func (q *Query[T]) Include[R any](rel *[]R) *Query[T]      // 1:N
func (q *Query[T]) IncludeOne[R any](rel **R) *Query[T]    // N:1, 1:1
func (q *Query[T]) Join[J any](rel **J) *Join2[T, J]
func (j *Join2[A, B]) Select[R any](f func(*A, *B) R) *Projection[R]
func (q *Query[T]) Each(ctx context.Context) iter.Seq2[T, error]
```

## How field probes work

A Go struct field has no runtime identity you can pass around, but it does have an address. ant uses that address as the identity.

`ant.Fields[User]()` allocates one `User` value, the probe, and returns a pointer to it. The probe holds no data. It only exists so that `&U.Age` produces a pointer to a known location.

```go
var U = ant.Fields[User]()

ant.Gt(&U.Age, 18)
```

This call does two things:

1. **At compile time**, the signature `Gt[V Ordered](field *V, v V)` infers `V` as `int` from `&U.Age`, so `18` must be an `int`. A string value, an unordered field type, or a mismatched column type in `EqCol` is a compile error.
2. **At runtime**, `Gt` records the pointer. When the query is built, ant walks `User` with `reflect` once, computing each field's offset from the start of the probe, including fields promoted from embedded structs. Subtracting the probe's address from `&U.Age` gives an offset, and the offset identifies the column.

An offset alone is not unique. In the model above, `&U.Base` and `&U.ID` have the same address, because `ID` is the first field of `Base`. ant therefore identifies a field by address and type together: `*ant.Base` and `*int64` resolve to different fields.

Relations work the same way. `&U.Posts` has type `*[]Post`, so `Include[R any](rel *[]R)` infers `R` as `Post`. `&P.User` has type `**User`, so `Join[J any](rel **J)` infers `J` as `User`.

Renaming a field with an editor rewrites `U.Age` like any other field reference. There are no strings to miss.

The probe mechanism depends on two facts: the probe stays allocated while the program runs, and its address does not change. ant keeps a reference to each probe in its registry and allocates it on the heap, where Go does not currently move objects. That second point is an implementation property, not a language guarantee, so it will be covered by a test and recorded as the documented use of `unsafe`. If it needs to be stronger, the registry can use `runtime.Pinner`.

## Known limits

The compiler checks a lot in this design, but not everything. These cases compile and are detected at the terminal call as returned errors:

- **A probe from the wrong model.** `Cond` has no model type parameter, so `db.Posts.Where(ant.Gt(&U.Age, 18))` compiles. ant resolves the probe at the terminal call, sees that `users.age` is not part of the query, and returns an error.
- **A pointer that is not a probe.** `x := 5; ant.Gt(&x, 3)` compiles for the same reason. A `go vet` analyzer is planned to flag this at build time.
- **A primary key of the wrong type.** `Find[K comparable]` avoids `any`, but `K` cannot be tied to the model's key type. `db.Users.Find(ctx, "1")` compiles and returns an error.

These are behaviors worth knowing about:

- **`Select` closures run in Go, not in SQL.** Go has no equivalent of C# expression trees, so ant cannot see inside the closure. It loads the mapped columns of each joined table and calls the function once per row. Column-level `SELECT` lists and SQL expressions will need a separate, explicit API.
- **`Each` is not tracked.** It yields each row by value, so changing the value has no effect on `SaveChanges`. Use `All`, `First`, or `Find` when you intend to save changes.
- **Use a session per unit of work.** A context's tracker keeps every entity it loads until cleared, so a context held for the life of the process grows, and it would mix concurrent requests. Two operations at once on one tracking context return `ErrConcurrentUse`. Share a context opened with `ant.WithTracking(false)`, and give each request `ant.Session(db, ant.WithTracking(true))` where it saves changes.
- **`Ordered` is a closed list.** It covers `cmp.Ordered` and `time.Time`. Other orderable column types, for example a UUID type or a decimal type, need a future extension.

## Migrations

This section summarizes [decision 11](DECISIONS.md#11-migration-model). The [migrations guide](migrations.md) covers everything else. The goal is the simplest model that keeps structs as the source of truth and leaves you free to stop using ant's tooling.

```text
migrations/
├── versions/
│   ├── 0001_create_users.up.sql
│   ├── 0002_add_bio.up.sql
│   └── schema.snapshot.json
└── code/
    └── active_users_view.sql
```

Files in `versions/` run once, in order. Files in `code/` run after them, and rerun when their contents change, which suits views and functions. Everything is plain SQL.

Go cannot load structs from another module at runtime, so the CLI is a small program in your own module:

```go
// cmd/migrate/main.go
func main() {
    migrate.Main[AppDB](postgres.Dialect)
}
```

```sh
go run ./cmd/migrate diff add_bio   # compare structs with the snapshot, write SQL
go run ./cmd/migrate up             # apply pending migrations
go run ./cmd/migrate status         # list applied and pending
go run ./cmd/migrate check          # fail if structs and snapshot differ
go run ./cmd/migrate drift          # compare the live database with the migrations
```

The runner reads from an `io/fs.FS`, so migrations can be embedded with `go:embed`. A small interface replaces the file handling if you want a different layout or storage.

There are no down migrations. To undo a change, write a new forward migration.

## Non-goals

- **No NoSQL.** ant targets relational databases through `database/sql`.
- **No lazy loading through field access.** A field that was not loaded stays empty. This follows from design decision 7.
- **No active record in v1.** There is no `user.Save()`. Saving goes through the context.
- **No code generation for models or queries.** Migration files are generated output, but they are reviewed artifacts, not part of the query API.
- **No hard dependency on a driver.** The core package uses the standard library only. You choose the driver.

## Delivery

Work is planned in phases, starting with foundations and PostgreSQL, then relations, the unit of work, the other dialects, query features, modeling depth, lifecycle and operations, migrations, tooling, and an API freeze at v1.0. The full list will live in `docs/ROADMAP.md`.
