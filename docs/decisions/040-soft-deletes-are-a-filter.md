---
id: 40
title: "Soft deletes are a filter"
area: hooks
status: active
tags: [soft-delete, DeletedAt, ForceDelete, Remove, DeleteByID]
issues: []
related: []
changed_by: []
code: [internal/meta/meta.go, query.go, table.go, ant.go]
tests: [tests/features/lifecycle/soft_delete_test.go]
---

# 40. Soft deletes are a filter

**In short:** An ant.DeletedAt field turns Delete, DeleteByID and Remove into soft deletes with a built-in filter; ForceDelete removes rows.

## Decision

A field of type `ant.DeletedAt` makes `Delete`, `DeleteByID`, and `Remove` mark rows, and adds a built-in global filter. `ForceDelete` removes rows for real.
