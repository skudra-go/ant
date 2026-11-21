---
id: 137
title: "A uint64 column on PostgreSQL is numeric(20,0)"
area: values
status: active
tags: [ColumnType, uint64, numeric, bigint]
issues: [206]
related: [134]
changed_by: []
code: [dialect/postgres/ddl.go]
tests: [dialect/postgres/ddl_test.go, tests/features/value_handling/uint64_test.go]
---

# 137. A uint64 column on PostgreSQL is numeric(20,0)

**In short:** `uint64` was `bigint`, which stops at `MaxInt64`, so a hash or a bitmask with the top bit set did not fit the column ant generated for it.

## Decision

`ColumnType` gives a `uint64` `numeric(20,0)`, which holds all twenty digits of its maximum, 18446744073709551615. Decision 134 says a type PostgreSQL has a column for maps to it, and `bigint` is not that column.

Before, a `uint64` field was `bigint` as `int64` was, so `Insert` failed with `9223372036854775813 is greater than maximum value for int64` — the shape #206 reported. A `uint` key was read through an `int64(...)` cast as well and wrapped, which this does not change: see below.

`uint`, `uint32`, `int` and `int64` stay `bigint`. `uint32` and the two signed types fit, and `uint`'s width follows the platform, so a schema that changed with the build host would be worse than one column that cannot take its top half.

`sql.Null[uint64]` does not get this far: pgx refuses its value while encoding, `uint64 values with high bit set are not supported`, whatever the column type. A nullable field of that type still cannot hold a value past `math.MaxInt64`; `*uint64` and a bare `uint64` can.

Rejected:

- `numeric` without a precision, as `decimal.Decimal` takes: a `uint64` has a fixed 20 digits, and the width says so where a reader looks for it.
- `uint` as `numeric(20,0)` too: on a 32-bit build the column would grow past what the field can hold, and the type of a column should not depend on where ant was compiled.
