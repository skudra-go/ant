---
id: 71
title: "Dependencies stay out of the root package"
area: tooling
status: active
tags: [dependencies, pgxext, ErrInTx, contrib/otel, antvet, Copy, Batch]
issues: [104]
related: [46]
changed_by: []
code: [dialect/postgres/pgxext, contrib/otel, tools/antvet, tools/compare, dialect/mysql/mysql.go]
tests: [tests/features/writing/merge_test.go, tests/api/api_test.go]
---

# 71. Dependencies stay out of the root package

**In short:** The root package uses only the standard library; pgx, OpenTelemetry, the analyzer and benchmarks are in separate packages or modules, and pgxext refuses transaction-bound contexts with ErrInTx.

## Decision

The root package uses the standard library. Code that needs more goes elsewhere: `pgxext` for pgx, and separate modules for OpenTelemetry (`contrib/otel`), the analyzer (`tools/antvet`), and the benchmarks (`tools/compare`). `Batch` left the root package for this reason, as the roadmap's `ant.Batch` needed pgx. `pgxext` reaches pgx through a pool connection of its own, as `database/sql` gives no access to a transaction's connection, so `Copy` and `Batch.Send` refuse a context bound to a transaction with `ErrInTx`: they wrote on another connection, committed on their own, and survived the caller's rollback, and `anttest.Isolate`'s with them (#104). `dialect/mysql` imports its driver, as the error numbers are not reachable any other way, and only a program using MySQL compiles it.
