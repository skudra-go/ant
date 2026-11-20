---
id: 28
title: "Identity and relation slices"
area: tracking
status: amended
tags: [identity-map, relation-slice, SaveChanges, Find, Snapshot]
issues: []
related: [93, 112]
changed_by: [112]
code: [internal/track/track.go, unit.go, query.go, entry.go]
tests: [tests/features/change_tracking/identity_test.go, tests/features/change_tracking/relations_test.go]
---

# 28. Identity and relation slices

**In short:** A tracked query returns the existing instance for a row, but value elements of relation slices are second tracked instances saved independently.

> **Amended.** Decision 112 makes a locking read (ForUpdate, ForShare) refresh the already tracked instance instead of dropping the row.

## Decision

As in EF Core, a tracked query returns the existing instance for a row, and `Find` returns it without a query. The exception is `[]Post` relation fields, whose elements are values. A row in one of those is a second tracked instance with its own snapshot, and changes to either are saved. Field-level updates mean the two do not overwrite each other unless both change the same column. When both are written in one save, the second writes over the version the first gave the row, and a row edited through one instance and dropped through the other is deleted, rather than either being refused as a concurrent change (decision 93).
