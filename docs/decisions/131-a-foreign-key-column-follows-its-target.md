---
id: 131
title: "A new foreign key column follows its target"
area: migrations
status: active
tags: [foreign-keys, uuid, Type, backfill, NOT NULL, SET NOT NULL]
issues: [186, 187]
related: [52, 54]
changed_by: []
code: [schema.go, schema/script.go, schema/change.go, dialect/postgres/ddl.go]
tests: [tests/features/migrations/relations_test.go]
---
# 131. A new foreign key column follows its target

**In short:** A foreign key column with no type of its own takes the type its target was given; a new NOT NULL one is added NULL and made NOT NULL after, for its rows to be filled.

## Decision

A string foreign key to a key typed `uuid` was `text`, which PostgreSQL can't join to `uuid`, so the first `up` failed though `Open`, `check` and `diff` passed (#187). A foreign key column with no type of its own now takes the type its target was set to, as a join table's columns do, with a serial target read as the integer it is. A new NOT NULL foreign key column on a table with rows was filled with its zero value, as decision 52 fills others, but no row has key 0, so the file could never apply (#186). Such a column is now added NULL and made `NOT NULL` after, with a warning to fill it in between; left as it is, the file fails on a table with rows with "contains null values", which says why. A default that names a real row can't be known when the file is written.
