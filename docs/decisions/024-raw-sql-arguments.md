---
id: 24
title: "Raw SQL arguments"
area: queries
status: active
tags: [Raw, Bind, placeholder, slice-expansion, In, named-args]
issues: []
related: []
changed_by: []
code: [dialect/bind.go, raw.go]
tests: [dialect/bind_test.go, tests/features/querying/raw_arguments_test.go, tests/features/querying/raw_test.go]
---

# 24. Raw SQL arguments

**In short:** Raw SQL binds ? only with no $n and one ? per argument, expands slices only inside IN lists, and turns an empty slice into NULL.

## Decision

The rules are in the `dialect.Bind` documentation. PostgreSQL has a `?` jsonb operator, so `?` is only a placeholder when the query has no `$n` and exactly one `?` per argument. A slice expands only when it fills an `IN (...)` list, so `= ANY(@ids)` keeps passing an array. An empty slice becomes `NULL`, so `NOT IN (?)` with an empty slice matches nothing, unlike `ant.NotIn`.
