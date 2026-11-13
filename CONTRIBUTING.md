# Contributing

Thanks for helping with ant. Bug reports, fixes and docs are all welcome. For a new feature, open an issue first so we can agree on the shape before you write it.

## Branches

ant uses GitHub Flow. `main` is always releasable, and every change reaches it through a pull request.

1. Branch off the latest `main`:

   ```sh
   git switch main
   git pull origin main
   git switch -c feat/short-name   # or fix/short-name
   ```

2. Push the branch and open a pull request into `main`.
3. CI must pass (`lint` and `test`), and one maintainer must approve.
4. The pull request is squash-merged, so its title becomes the commit on `main`.

## Linear history

`main` only accepts a linear history, so a branch that has fallen behind must be rebased, never merged:

```sh
git pull --rebase origin main
# fix any conflicts, then git add <files> and git rebase --continue
git push --force-with-lease
```

Don't merge `main` into your branch. Rebasing keeps the branch linear, so each commit can be reviewed on its own.

## Commit messages

Use [Conventional Commits](https://www.conventionalcommits.org/). The pull request title matters most, because it becomes the squashed commit:

```
feat: add ChunkByKey for composite keys
fix(mysql): quote reserved column names in upserts
docs: explain Include ordering
```

Types: `feat`, `fix`, `docs`, `test`, `refactor`, `perf`, `ci`, `chore`. Add `!` after the type for a breaking change, as in `feat!: rename Session options`.

## Running it locally

You need Go 1.27 or later, Docker, `make` and [staticcheck](https://staticcheck.dev/). CI runs the same `make` targets.

```sh
make build                     # build every module
make fmt                       # lay out the code; lint fails on anything this would change
make lint                      # go vet, antvet, staticcheck and golangci-lint
make test                      # unit tests of every module; database tests skip
make test-modules              # just the nested modules (tools/*, contrib/otel)
make tidy                      # go mod tidy in every module
make deps                      # govulncheck, and a list of outdated dependencies

make db-up DATABASES=postgres  # start PostgreSQL in Docker
make test-pg                   # API tests and feature examples on PostgreSQL
make db-down                   # stop the databases and discard their data
```

`make test` skips every database test unless `ANT_TEST_DATABASES` is set, so a green run without a database proves little. For anything that reaches SQL, run `make test-pg`. If you touch another dialect, also run `test-mysql`, `test-mariadb`, `test-sqlite` or `test-mssql` (start them with `make db-up`, or leave `DATABASES` out to start all five). CI also runs the tests against PostgreSQL under the race detector, which needs cgo, except the value probes under `tests/features/value_handling/probes`, which start no goroutines and run on a runner of their own.

Database tests run in parallel, eight at a time per package (`PARALLEL=8`). Each test gets a database of its own: a temporary file on SQLite, a schema on PostgreSQL, and a database on MySQL, MariaDB and SQL Server, so their users need the right to create databases (the defaults connect as `root` and `sa`). A test that changes something the whole process shares, such as an environment variable, `slog`'s default logger, `secure.SetKeys`, `WithNaming` or `WithConvention`, calls `testdb.Serial(t)` (`fixture.Serial(t)` in `tests/features`) before it reaches the database. Go runs those first, one at a time, then the parallel ones.

## What a change needs

- A test that fails without it. New features also get a short example under [`tests/features/`](tests/features/).
- If the exported API changes on purpose, rewrite the record with `go test ./tests/api -update` and say so in the pull request.
- Design decisions go in [`docs/DECISIONS.md`](docs/DECISIONS.md).
- A change to SQL, scanning, types, quoting, paging or transactions has to hold on all five databases. [`docs/databases.md`](docs/databases.md) lists what each one supports.
- User docs live in [ant-docs](https://github.com/skudra-go/ant-docs). Update them there when behaviour changes.
- The skill for coding agents, [`skills/ant/SKILL.md`](skills/ant/SKILL.md), states the API's rules in brief. Update it when a change makes it wrong. `tests/api` fails if it names something the API no longer has. To type-check its samples, run ant-docs' doccheck on it, from `ant-docs/tools/doccheck` with a `go.work` that points at your ant checkout: `go run ./cmd/doccheck ../../../ant/skills`.

## Conduct and security

Everyone taking part follows the [code of conduct](CODE_OF_CONDUCT.md). Report vulnerabilities privately, as [SECURITY.md](SECURITY.md) describes, not in an issue.
