---
id: 118
title: "A SQLite DSN says whether foreign keys are on"
area: dialects
status: active
tags: [sqlite, foreign-keys, dsn, CheckDSN]
issues: [214]
related: [74]
changed_by: []
code: [dialect/sqlite/sqlite.go]
tests: [dialect/sqlite/sqlite_test.go]
---

# 118. A SQLite DSN says whether foreign keys are on

**In short:** Open refuses a SQLite DSN that does not set foreign_keys, in any of the driver's forms; foreign_keys(0) keeps them off on purpose.

## Decision

SQLite turns foreign keys off on every new connection unless the connection turns them on, so a model's `REFERENCES` constraints were created and never enforced: a row whose foreign key pointed at nothing was inserted with no error, and nothing at `Open` said so (#214). `Open` now refuses a SQLite data source name that does not set them, as it refuses a MySQL one without `parseTime`, and the error names `_pragma=foreign_keys(1)`. The check looks only at whether the DSN names the setting, in any of the driver's forms (`_pragma=foreign_keys(...)`, `_foreign_keys`, `_fk`), not at its value, so `_pragma=foreign_keys(0)` keeps them off on purpose. `Wrap` takes a pool the application opened and checks nothing. Reading `PRAGMA foreign_keys` from a connection was rejected: a database with them off on purpose would have had no way through.
