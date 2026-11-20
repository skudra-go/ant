---
id: 23
title: "Guarding bulk writes"
area: writes
status: active
tags: [ErrNoCondition, Unfiltered, Update, Delete, bulk-write, guard]
issues: []
related: [38]
changed_by: []
code: [query.go, cond.go]
tests: [tests/features/writing/no_condition_guard_test.go, tests/features/writing/bulk_changes_test.go]
---

# 23. Guarding bulk writes

**In short:** Update and Delete return ErrNoCondition unless a condition can exclude rows or Unfiltered is called; empty And or NotIn do not count.

## Decision

`Update` and `Delete` return `ErrNoCondition` unless a condition can exclude rows or `Unfiltered` was called. Conditions that cannot exclude a row do not count, such as `ant.And()` or `ant.NotIn` with an empty list, because they usually come from an empty slice. The roadmap's original `q.All()` name was replaced, since `All` is already a terminal call.
