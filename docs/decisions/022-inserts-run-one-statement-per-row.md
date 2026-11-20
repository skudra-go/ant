---
id: 22
title: "Inserts run one statement per row"
area: writes
status: amended
tags: [Insert, transaction, generated-key, Batch, InsertMany]
issues: []
related: [63]
changed_by: [63]
code: [table.go, unit.go, upsert.go]
tests: [tests/features/writing/insert_test.go, tests/features/writing/bulk_test.go]
---

# 22. Inserts run one statement per row

**In short:** Inserts in one call share a transaction so generated keys match entities and a failure clears them; batching is a later change.

> **Amended.** Decision 63 batches new rows up to 200 per statement; the ErrNotImplemented guard for related entities is gone (Insert links or inserts related entities, table.go:95).

## Decision

Several entities are written one at a time in a transaction, which keeps generated keys matched to entities and makes the call all or nothing. Generated keys are cleared again if the call fails. Faster batched inserts are planned in v0.5.

Until v0.3, `Insert` returns `ErrNotImplemented` if an entity has related entities set, rather than dropping them. `Include`, `IncludeOne`, and `Join` do the same.
