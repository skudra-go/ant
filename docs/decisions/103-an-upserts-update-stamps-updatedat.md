---
id: 103
title: "An upsert's update stamps UpdatedAt"
area: writes
status: active
tags: [Upsert, InsertMany, DoUpdate, UpdatedAt, on-conflict, excluded]
issues: [84]
related: [89]
changed_by: []
code: [upsert.go]
tests: [tests/conformance_test.go, tests/features/writing/upsert_test.go, tests/features/lifecycle/upsert_version_test.go]
---

# 103. An upsert's update stamps UpdatedAt

**In short:** An upsert's conflict update, on Upsert and InsertMany with a conflict, sets UpdatedAt to ant's clock unless DoUpdate or Set assigns it.

## Decision

`Update`, `SaveChanges` and `Merge` set `UpdatedAt` on every update, but an upsert's update branch did not: `DoUpdate` left the column as it was, and the default clause wrote `excluded.updated_at`, the struct's value, which for a loaded copy is the time it was last saved (#84). The conflict update now sets `UpdatedAt` to ant's clock, on `Upsert` and on `InsertMany` with a conflict, unless `DoUpdate` or `Set` assigns it, in which case the clause's value wins, as with a bulk `Update` (decision 89). The default clause no longer copies the column from the inserted row, since that value is the one being replaced.
