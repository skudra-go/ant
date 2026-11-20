---
id: 133
title: "A concurrent index build clears its own leftover"
area: migrations
status: active
tags: [Concurrently, CREATE INDEX CONCURRENTLY, invalid index, drift, Inspect]
issues: [97]
related: [57]
changed_by: []
code: [dialect/postgres/ddl.go, dialect/postgres/inspect.go, schema/schema.go, schema/change.go]
tests: [tests/features/migrations/up_test.go, schema/diff_test.go]
---
# 133. A concurrent index build clears its own leftover

**In short:** A generated CREATE INDEX CONCURRENTLY is preceded by DROP INDEX CONCURRENTLY IF EXISTS, and Inspect reads invalid indexes, which drift drops and builds again.

## Decision

A `CREATE INDEX CONCURRENTLY` that fails leaves an invalid index of its name. Inspect hid invalid indexes, so drift asked to create the index the database had, and running the file again failed with "already exists" (#97). A generated concurrent build now drops a leftover of its name first, so a fixed file runs again; the index was not there before the file, by the snapshot, so nothing valid is lost. Inspect reads invalid indexes as `Index.Invalid`, which differs from any declared one, so drift names it, drops it, and builds it again.
