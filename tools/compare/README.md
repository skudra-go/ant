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

[results.txt](results.txt), from 2026-11-10: Ryzen 5 3600, Go 1.27.3, PostgreSQL 17.11 in Docker on WSL2.

Each number is the median of 10 runs, and each run repeats the operation for a second and averages. The round trip to the database is in every number. Run-to-run spread was within ±10%, except Plain SQL on `UpdateOneColumn` (±21%) and `LoadEditSave` (±16%). Absolute numbers depend on the machine, so compare within a row.

Time per operation, in µs:

| | ant | ant<br><sub>NoTracking</sub> | GORM | Bun | ent | | Plain SQL<br><sub>baseline</sub> |
| --- | ---: | ---: | ---: | ---: | ---: | - | ---: |
| Read1000 | 2,799 | 🟢 **1,688** | 3,285 | 2,259 | 2,226 | | 1,557 |
| FindOne | 406 | 389 | 416 | 🟢 **372** | 383 | | 365 |
| FilteredQuery | 574 | 🟢 **552** | 561 | 584 | 635 | | 518 |
| Include100 | 3,039 | 🟢 **2,091** | 3,034 | 2,198 | 2,194 | | 1,750 |
| Insert | 🟢 **431** | – | 442 | 1,164 | 441 | | 400 |
| UpdateOneColumn | 🟢 **413** | – | 444 | 444 | 🟢 **412** | | 396 |
| LoadEditSave | 830 | – | 861 | 🟢 **779** | 1,784 | | 806 |

Memory per operation, in KiB:

| | ant | ant<br><sub>NoTracking</sub> | GORM | Bun | ent | | Plain SQL<br><sub>baseline</sub> |
| --- | ---: | ---: | ---: | ---: | ---: | - | ---: |
| Read1000 | 823 | 🟢 **277** | 392 | 295 | 589 | | 257 |
| FindOne | 7.5 | 6.5 | 5.7 | 6.3 | 🟢 **4.9** | | 1.5 |
| FilteredQuery | 15.1 | 10.5 | 9.7 | 🟢 **9.3** | 11.9 | | 3.8 |
| Include100 | 649 | 327 | 362 | 🟢 **277** | 335 | | 257 |
| Insert | 4.3 | – | 7.0 | 6.8 | 🟢 **3.7** | | 1.3 |
| UpdateOneColumn | 🟢 **2.9** | – | 5.6 | 5.0 | 3.1 | | 0.3 |
| LoadEditSave | 🟢 **10.2** | – | 11.2 | 11.2 | 10.7 | | 1.8 |

Allocations per operation:

| | ant | ant<br><sub>NoTracking</sub> | GORM | Bun | ent | | Plain SQL<br><sub>baseline</sub> |
| --- | ---: | ---: | ---: | ---: | ---: | - | ---: |
| Read1000 | 19,370 | 🟢 **6,831** | 14,810 | 8,790 | 15,840 | | 7,772 |
| FindOne | 118 | 94 | 89 | 🟢 **41** | 126 | | 36 |
| FilteredQuery | 306 | 176 | 219 | 🟢 **124** | 305 | | 99 |
| Include100 | 11,370 | 🟢 **4,528** | 9,558 | 6,218 | 8,152 | | 4,063 |
| Insert | 107 | – | 79 | 🟢 **54** | 86 | | 23 |
| UpdateOneColumn | 45 | – | 61 | 🟢 **14** | 64 | | 8 |
| LoadEditSave | 202 | – | 146 | 🟢 **52** | 269 | | 44 |

🟢 is the best library in each row. Plain SQL is the baseline and doesn't compete.

## Reading them

- Small queries cost about the same everywhere: 2% to 23% over Plain SQL, mostly the round trip.
- Change tracking costs on large reads. ant keeps a copy of each loaded row to find changes, so 1000 rows take 1.7 times as long and 3 times the memory as with `NoTracking`. With `NoTracking`, ant is the fastest on 1000 rows (8% over Plain SQL) and level with Bun and ent on `Include100`.
- Writes are close, with two exceptions caused by defaults. Bun puts values into the SQL text, so pgx prepares every insert as a new statement, an extra round trip; `default_query_exec_mode=simple_protocol` in the DSN brought it to 452 µs. ent's `UpdateOne` reads the row back in a transaction, so `LoadEditSave` sends four statements instead of two.
- Bun allocates the least in most rows. ant allocates more than the others on small queries.
