---
id: 16
title: "Test schema is hand-written"
area: testing
status: active
tags: [testdb, schema, fixture, hand-written-ddl]
issues: []
related: []
changed_by: []
code: [internal/testdb/schema.go, internal/testdb/testdb.go]
tests: [tests/conformance_test.go]
---

# 16. Test schema is hand-written

**In short:** The test schema for every database is written by hand in internal/testdb, not generated from migrations.

> **Note.** Migrations still exist only for PostgreSQL (docs/databases.md), so the hand-written schema remains.

## Decision

`internal/testdb` creates the fixture tables with per-database SQL. Migrations exist now, but only on PostgreSQL, and the harness covers all five databases, so it stays hand-written until v0.4.
