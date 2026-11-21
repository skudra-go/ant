---
id: 136
title: "An outer join's side reads what only the driver converts"
area: queries
status: active
tags: [IncludeJoined, LeftJoin, Select, nullableColumn, netip, arrays, Scanner]
issues: [197]
related: [25, 134]
changed_by: []
code: [projection.go, joined.go, internal/meta/read.go]
tests: [tests/features/value_handling/native_types_test.go]
---

# 136. An outer join's side reads what only the driver converts

**In short:** A column of the side of an outer join whose type only the driver converts, such as `netip.Addr` or `[]string`, is scanned into a pointer to its type, which `NULL` leaves nil, rather than through a `Scanner`.

## Decision

`IncludeJoined`, and a projection with a `LeftJoin`, read each column of the joined side through a `Scanner` that skips `NULL`, to tell a row with no match from one with values. A `Scanner` is given what `database/sql` makes of the value, text for an `inet` or an array, and it converted that with `database/sql`'s rules, which refuse a `netip.Addr`, a slice or a `[16]byte`. pgx scans those itself when it is given the field, so `Find` read them and `IncludeJoined` failed with "unsupported Scan".

A field whose type `database/sql` converts by itself, a scalar, bytes, a time, a `Scanner`, or a pointer to one, still goes through that `Scanner`, as does one ant reads itself (decision 134). Any other is scanned into a pointer to its type, which the driver leaves nil for `NULL` and fills otherwise, and the value is moved into the field after the row is read; a value there marks the side as found, as a value in the `Scanner` does.
