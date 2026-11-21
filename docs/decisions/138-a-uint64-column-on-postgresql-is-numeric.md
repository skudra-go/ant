---
id: 138
title: "A uint64 column on PostgreSQL is numeric(20,0), and a uint64 key keeps its value"
area: values
status: active
tags: [ColumnType, uint64, uint, numeric, bigint, identity, keys, foreign keys, KeyOf, ManyToMany]
issues: [206]
related: [134, 135]
changed_by: []
code: [dialect/postgres/ddl.go, schema.go, internal/meta/meta.go, run.go, include.go, pivot.go]
tests: [dialect/postgres/ddl_test.go, tests/features/value_handling/uint64_test.go, tests/features/migrations/uint64_test.go, tests/wide_keys_test.go, internal/meta/key_test.go]
---

# 138. A uint64 column on PostgreSQL is numeric(20,0), and a uint64 key keeps its value

**In short:** `uint64` is `numeric(20,0)`, except for a key the database generates and a foreign key to one, which stay `bigint`; a foreign key to a uint64 key takes its type; and a `uint` or `uint64` key past `math.MaxInt64` is read as a `uint64`, not wrapped into a negative `int64`.

## Decision

`uint64` was `bigint`, as `int64` is, which stops at `math.MaxInt64`, so a hash or a bitmask with its top bit set did not fit the column ant made for it: `Insert` failed with `9223372036854775813 is greater than maximum value for int64` (#206). `ColumnType` now gives a `uint64` `numeric(20,0)`, which holds all twenty digits of its maximum, as decision 134 gives a type the column PostgreSQL has for it.

An identity can only be a `smallint`, `integer` or `bigint`, so a `uint64` key the database generates would have failed `CREATE TABLE`. Its values count up from 1 and stay far below `math.MaxInt64`, so it stays `bigint`, and a database made before keeps it. A foreign key whose field or target is a `uint64` takes its target's type, as one to a typed key does (#187): PostgreSQL refuses a foreign key from `numeric` to `bigint` or back. The next `diff` of a table ant made converts its other `uint64` columns, which PostgreSQL casts with no loss; `.Type("bigint")` keeps one.

A key that fits an `int64` is still read as one, whatever integer holds it, so an `int64` foreign key finds a `uint64` key and the tracker's fast path is unchanged. A `uint` or `uint64` past `math.MaxInt64` was read through `int64(...)` and wrapped, and that negative number was written to the foreign keys of a graph, bound in `Include`, and named in a conflict's error. It is now its `uint64`. The keys of a join table are read into their key fields' types, as their models read them: read into an interface, a `NUMERIC` or `BIGINT UNSIGNED` column came as text and matched no key, so `Include` of a many-to-many found nothing and `Toggle` linked again what it had just removed.

`uint` stays `bigint`. It is what Go code uses for an ordinary count or key, such as GORM's `ID uint`, where `numeric` would cost space and speed and migrate many columns for values that fit; a value past `math.MaxInt64` in one fails on insert, and the docs say to use `uint64`.

`sql.Null[uint64]` does not get as far as the column: pgx refuses its value while encoding, `uint64 values with high bit set are not supported`, whatever the column type. `*uint64` and a bare `uint64` hold every value.

Rejected:

- `numeric` without a precision, as `decimal.Decimal` takes: a `uint64` has a fixed 20 digits, and the width says so where a reader looks for it.
- `numeric(20,0)` for a generated key, with a sequence in place of an identity: it would change the schema of every application with a `uint64` key, for values no sequence reaches.
