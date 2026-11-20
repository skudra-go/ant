---
id: 92
title: "Tracked values compare as stored"
area: tracking
status: active
tags: [change-tracking, Snapshot, nan, time, deepequal, valuer, UnixTime, SaveChanges]
issues: []
related: []
changed_by: []
code: [internal/track/snapshot.go, internal/track/track.go, internal/meta/meta.go]
tests: [internal/track/track_test.go, tests/features/change_tracking/probes_test.go]
---

# 92. Tracked values compare as stored

**In short:** Change tracking compares with DeepEqual except NaN equals NaN and equal instants match in any zone, so unchanged rows write nothing.

## Decision

A float field holding NaN was never equal to its snapshot, and an `sql.Null[time.Time]` or `ant.UnixTime` at the same instant in another zone counted as changed, so every save wrote an update that changed nothing. Comparison now follows `reflect.DeepEqual` except that NaN equals NaN and a time equals another at the same instant, at any depth, as a `time.Time` field already did. Nil and empty still differ, as NULL and empty do. A snapshot copies what an `any` holds, so an edit inside a map or slice that JSON decoded is seen. A time in a key is the instant in UTC, to the microsecond, which PostgreSQL and MySQL keep and ant's own timestamps hold, so a row read back is the instance already tracked; two keys less than a microsecond apart are one row to the tracker, which SQL Server and SQLite could store as two. A slice type that is a `driver.Valuer` and an `sql.Scanner` is a column, not a has-many relation, and a type whose `Value` has a pointer receiver is bound through a pointer, so its method runs.
