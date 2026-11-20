---
id: 129
title: "status --check fails while anything waits"
area: migrations
status: active
tags: [status, ErrPending, ErrChecksum, CI, deploy]
issues: [188]
related: [56]
changed_by: []
code: [migrate/cli.go]
tests: [tests/features/migrations/up_test.go]
---
# 129. status --check fails while anything waits

**In short:** status --check returns ErrPending while a migration waits, and the error up would refuse with otherwise; plain status only lists.

## Decision

`status` exited 0 whatever it listed, a `changed` file included, and nothing returned `ErrPending`, so a deploy gate had to parse its table (#188). `status --check` now fails with `ErrPending` while anything waits to run, joined with the errors `up` would refuse with, such as `ErrChecksum`. A failing plain `status` was rejected: it is how people look, and scripts run it to print.
