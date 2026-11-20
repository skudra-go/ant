---
id: 74
title: "Dialects state their differences"
area: dialects
status: active
tags: [Syntax, ErrUnsupported, RegisterErrors, NoFilter, NoILike, FULL-JOIN, emulation, UTCTimes]
issues: []
related: [49, 75, 76]
changed_by: []
code: [dialect/syntax.go, dialect/render.go, dialect/sqlite/sqlite.go, dialect/mysql/mysql.go, dialect/sqlserver/sqlserver.go]
tests: [dialect/syntax_test.go, dialect/render_test.go, dialect/dialect_test.go, tests/matrix/matrix_test.go]
---

# 74. Dialects state their differences

**In short:** A dialect's Syntax lists differences from PostgreSQL as data, missing features return ErrUnsupported before sending, safe ones are emulated, and RegisterErrors maps driver codes to SQLSTATE.

## Decision

A dialect's `Syntax` lists how its SQL differs from PostgreSQL's, and the zero value changes nothing, so PostgreSQL's output cannot move when another dialect is added. Differences are data, in one place, rather than a renderer per database.

A feature a database lacks is reported as `ErrUnsupported` before sending anything. Where the meaning can be kept exactly, ant emulates it instead: `FILTER` becomes a `CASE` inside the aggregate, and `ILIKE` a `LIKE` between lowered values. Where it cannot, ant refuses. It does not emulate `FULL JOIN` on MySQL, or upserts on SQL Server, because those would quietly change concurrency behaviour.

Error codes are translated to SQLSTATE values through `dialect.RegisterErrors`, so `IsUniqueViolation` takes no dialect.
