---
id: 12
title: "Additions to the `Dialect` contract"
area: dialects
status: active
tags: [Dialect, DriverName, Open, database-sql]
issues: []
related: []
changed_by: []
code: [dialect/dialect.go, ant.go]
tests: [dialect/dialect_test.go]
---

# 12. Additions to the `Dialect` contract

**In short:** The Dialect interface includes DriverName so Open can call sql.Open without the core importing a driver.

## Decision

The brief lists five `Dialect` methods. `Open` also needs the `database/sql` driver name, so the interface adds `DriverName() string`. The application still imports the driver, so the core has no driver dependency.
