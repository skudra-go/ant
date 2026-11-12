<p align="center">
  <picture>
    <source media="(prefers-color-scheme: dark)" srcset="docs/assets/logo-dark.svg">
    <img src="docs/assets/logo.svg" alt="ant" width="220">
  </picture>
</p>

<p align="center">
  A typed ORM for Go. Plain structs, compiler-checked queries, no code generation.
</p>

<p align="center">
  <a href="https://skudra.net">Documentation</a> ·
  <a href="https://skudra.net/quick-start">Quick start</a> ·
  <a href="https://github.com/skudra-go/ant-examples">Examples</a>
</p>

<p align="center">
  <a href="https://github.com/skudra-go/ant/actions/workflows/pr-ci.yml"><img src="https://github.com/skudra-go/ant/actions/workflows/pr-ci.yml/badge.svg?branch=main" alt="CI"></a>
  <a href="https://pkg.go.dev/github.com/skudra-go/ant"><img src="https://pkg.go.dev/badge/github.com/skudra-go/ant.svg" alt="Go Reference"></a>
  <img src="https://img.shields.io/badge/go-1.27%2B-00ADD8" alt="Go 1.27+">
  <a href="LICENSE"><img src="https://img.shields.io/badge/license-MIT-blue" alt="MIT license"></a>
</p>

---

Columns are field pointers, not strings. Write `ant.Gt(&Users.Age, 18)` and the compiler checks both the column and the value. Rename a field and every query follows.

```go
type User struct {
    ant.Base // ID, CreatedAt, UpdatedAt
    Name     string
    Age      int
    Posts    []Post // 1:N through Post.UserID
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

var Users = ant.Fields[User]()

db, err := ant.Open[AppDB](ctx, postgres.Dialect, dsn)

adults, err := db.Users.
    Where(ant.Gt(&Users.Age, 18)). // ant.Gt(&Users.Age, "18") won't compile
    Include(&Users.Posts).         // one batched query, not one per user
    All(ctx)

adults[0].Age++
err = db.SaveChanges(ctx) // writes only the columns that changed
```

## Install

```sh
go get github.com/skudra-go/ant
go get github.com/jackc/pgx/v5/stdlib  # or the driver for your database
```

Requires Go 1.27 or later, for generic methods.

## Try it in a minute

