---
id: 30
title: "Tracking follows keys, not addresses"
area: tracking
status: active
tags: [SaveChanges, relation-reconcile, unlink, orphan, foreign-key, null]
issues: []
related: [93, 111]
changed_by: []
code: [unit.go, internal/track/track.go, entry.go]
tests: [tests/features/change_tracking/relations_test.go, tests/features/change_tracking/graph_test.go, tests/tracking_test.go]
---

# 30. Tracking follows keys, not addresses

**In short:** SaveChanges reconciles relations by key, deleting or unlinking removed children from copies, and unlinks only when the foreign key can be NULL.

## Decision

Appending to a relation slice can move its elements, and `slices.Delete` shifts them in place, so an address says nothing about which row it holds. Every relation is reconciled by key before anything else in `SaveChanges`. A removed row is deleted or unlinked using a copy, of what its element holds if that is still the row, so an edit made before it left the slice is saved with the unlink, and of its snapshot otherwise, and its key comes from the entry, never from memory. An unlink moves the version and `UpdatedAt` on, as any update does. A child is unlinked only when the zero value of its foreign key is written as NULL: a pointer, or a `driver.Valuer` whose zero value is NULL, as `sql.Null` is, on a column not configured `NotNull`. Any other key is required and its child is deleted, which includes a `uuid.UUID`, whose zero value is a UUID like any other. A child that was detached, or removed, is not orphaned again.
