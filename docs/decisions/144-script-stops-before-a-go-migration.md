---
id: 144
title: "script stops before a Go migration, and starts from a version"
area: migrations
status: active
tags: [script, Go-migrations, no-transaction, from, guarded, OutOfOrder]
issues: [183]
related: [11, 115, 127]
changed_by: []
code: [migrate/operate.go, migrate/cli.go]
tests: [tests/features/migrations/script_test.go]
---

# 144. script stops before a Go migration, and starts from a version

**In short:** a guarded `script` writes the migrations up to the first Go migration and stops there, saying how to go on, and `script --from <version>` leaves out the versioned migrations before that version, as applied.

## Decision

`script` left Go migrations out and went on, saying "run it with up". A DBA who ran the file applied the later SQL migrations too, so the Go migration was then older than an applied one, and `up` refused it unless `Config.OutOfOrder` was set. And a file marked `-- ant:no-transaction` refused the whole script, even one applied long ago, with no way to write only what was left (#183).

A guarded script now stops before the first Go migration among those it writes, and leaves out the repeatable files too, as they run after every versioned migration. The end of the file and a warning say what to do: run it with `up --to <version>`, which applies no code file, then `script --from <next version>` for the rest. `--from` leaves out every versioned migration before the version, which must be one a migration has, as `up --to` asks (#185); a version matches without its leading zeros. A file that cannot run in a transaction still refuses a guarded script while it is among the migrations to write, since its guard would be a block in one transaction, and the error says that `--from` the version after it leaves it out once it has run.

`--plain` takes `--from` too, and still leaves Go migrations out with a warning: it writes no history, so no `up` later finds a migration older than an applied one.

Rejected:

- Stopping before a no-transaction file as before a Go one, with a warning: the refusal is already explicit, and a script that ends early with only a warning is easier to ship by mistake.
- A pending-only mode that reads the history: `script` is for databases ant cannot reach, so it cannot know what is applied, and the guards already skip what is.
