---
id: 102
title: "UpdateMany keeps what a bulk Update keeps"
area: writes
status: active
tags: [UpdateMany, version, UpdatedAt, global-filter, soft-delete, tenant-filter]
issues: [86]
related: [80, 89, 98]
changed_by: []
code: [upsert.go, query.go]
tests: [tests/features/writing/bulk_changes_test.go, tests/features/writing/update_test.go, tests/conformance_test.go]
---

# 102. UpdateMany keeps what a bulk Update keeps

**In short:** UpdateMany moves the version on, stamps UpdatedAt unless named, and applies the model's global filters, leaving passed entities untouched.

## Decision

`UpdateMany` wrote only the named columns with a key condition, so it left the version where it was, never set `UpdatedAt`, and wrote soft-deleted rows; a copy loaded before it then saved over its value with no conflict (#86). Each statement now moves the version on (decision 80), sets `UpdatedAt` unless a field names it (decision 89), and carries the model's global filters, as a bulk `Update` does, so a soft-deleted row or one a tenant filter hides is not counted or changed. The entities passed in are left as they are, since ant does not know the stored version to move them to; a caller that keeps one should load it again. Applying only the soft-delete filter was rejected: a tenant filter guards a write at least as much as a read.
