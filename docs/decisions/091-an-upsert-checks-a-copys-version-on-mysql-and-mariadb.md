---
id: 91
title: "An upsert checks a copy's version on MySQL and MariaDB too"
area: writes
status: active
tags: [Upsert, version, mysql, mariadb, on-duplicate-key, ErrConcurrency, optimistic-concurrency]
issues: []
related: [80, 89]
changed_by: []
code: [upsert.go, dialect/render.go, dialect/syntax.go]
tests: [tests/features/lifecycle/upsert_version_test.go, tests/conformance_test.go]
---

# 91. An upsert checks a copy's version on MySQL and MariaDB too

**In short:** On MySQL and MariaDB an upsert guards each assignment with CASE WHEN version matches, so a stale copy changes nothing and returns ErrConcurrency.

## Decision

`ON DUPLICATE KEY UPDATE` takes no condition, so a stale copy's update went ahead there (decision 80). Now each of its assignments keeps the stored value unless the version is the copy's: `col = CASE WHEN version = ? THEN new ELSE col END`. MySQL and MariaDB run them in order, each seeing what came before, and the version's comes last, so every one reads the version the row had. A stale copy then changes nothing, which the affected-row count shows, and the upsert returns `ErrConcurrency`, as on PostgreSQL. This needs the driver's `clientFoundRows` off, its default, which detecting `DoNothing` needed already.
