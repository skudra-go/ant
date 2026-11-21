---
id: 135
title: "A column ant made for an earlier default is converted as ant stored it"
area: migrations
status: active
tags: [AlterColumn, USING, interval, inet, jsonb, bigint, bytea, Type, upgrade]
issues: [197]
related: [52, 78, 94, 134]
changed_by: []
code: [dialect/postgres/ddl.go, schema/change.go]
tests: [tests/features/migrations/native_types_test.go, dialect/postgres/ddl_test.go]
---

# 135. A column ant made for an earlier default is converted as ant stored it

**In short:** The diff that moves a `bigint` to `interval`, or a `bytea` to `inet` or `jsonb`, converts the values as ant wrote a `time.Duration`, `net.IP` or `json.RawMessage` there, says so in its warning, and a model that names the old type keeps the column.

## Decision

Decision 134 changed the column type of three Go types that had one, so the next `diff` of an application that made its tables with ant proposes `bigint` to `interval`, `bytea` to `inet`, and `bytea` to `jsonb`. PostgreSQL has a cast for none of them, so the `USING col::type` ant wrote could never apply.

`ALTER COLUMN ... TYPE` now converts those three as ant stored the values: nanoseconds to an interval of the microseconds in them, cut toward zero as pgx cuts them; the 4 or 16 bytes of a `net.IP` to the address they spell, with a 16-byte IPv4 kept as `::ffff:a.b.c.d`, the value pgx binds for that `net.IP` now, so a condition finds the rows it found before; and the UTF-8 text of a `json.RawMessage` to `jsonb`. A `bytea` that is no address becomes `NULL`. The warning names how the values are read, so that someone whose `bigint` held seconds, not a `time.Duration`, sees it in the file they review. Any other change of type keeps its cast and warning.

A model that names the old type, as `b.Column(&Hooks.Timeout).Type("bigint")`, keeps the column with no migration, and its values read as they did (decision 134).

Keeping the old type for a column ant made before, with the new one only for new columns, was rejected: two databases of one application would differ in schema by when their tables were made, and the snapshot would have to remember which default made each column.
