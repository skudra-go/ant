---
id: 41
title: "Automatic timestamps"
area: models
status: active
tags: [timestamps, CreatedAt, UpdatedAt, utc, NoTimestamps, microseconds]
issues: []
related: [96, 111]
changed_by: []
code: [internal/meta/meta.go, unit.go, config.go]
tests: [tests/features/lifecycle/timestamps_test.go, tests/features/lifecycle/utc_test.go]
---

# 41. Automatic timestamps

**In short:** CreatedAt and UpdatedAt time.Time fields are maintained on any model, stored in UTC truncated to microseconds; NoTimestamps opts out.

> **Note.** Decisions 96 and 111 refine how UpdatedAt is stamped on forced and replaced updates.

## Decision

`time.Time` fields named `CreatedAt` and `UpdatedAt` are maintained on any model, as in GORM and Eloquent. They are stored in UTC, truncated to microseconds, so a value reads back exactly as written. A value the application set on insert is kept. `b.NoTimestamps()` opts out.
