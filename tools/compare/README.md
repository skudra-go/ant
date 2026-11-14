# compare

Benchmarks of ant, GORM 1.31, Bun 1.3, ent 0.14 and plain `database/sql` doing the same work on PostgreSQL. Plain `database/sql` stands in for sqlc, which generates the same kind of code. It's a separate module so ant doesn't depend on the others.

`TestSameQuery` runs the [README](../../README.md#next-to-gorm-bun-and-ent) query in all four libraries and checks they return the same rows.

## Run it

```sh
make db-up DATABASES=postgres   # from the repo root
cd tools/compare
go test -run '^$' -bench . -benchmem -count 10 | tee new.txt
go run golang.org/x/perf/cmd/benchstat@latest new.txt
```

`ANT_PG_DSN` picks another database. After changing `ent/schema`, run `go generate ./ent`.

## What each benchmark does

The tables hold 1000 users with 5 posts each.

| Benchmark | Work |
| --- | --- |
| `Read1000` | Every user, 1000 rows |
| `FindOne` | One user by key |
| `FilteredQuery` | `age > 30 AND name LIKE 'user 1%'`, by name, 10 rows |
| `Include100` | 100 users and their 500 posts, in two queries |
| `Insert` | One user |
| `UpdateOneColumn` | `UPDATE ... SET age` by key, without loading the row |
| `LoadEditSave` | Load a user, change the age, save it: one request's work |

- All libraries share one connection pool.
- `ant` has change tracking on, its default, with a new `ant.Session` per operation, like a request handler. `ant-notrack` uses `NoTracking`.
- Commits don't wait for the disk (`synchronous_commit=off`). On this machine that wait was up to 9 ms and varied by ±90%, which hid the libraries completely.

## Results

[results.txt](results.txt), from 2026-11-14: Ryzen 5 3600, Go 1.27.3, PostgreSQL 17.11 in Docker on WSL2, with ant's allocation work ([#138](https://github.com/skudra-go/ant/issues/138)). The earlier results, from 2026-11-10, are in this file's history.

Each number is the median of 10 runs, and each run repeats the operation for a second and averages. The round trip to the database is in every number. Run-to-run spread was within ±10%, except ant on `FindOne` (±11%) and Plain SQL on `LoadEditSave` (±13%). Absolute numbers depend on the machine, so compare within a row.

Time per operation, in µs:

| | ant | ant<br><sub>NoTracking</sub> | GORM | Bun | ent | | Plain SQL<br><sub>baseline</sub> |
| --- | ---: | ---: | ---: | ---: | ---: | - | ---: |
| Read1000 | 2,075 | 🟢 **1,692** | 3,361 | 2,370 | 2,240 | | 1,692 |
| FindOne | 442 | 397 | 405 | 🟢 **384** | 398 | | 398 |
| FilteredQuery | 557 | 🟢 **539** | 568 | 🟢 **535** | 556 | | 535 |
| Include100 | 2,238 | 🟢 **1,774** | 2,731 | 2,030 | 2,031 | | 1,581 |
| Insert | 410 | – | 420 | 1,112 | 🟢 **404** | | 392 |
| UpdateOneColumn | 🟢 **399** | – | 407 | 424 | 🟢 **402** | | 390 |
| LoadEditSave | 🟢 **805** | – | 818 | 🟢 **812** | 1,869 | | 798 |

Memory per operation, in KiB:

| | ant | ant<br><sub>NoTracking</sub> | GORM | Bun | ent | | Plain SQL<br><sub>baseline</sub> |
| --- | ---: | ---: | ---: | ---: | ---: | - | ---: |
| Read1000 | 582 | 🟢 **275** | 392 | 295 | 589 | | 257 |
| FindOne | 3.8 | 🟢 **3.5** | 5.7 | 6.3 | 4.9 | | 1.5 |
| FilteredQuery | 11.5 | 🟢 **7.9** | 9.7 | 9.2 | 11.9 | | 3.8 |
| Include100 | 486 | 🟢 **241** | 362 | 277 | 335 | | 257 |
| Insert | 🟢 **2.5** | – | 7.0 | 6.8 | 3.7 | | 1.3 |
| UpdateOneColumn | 🟢 **2.5** | – | 5.6 | 5.0 | 3.1 | | 0.3 |
| LoadEditSave | 🟢 **5.8** | – | 11.2 | 11.2 | 10.7 | | 1.8 |

Allocations per operation:

| | ant | ant<br><sub>NoTracking</sub> | GORM | Bun | ent | | Plain SQL<br><sub>baseline</sub> |
| --- | ---: | ---: | ---: | ---: | ---: | - | ---: |
| Read1000 | 6,850 | 🟢 **6,794** | 14,810 | 8,790 | 15,843 | | 7,772 |
| FindOne | 67 | 52 | 89 | 🟢 **41** | 126 | | 36 |
| FilteredQuery | 146 | 🟢 **115** | 219 | 124 | 305 | | 99 |
| Include100 | 5,011 | 🟢 **3,144** | 9,558 | 6,218 | 8,152 | | 4,063 |
| Insert | 🟢 **47** | – | 79 | 54 | 86 | | 23 |
| UpdateOneColumn | 27 | – | 61 | 🟢 **14** | 64 | | 8 |
| LoadEditSave | 116 | – | 146 | 🟢 **52** | 269 | | 44 |

🟢 is the best library in each row, shared by any within 1% of it. Plain SQL is the baseline and doesn't compete.

## Reading them

- Small queries cost about the same everywhere: within 11% of Plain SQL, mostly the round trip, apart from the two defaults below.
- Change tracking costs on large reads. ant keeps a copy of each loaded row to find changes, so 1000 rows take 1.2 times as long and twice the memory as with `NoTracking`, with about the same number of allocations. With `NoTracking`, ant is level with Plain SQL on 1000 rows and the fastest library on `Include100`.
- Writes are close, with two exceptions caused by defaults. Bun puts values into the SQL text, so pgx prepares every insert as a new statement, an extra round trip; `default_query_exec_mode=simple_protocol` in the DSN brought it to 452 µs (measured on 2026-11-10). ent's `UpdateOne` reads the row back in a transaction, so `LoadEditSave` sends four statements instead of two.
- ant uses the least memory in every row. Bun allocates the least on single-row reads and updates; ant allocates the least on large reads, filtered queries and inserts.
