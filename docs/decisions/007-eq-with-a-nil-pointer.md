---
id: 7
title: "`Eq` with a nil pointer"
area: queries
status: amended
tags: [Eq, Ne, nil, IS-NULL, pointer]
issues: []
related: [95]
changed_by: [146]
code: [cond.go]
tests: [tests/features/querying/eq_test.go, tests/features/querying/is_null_test.go, tests/features/querying/ne_test.go]
---

# 7. `Eq` with a nil pointer

**In short:** Eq and Ne with a nil or NULL-valued argument render IS NULL / IS NOT NULL; a non-nil pointer compares its pointee.

> **Note.** Decision 95 extends the rule to In, NotIn and Expr methods.

> **Amended.** Decision 146 makes `Ne` with a value, and `Not` of `Eq`, match NULL on a column that can hold it, as `column <> v OR column IS NULL`.

## Decision

`ant.Eq(&Users.Bio, nil)` compiles with `V = *string`.

Decision: nil renders as `IS NULL`, as EF Core does for null comparisons. A non-nil pointer compares the pointed-to value, not the pointer.
