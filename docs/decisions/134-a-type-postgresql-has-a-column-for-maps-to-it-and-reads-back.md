---
id: 134
title: "A type PostgreSQL has a column for maps to it, and reads back"
area: values
status: active
tags: [ColumnType, interval, inet, cidr, jsonb, numeric, arrays, time.Duration, net.IP, netip, json.RawMessage, decimal, IntervalStyle, AvgOf, ErrNoColumnType]
issues: [197]
related: [25, 42, 84, 95, 109, 125]
changed_by: []
code: [dialect/postgres/ddl.go, internal/meta/read.go, internal/meta/interval.go, internal/scan/scan.go, schema.go, aggregate.go, dialect/bind.go, unit.go]
tests: [tests/features/value_handling/native_types_test.go, internal/meta/read_test.go, dialect/postgres/ddl_test.go, tests/features/value_handling/probes]
---

# 134. A type PostgreSQL has a column for maps to it, and reads back

**In short:** On PostgreSQL, `time.Duration` is `interval`, `net.IP` and `netip.Addr` are `inet`, `netip.Prefix` is `cidr`, `json.RawMessage` and a map with string keys are `jsonb`, `decimal.Decimal` is `numeric` and a slice of a mapped type is an array of it; ant reads each back, an interval in any `IntervalStyle`.

## Decision

`json.RawMessage` and `net.IP` were `bytea`, and `time.Duration` was `bigint`, because they are a byte slice and an `int64`. A team adopting a schema that keeps them in `jsonb`, `inet` and `interval` saw `drift` propose changing those columns to `bytea` and `bigint`, and `netip.Addr`, `netip.Prefix`, `[]string`, `[]int64` and `decimal.Decimal` had no column type at all (#197). Setting the type by hand did not help: pgx hands `interval` and `inet` to `database/sql` as text, which `database/sql` cannot scan into a `time.Duration` or a `net.IP`, so every read failed.

`ColumnType` now maps each of these to PostgreSQL's own type. A slice is an array of its element's type, `[]string` `text[]` and `[]time.Duration` `interval[]`, with the field's length or precision on the element, unless the slice is a `Valuer` or a `Scanner`, as one that stores itself as JSON is: ant can't know what such a slice holds, so it still asks for a type. A slice of slices, a slice of maps and `decimal.NullDecimal`, for which `sql.Null[decimal.Decimal]` maps, have none either. `decimal.Decimal` is matched by its package and name, as `dialect/postgres` imports no library. The zero `netip.Addr` and `netip.Prefix` are no address, which pgx writes as `NULL` and reads `NULL` as, so their columns allow `NULL`, as `ant.DeletedAt`'s does.

Reads go through a `Scanner` of ant's for the types `database/sql` can't fill, found once per field as the UTC conversion of decision 84 is, at every place ant scans a row:

- A `time.Duration`, `*time.Duration` or `sql.Null[time.Duration]` reads a whole number as nanoseconds, as before, and other text as an interval in each `IntervalStyle` PostgreSQL has, and as pgx writes one, counted as `EXTRACT(EPOCH FROM ...)` counts it: a year is 365.25 days, a month left over 30 days, a day 24 hours. An infinite interval, or one past about 292 years, is an error, not a wrapped number. An interval keeps microseconds, so pgx cuts a `time.Duration`'s nanoseconds below one toward zero when it writes it.
- A `net.IP`, `*net.IP` or `sql.Null[net.IP]` reads bytes as they are, as a `bytea` held them, and text as an address, four bytes for IPv4. An `inet` with a netmask, such as `10.0.0.1/24`, is an error naming `netip.Prefix`, as pgx refuses one for a `netip.Addr`.
- A named byte slice, such as `json.RawMessage`, reads `NULL` as nil, as `database/sql` reads it into a `[]byte`, where it refused it, and text as its bytes. `sql.RawBytes` keeps its meaning.
- A `float64`, `*float64` or `sql.Null[float64]` reads an interval as its nanoseconds, so `Avg`, `AvgOf` and `AvgRelated` of a `time.Duration` give nanoseconds from an interval column, whose `AVG` is an interval, as from an integer one. Text that is neither a number nor an interval is `database/sql`'s error, as before.
- A map with string keys reads JSON with `json.Number`, as `ant.JSON` does (decision 109); pgx's own decoding would give `float64` and lose a large integer's digits. A `[]time.Duration` is bound as it is, not as `[]int64`, as pgx writes one as an `interval[]`, and as nanoseconds to a `bigint[]`.

Rejected:

- Mapping none of these, as #197's first fix did, keeps every adopted `inet` and `interval` column as drift and leaves the reads failing.
- Reading through pgx's own types would put a driver in the root package, which decision 71 keeps out.
- pgx's own types, such as `pgtype.Interval`, need rules for their `Valid` field to be nullable; `time.Duration` now reads an interval itself.
- Writing a 16-byte IPv4 `net.IP`, as `net.ParseIP` returns, as IPv4: ant can't tell at the time it binds a value whether the column is `inet` or a `bytea` that keeps the 16 bytes, so pgx's `::ffff:10.0.0.1` stands, and the docs say to call `To4` or use `netip.Addr`.
