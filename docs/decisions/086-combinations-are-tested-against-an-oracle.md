---
id: 86
title: "Combinations are tested against an oracle"
area: testing
status: active
tags: [matrix, oracle, three-valued-logic, conformance, sql-snapshots, known-failures]
issues: []
related: []
changed_by: []
code: [tests/matrix]
tests: [tests/matrix/matrix_test.go, tests/matrix/sql_test.go]
---

# 86. Combinations are tested against an oracle

**In short:** tests/matrix checks generated query combinations against a Go oracle on seeded rows on every database, with a known-failure list that must stay accurate.

## Decision

Most bugs found lately came from one feature meeting another: a bulk write on a derived source, a paged member of a union, a nullable time on a database without zones, a relation load inside a transaction that rolls back. Hand-written tests check features one at a time, so the combinations went unchecked.

`tests/matrix` builds queries from a grid of dimensions and checks every point against an oracle: the same combination worked out in Go, on the same seeded rows, with SQL's three-valued logic. It runs on every database, about 40,000 checks each, in a minute on PostgreSQL. Checking rows, not SQL, is what finds wrong results; SQL snapshots per dialect add a review of every change to generated SQL, with no database needed. A known failure is listed with its issue and skips, and an entry whose combinations all pass fails the run, so the list tracks what is still broken. The grid grows by adding a point to a dimension, with its meaning in Go beside it.
