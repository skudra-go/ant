---
id: 76
title: "One clock for times without a zone"
area: dialects
status: active
tags: [utc, UTCTimes, sqlite, sqlserver, datetime2, time-zone]
issues: []
related: [74]
changed_by: []
code: [dialect/bind.go, dialect/syntax.go, dialect/sqlite/sqlite.go, dialect/sqlserver/sqlserver.go]
tests: [dialect/utc_test.go, tests/features/lifecycle/utc_test.go]
---

# 76. One clock for times without a zone

**In short:** Dialects whose columns drop the zone (SQLite, SQL Server) bind every time in UTC via Syntax.UTCTimes.

## Decision

SQLite compares times as text, and SQL Server's `DATETIME2` drops the zone, so a local time stored there sits hours from a UTC one and compares wrongly with no error. Those dialects bind every time in UTC.