[examples/hello](https://github.com/skudra-go/ant-examples/tree/main/examples/hello) is one file on SQLite. It needs only Go:

```sh
git clone https://github.com/skudra-go/ant-examples
cd ant-examples/examples/hello
go run .
```

```text
Created Alice
Alice is 30 and has 2 posts
Happy birthday, Alice
```

The first run takes about 30 seconds, mostly compiling the SQLite driver. The [quick start](https://skudra.net/quick-start) walks through the same program.

## Next to GORM, Bun and ent

The same query in each: users over 18 with their posts, sorted by name, 10 at a time. A [test](tools/compare) checks that all four return the same rows.

<table>
<tr>
<td>

**ant**: checked, with nothing generated

```go
users, err := db.Users.
	Include(&Users.Posts).
	Where(ant.Gt(&Users.Age, 18)).
	OrderBy(&Users.Name).
	Limit(10).
	All(ctx)
```

</td>
<td>

**GORM 1.31**: strings, so a typo fails at run time

```go
var users []User

err := db.WithContext(ctx).
	Preload("Posts").
	Where("age > ?", 18).
	Order("name").
	Limit(10).
	Find(&users).
	Error
```

</td>
</tr>
<tr>
<td>

**Bun 1.3**: strings, so a typo fails at run time

```go
var users []User

err := db.NewSelect().
	Model(&users).
	Relation("Posts").
	Where("age > ?", 18).
	Order("name").
	Limit(10).
	Scan(ctx)
```

</td>
<td>

**ent 0.14**: checked, through generated code

```go
users, err := client.User.Query().
	WithPosts().
	Where(user.AgeGT(18)).
	Order(user.ByName()).
	Limit(10).
	All(ctx)
```

</td>
</tr>
</table>

### What the compiler catches

| Mistake | ant | GORM | Bun | ent | sqlc |
| --- | :---: | :---: | :---: | :---: | :---: |
| Misspelled column, `agee` | ✅ | ❌ | ❌ | ✅ | ⚠️ |
| `"18"` for an `int` | ✅ | ❌ | ❌ | ✅ | ✅ |
| Misspelled relation, `Psots` | ✅ | ❌ | ❌ | ✅ | ⚠️ |
| Column of another model | ⚠️ | ❌ | ❌ | ✅ | ⚠️ |
| No code generation step | ✅ | ✅ | ✅ | – | – |

✅ compile error &nbsp; ⚠️ caught by a separate step (`sqlc generate`, or `antvet` for ant) &nbsp; ❌ compiles, fails at run time &nbsp; – needs a generate step

Each cell was tested by compiling the mistake. GORM also has an optional [CLI](https://github.com/go-gorm/cli) that generates typed field helpers.

## Performance

[tools/compare](tools/compare) runs the same operations in each library against PostgreSQL 17 on one machine (Ryzen 5 3600, Docker on WSL2). Each number is the median of 10 runs, and each run repeats the operation hundreds to thousands of times. The database round trip is included, which is why small queries are close. Commits don't wait for the disk, so writes measure the library and not the drive. Plain SQL (a hand-written query and `Scan`, what sqlc generates) is the baseline. Lower is better; 🟢 is the best in each row.

| µs per operation | ant | ant<br><sub>NoTracking</sub> | GORM | Bun | ent | | Plain SQL<br><sub>baseline</sub> |
| --- | ---: | ---: | ---: | ---: | ---: | - | ---: |
| Read 1000 rows | 2,799 | 🟢 **1,688** | 3,285 | 2,259 | 2,226 | | 1,557 |
| Find one by key | 406 | 389 | 416 | 🟢 **372** | 383 | | 365 |
| Filter, sort, 10 rows | 574 | 🟢 **552** | 561 | 584 | 635 | | 518 |
| 100 users, 500 posts | 3,039 | 🟢 **2,091** | 3,034 | 2,198 | 2,194 | | 1,750 |
| Insert one | 🟢 **431** | – | 442 | 1,164 | 441 | | 400 |
| Update one column | 🟢 **413** | – | 444 | 444 | 🟢 **412** | | 396 |
| Load, edit, save | 830 | – | 861 | 🟢 **779** | 1,784 | | 806 |

| KiB per operation | ant | ant<br><sub>NoTracking</sub> | GORM | Bun | ent | | Plain SQL<br><sub>baseline</sub> |
| --- | ---: | ---: | ---: | ---: | ---: | - | ---: |
| Read 1000 rows | 823 | 🟢 **277** | 392 | 295 | 589 | | 257 |
| Find one by key | 7.5 | 6.5 | 5.7 | 6.3 | 🟢 **4.9** | | 1.5 |
| Filter, sort, 10 rows | 15.1 | 10.5 | 9.7 | 🟢 **9.3** | 11.9 | | 3.8 |
| 100 users, 500 posts | 649 | 327 | 362 | 🟢 **277** | 335 | | 257 |
| Insert one | 4.3 | – | 7.0 | 6.8 | 🟢 **3.7** | | 1.3 |
| Update one column | 🟢 **2.9** | – | 5.6 | 5.0 | 3.1 | | 0.3 |
| Load, edit, save | 🟢 **10.2** | – | 11.2 | 11.2 | 10.7 | | 1.8 |

What the numbers say:

- Small queries cost about the same in every library.
- Change tracking costs on large reads: 1000 rows take 1.7 times as long and 3 times the memory, because ant keeps a copy of each row to find changes. With `NoTracking`, ant is the fastest here.
- Bun's insert is slow by default (it prepares every insert as a new statement), and ent's update reads the row back. [tools/compare](tools/compare#reading-them) has the details.

## Features

- **Typed queries:** conditions, joins, projections, aggregates, subqueries, CTEs and window functions, all checked by the compiler
- **Plain structs:** your types are the models, with conventions you can override in `Configure`
- **Relations:** 1:1, 1:N and many-to-many, loaded explicitly with `Include`, in batches. Nothing loads behind your back
- **Change tracking:** load, edit, `SaveChanges`. Only the columns that changed are written
- **Writes:** insert, update, delete, upserts, `MERGE` and bulk writes, with a guard against unfiltered updates and deletes
- **Transactions:** savepoints, isolation levels, retries and read replicas
- **Lifecycle:** soft deletes, timestamps, global filters, hooks, validation and optimistic concurrency
- **Migrations:** plain SQL generated from your structs, with drift checks. No down migrations, and no lock-in
- **Tooling:** a `go vet` analyzer, query logging, metrics, OpenTelemetry, and test fixtures and factories in `anttest`

## Databases

| | PostgreSQL | MySQL | MariaDB | SQLite | SQL Server |
| --- | :---: | :---: | :---: | :---: | :---: |
| Queries and writes | ✅ | ✅ | ✅ | ✅ | ✅ |
| Migrations | ✅ | | | | |

PostgreSQL gets every feature first. [docs/databases.md](docs/databases.md) lists what each database lacks.

## What's missing

- Migrations are PostgreSQL only. On MySQL, MariaDB, SQLite and SQL Server, ant reads and writes, and you manage the schema with your own tool or plain SQL.
- The full test suite runs on PostgreSQL only. The other databases run a smaller one, so expect more rough edges there.
- ant is pre-1.0, so the API can still change. Each release lists its breaking changes.

The [roadmap](docs/ROADMAP.md) lists what's planned, and why some items were skipped.

## Learn more

- [Documentation](https://skudra.net): guides, recipes and reference.
- [Feature examples](tests/features/): a short test for every feature, edge cases included
- [ant-examples](https://github.com/skudra-go/ant-examples): small projects built on ant
- [Roadmap](docs/ROADMAP.md) and [design decisions](docs/DECISIONS.md)

## Contributing

Issues and pull requests are welcome. Read [CONTRIBUTING.md](CONTRIBUTING.md) first. Report vulnerabilities as [SECURITY.md](SECURITY.md) describes.

Thank you to everyone who has filed an issue, fixed a bug or improved the docs:

<a href="https://github.com/skudra-go/ant/graphs/contributors">
  <img src="https://contrib.rocks/image?repo=skudra-go/ant" alt="Contributors">
</a>

## About

*Skudra* means *ant* in Latvian, which is where [skudra.net](https://skudra.net) gets its name :)

## License

[MIT](LICENSE)
