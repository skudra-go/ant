---
id: 80
title: "Every write moves the version on"
area: writes
status: amended
tags: [version, optimistic-concurrency, Upsert, Merge, DoUpdate, ConcurrencyError, soft-delete, Restore, UpdatedAt]
issues: [87, 88]
related: [89, 91, 98, 102]
changed_by: [89, 91, 98]
code: [upsert.go, merge.go, query.go, table.go]
tests: [tests/features/writing, tests/features/change_tracking/relations_test.go]
---

# 80. Every write moves the version on

**In short:** Upsert, Merge, InsertMany with conflict, soft delete, Restore and relation-member updates move the stored version on by one and refuse a version named in assignments.

> **Amended.** Decision 91 changed the MySQL/MariaDB part: a stale copy's upsert no longer goes ahead there but is guarded by CASE WHEN version = ? and returns ErrConcurrency (upsert.go versionCheck). Decisions 89 and 98 extend the refusal of the version to bulk Update and UpdateMany.

## Decision

An upsert that updated copied every column from the row it tried to insert, the version included, and `Insert` starts a version at 1. So the stored version went back to 1, and a copy loaded at version 1, two saves earlier, matched again and saved over both. `DoUpdate` and `Set` left the version where it was, which hid the upsert from every copy loaded before it. `Merge` did the same, and `InsertMany` with a conflict shares the upsert's clause.

Now each of them moves the stored version on by one when it updates a row, as `SaveChanges` and bulk `Update` do, and a row `Merge` inserts starts at 1 unless the source gives a version. The version is ant's to keep, so naming it in `DoUpdate`, `Set`, or a `Merge` assignment is an error before anything runs.

An entity passed to `Upsert` with a version of its own is a loaded copy, so its update applies only while the stored row still has that version, and otherwise `Upsert` returns a `ConcurrencyError`, as `SaveChanges` would. A zero version is a new entity, with nothing to check. `ON DUPLICATE KEY UPDATE` takes no condition, so on MySQL and MariaDB the version moves on but the copy's update goes ahead. `InsertMany` cannot tell a row's own version from the 1 it stamps on a new one, so it moves versions on without checking them.

A soft delete and `Restore` are updates too. They left the version and `UpdatedAt` as they were, so a copy loaded before a soft delete saved into the deleted row with no conflict, where a real delete gives `ErrConcurrency` (#87). Both now move the version on and stamp `UpdatedAt`, tracked (`Remove`) or in bulk (`Delete`, `DeleteByID`, `Restore`). Adding `deleted_at IS NULL` to tracked updates was rejected: it would close the soft delete but not `Restore`, and a copy loaded with `WithDeleted` may mean to edit a deleted row.

An untracked row the application puts into a loaded relation, loaded with `NoTracking` or decoded from JSON, was moved with an `UPDATE` of its foreign key alone, checking and moving no version (#88). Its update now checks the version the copy carries and moves it on, as `Upsert` does with a copy, and stamps `UpdatedAt`. A copy with a zero version, built from a key alone, has nothing to check, so the database moves the stored version on (`version = version + 1`) and the copy is left as it was.
