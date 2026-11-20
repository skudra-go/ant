---
id: 122
title: "A type maps to a native column only if it reads back"
area: values
status: active
tags: [ColumnType, jsonb, inet, cidr, interval, bigint, bytea, ErrNoColumnType, RawMessage, net.IP, netip]
issues: [197]
related: [25, 74, 95]
changed_by: []
code: [dialect/postgres/ddl.go]
tests: [dialect/postgres/ddl_test.go, tests/features/value_handling/native_types_test.go]
---

# 122. A type maps to a native column only if it reads back

**In short:** The PostgreSQL column mapping gives `json.RawMessage` `jsonb`, and gives no native type to `net.IP`, `netip`, or `time.Duration`, which PostgreSQL returns as text.

## Decision

`ColumnType` maps a Go type to a native column type only when a value read back from that column scans into the same Go type through `database/sql`. `json.RawMessage` does: PostgreSQL sends `jsonb` as bytes. `inet`, `cidr` and `interval` reach `database/sql` as text, which it cannot scan into `net.IP`, `netip.Addr`, `netip.Prefix`, or `time.Duration`, so a model with those fields would build and migrate but fail on `Find`.

Those types keep what they had. `time.Duration` stays `bigint`, a count of nanoseconds, and `net.IP` stays `bytea`. `netip.Addr` and `netip.Prefix` have no mapping, so `EnsureCreated` and migrations report `ErrNoColumnType` until the column is named with `ColumnBuilder.Type`. A column named that way is still read as text.

Mapping the four types to native columns, so that drift stops proposing ALTERs against existing `inet` or `interval` columns, was rejected: it trades a migration complaint for a read that always fails. Making them scan would need a converter for each type, and that is a decision of its own.

A `json.RawMessage` column that an earlier migration made `bytea` is changed to `jsonb` by the next migration. That change is flagged with a warning, as every type change is, and the values are not converted for it.
