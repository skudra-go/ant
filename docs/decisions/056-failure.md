---
id: 56
title: "Failure"
area: migrations
status: active
tags: [transaction, ant:no-transaction, repair, RESET ALL, history, CONCURRENTLY]
issues: []
related: []
changed_by: []
code: [migrate/engine.go, migrate/migration.go, migrate/registry.go, dialect/postgres/session.go]
tests: [tests/features/migrations/up_test.go, migrate/internal_test.go, migrate/migrate_test.go]
---

# 56. Failure

**In short:** Each migration runs in a transaction with its history row; -- ant:no-transaction migrations record failure and block up until repair, and each migration ends with RESET ALL.

## Decision

A migration runs in a transaction with its history row, so a crash cannot leave it half done, or done but unrecorded. A failure leaves no trace, and fixing the file is enough.

`-- ant:no-transaction` is for statements PostgreSQL refuses in a transaction, such as `CREATE INDEX CONCURRENTLY`. A failure there can leave work behind, so it is recorded, and `up` refuses to continue until `repair`, as Flyway does.

Connections keep settings past a commit, and a `pg_dump` file clears `search_path`, so each migration ends with `RESET ALL`.
