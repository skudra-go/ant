---
id: 60
title: "One program, two jobs"
area: migrations
status: active
tags: [Config.Embedded, embed, migrate, Dir, go-run]
issues: []
related: []
changed_by: []
code: [migrate/config.go, migrate/source.go]
tests: [tests/features/migrations/source_test.go]
---

# 60. One program, two jobs

**In short:** Config.Embedded is used only when the migrations folder is missing on disk, so one main.go both writes files in development and runs embedded ones when deployed.

## Decision

`go run ./cmd/migrate` writes files, and a deployed binary has no folder to read. `Config.Embedded` is used only when the folder is missing, so one `main.go` does both, with no flag to forget.
