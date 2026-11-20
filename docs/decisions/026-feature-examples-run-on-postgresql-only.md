---
id: 26
title: "Feature examples run on PostgreSQL only"
area: testing
status: active
tags: [features, postgres, fixture, schema-per-test, parallel]
issues: []
related: [16]
changed_by: []
code: [tests/features/fixture/fixture.go, internal/testdb/testdb.go]
tests: [tests/features/README.md]
---

# 26. Feature examples run on PostgreSQL only

**In short:** Feature examples run on PostgreSQL only, each test in its own schema.

> **Note.** Still PostgreSQL-only: fixture.Each uses testdb.ForOnly postgres; per-test schema in testdb.go isolate.

## Decision

`tests/features` holds one small example per feature. It uses PostgreSQL SQL for its tables, and skips other databases until v0.4. Each PostgreSQL test gets its own schema, so test packages can run in parallel against one server.
