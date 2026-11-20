---
id: 84
title: "Every time is bound in UTC where columns keep no zone"
area: values
status: active
tags: [utc, time, sql-null, nulltime, DeletedAt, raw-query, sqlserver, sqlite]
issues: [218]
related: [76]
changed_by: []
code: [dialect/bind.go, internal/meta/utc.go, internal/meta/meta.go, internal/scan/scan.go, dialect/render.go]
tests: [dialect/utc_test.go, tests/conformance_test.go, tests/features/lifecycle/utc_test.go, internal/meta/meta_test.go]
---

# 84. Every time is bound in UTC where columns keep no zone

**In short:** ant binds every time shape and raw argument in UTC on zone-less databases and returns scanned times in UTC, without double-asking other Valuers.

## Decision

SQL Server's `DATETIME2` and SQLite's text keep a time's wall clock and drop its zone, so ant binds times in UTC there, as decision 76 says. Only a plain `time.Time` was converted, though. A `*time.Time`, `sql.Null[time.Time]`, `sql.NullTime`, `ant.DeletedAt`, a sensitive field's wrapped value, and every argument of a raw query went in with the application's zone, so a nullable time stored three hours off in a zone three hours from UTC, and a condition on it found nothing.

The renderer and the raw-query binder now share one conversion. It sees through pointers, as many as database/sql would follow, and interfaces anywhere along them, `qb.Secret` and a pointer to one, which stay secrets, as a secret inside a `Valuer` that gives a time does once the `Valuer` is asked, and `sql.NullTime` and an `sql.Null` of a time, of a pointer to one, of a `Valuer` that gives one, or of an interface that holds one when it is bound, and structs built on them, such as `ant.DeletedAt`, and it leaves NULL as NULL. Raw arguments are converted after an `IN` list expands, so each element is. Other `driver.Valuer` types are left alone rather than asked for their value: a JSON or encrypted value would be serialized twice, once to look for a time and once by database/sql. A type built on a time is asked once, and its value, or its error, goes on in its place, so one that overrides `Value` is not asked twice either. A `Valuer` of one's own that gives a time is the application's to give in UTC. A pointer chain that comes back to itself binds an error rather than being followed for ever. `TestConformanceTimeShapes` stores and finds each shape in a zone three hours from UTC on all five databases, and reads the stored clock back through database/sql, past ant.

Reads are held in UTC too (#218). pgx decodes `TIMESTAMPTZ` in the process's zone, so a row read back differed from the one written, and a JSON API rendered a create as `...Z` and a get as `+02:00`; the session's `timezone` does not change that. Every place ant scans a row (queries, `Raw`, projections and joins, `IncludeJoined`, and a value an insert reads back) moves a `time.Time`, `*time.Time`, `sql.NullTime`, `sql.Null[time.Time]`, `ant.DeletedAt` or `[]time.Time` field, and a time read as a single value, to UTC after `database/sql` has scanned it. The instant does not move, and a NULL or a value no time comes from is still refused by `database/sql` in its own words. The field's conversion is found once, with its other per-type helpers, so a row pays only for its time columns. A `Scanner` of one's own is the application's to give in UTC. The lifecycle feature tests run with `time.Local` two hours from UTC, so they fail on a machine whose zone is UTC as well.
