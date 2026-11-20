---
id: 3
title: "`Find` takes a generic key"
area: queries
status: active
tags: [Find, generic-key, comparable, checkKey]
issues: []
related: []
changed_by: []
code: [table.go, query.go]
tests: [tests/features/querying/find_test.go]
---

# 3. `Find` takes a generic key

**In short:** Find takes a comparable generic key and returns an error at runtime when the key type does not match the primary key.

> **Note.** Find also exists on Query[T] (query.go) beside Table[T].

## Decision

An `any` key would break the rule against `any` in the query API, and `Table[T]` has no type parameter for the key.

Decision: `func (t *Table[T]) Find[K comparable](ctx context.Context, key K) (*T, error)`. A key of the wrong type is returned as an error at runtime. This is a known limit.
