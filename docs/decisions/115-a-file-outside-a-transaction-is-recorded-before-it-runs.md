---
id: 115
title: "A file outside a transaction is recorded before it runs"
area: migrations
status: pending
pr: 274
tags: [no-transaction, migrate-up, history, crash, ErrFailed]
issues: [180]
related: [56]
changes: []
code: [migrate/engine.go, migrate/history.go]
tests: [tests/features/migrations/up_test.go]
---

# 115. A file outside a transaction is recorded before it runs

**In short:** A no-transaction migration is recorded as failed before its first statement, so a process killed partway leaves it failed rather than pending.

> **Pending.** Added by pull request #274, not merged yet. When it merges, set `status: active`.

## Decision

A no-transaction migration that failed partway was recorded as failed, so `up` refused to go on until someone had looked, but only when the process lived to write that row. After a kill, an out-of-memory exit or a lost connection nothing was recorded, and the next `up` ran the file again from its first statement, which repeated what had run (#180). Now `up` records the file as failed, as not finished, before it runs the first statement, and replaces that row once the file ends, with its success or its error. Each of those writes is a transaction of its own, so a crash between taking out the old row and putting in the new one cannot leave neither. While the file runs, `status` from another process shows it failed with the same message, which is true of what the database holds until it ends. A file in a transaction needs none of this, since it and its row commit together.
