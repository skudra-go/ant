---
id: 59
title: "Adopting an existing database"
area: migrations
status: active
tags: [adopt, baseline, drift, pg_dump, reverse-engineering, scaffold]
issues: []
related: [57]
changed_by: []
code: [migrate/cli.go, migrate/generate.go, migrate/operate.go]
tests: [tests/features/migrations/adopt_test.go]
---

# 59. Adopting an existing database

**In short:** ant does not generate structs from a live schema; instead drift works without a snapshot, adopt makes a dump the first migration, and baseline records it on existing databases.

## Decision

Generating Go structs from a live schema is not offered. The mapping is ambiguous, since `text` could be `string`, `*string`, a named type, or an enum, and relations cannot be read from foreign keys alone. A person or an assistant does this better, given a dump and a way to check the work.

So ant provides the check: `drift` works with no snapshot, and says exactly what still differs, so the loop ends when it reports nothing. `adopt` then makes a dump the first migration, removing psql commands that recent `pg_dump` versions write, and `baseline` records it on existing databases. The guide has a prompt to start from. The `migrate scaffold` item in v0.9 needs revisiting in light of this.
