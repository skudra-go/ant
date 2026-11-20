---
id: 6
title: "Return shapes and tracking"
area: tracking
status: superseded
tags: [Find, First, Each, All, tracking, return-shape]
issues: []
related: [27, 28]
changed_by: [27]
code: [query.go, table.go]
tests: [tests/features/querying/each_test.go, tests/features/change_tracking/identity_test.go]
---

# 6. Return shapes and tracking

**In short:** Find and First return *T and the tracker holds pointers; Each yields values and is untracked.

> **Superseded.** Decision 27 changed All to []*T; query.go:647 All returns []*T. Find, First and untracked Each still hold.

## Decision

Change tracking requires a stable pointer to the loaded entity, and the target API mutates `user.Age++` after `Find`.

Decision: `Find` and `First` return `*T`. `All` returns `[]T`, and the tracker holds pointers into that slice's backing array. `Each` yields `T` by value and is not tracked. EF Core tracks streamed entities, but a value copy in Go cannot be tracked.
