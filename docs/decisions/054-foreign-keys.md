---
id: 54
title: "Foreign keys"
area: migrations
status: active
tags: [foreign-key, OnDelete, NoAction, NoIndex, index, relations]
issues: []
related: [8, 66]
changed_by: []
code: [config.go, schema.go, schema/schema.go]
tests: [tests/features/migrations/relations_test.go, schema/diff_test.go]
---

# 54. Foreign keys

**In short:** OnDelete defaults to NO ACTION, both relation sides configure one key (contradiction errors), and foreign keys get an index unless one already leads with the column, opt out with NoIndex.

## Decision

`OnDelete` defaults to the database's `NO ACTION`. EF Core cascades required relations by default, which silently deletes rows, and decision 8 prefers saying things out loud. The two sides of a relation are one key, either can configure it, and a contradiction is an error.

Foreign keys get an index unless one already leads with the column, as in EF Core. PostgreSQL does not make one, and a delete on the parent then scans the child. `.NoIndex()` opts out.
