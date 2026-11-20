---
id: 98
title: "An explicit write refuses the columns ant keeps"
area: writes
status: active
tags: [immutable, readonly, computed, Update, Set, DoUpdate, UpdateMany, WhenMatched]
issues: [62]
related: [80, 89]
changed_by: []
code: [upsert.go, query.go, internal/meta/meta.go]
tests: [tests/features/model_definition/field_roles_test.go, tests/features/writing/bulk_changes_test.go, tests/conformance_test.go]
---

# 98. An explicit write refuses the columns ant keeps

**In short:** Bulk Update, upsert DoUpdate/Set, Merge WhenMatched and UpdateMany refuse immutable, readonly and computed fields by name, and UpdateMany also refuses the version.

## Decision

`SaveChanges` never updates an `immutable`, `readonly`, or `computed` field, but a bulk `Update` with `ant.Set`, an upsert's `DoUpdate` or `Set`, a merge's `WhenMatched`, and `UpdateMany`'s field list wrote whatever they named, so an immutable column changed after all (#62). Each of them now refuses such a field by name before anything runs, as it refuses the version (decisions 80 and 89), and `UpdateMany` now refuses the version too. Inserts are unchanged, since an immutable field is written on insert. Letting an explicit write win, as an explicit `UpdatedAt` does, was rejected: the tag is the model's promise, and a field that must change sometimes should not carry it.
