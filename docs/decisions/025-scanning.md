---
id: 25
title: "Scanning"
area: queries
status: amended
tags: [scan, Rows.Scan, partial-select, timestamps, Base]
issues: []
related: [41]
changed_by: [41]
code: [internal/scan/scan.go, internal/meta/meta.go]
tests: [tests/features/value_handling/probes/rows/probes_test.go]
---

# 25. Scanning

**In short:** Scanning ignores columns with no field and fields with no column, using Rows.Scan; timestamp handling was later made automatic.

> **Amended.** The zero-time-stored-as-0001-01-01 remark is replaced by automatic timestamps (decision 41) that stamp CreatedAt/UpdatedAt.

## Decision

Result columns with no field are ignored, and fields with no column are left alone, so `SELECT name` into a model works. `internal/scan` uses `Rows.Scan`, which calls `sql.ConvertAssign` itself, instead of the direct calls the brief describes.

`ant.Base` timestamps are written as they are until automatic timestamps arrive in v0.6, so a zero time is stored as `0001-01-01`.
