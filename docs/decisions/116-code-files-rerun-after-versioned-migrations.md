---
id: 116
title: "Code files rerun after versioned migrations"
area: migrations
status: pending
pr: 274
tags: [repeatable, code-files, views, migrate-up, outdated]
issues: [175]
related: [11, 78, 94]
changes: []
code: [migrate/engine.go, migrate/cli.go, migrate/migration.go]
tests: [tests/features/migrations/repeatable_test.go]
---

# 116. Code files rerun after versioned migrations

**In short:** Files in code/ rerun after any versioned migration in the same up, and are outdated when a versioned migration was applied after them.

> **Pending.** Added by pull request #274, not merged yet. When it merges, set `status: active`.

## Decision

Files in `code/` reran only when their text changed. A versioned migration that changes a column a view reads has to drop the view first, and the view never came back, because its file did not change: `status` and `drift` said everything was applied (#175). Now a code file reruns whenever a versioned migration runs before it in the same `up`, and is `outdated` when a versioned migration was applied after it, as after `up --to`, which applies no code file, or a process that stopped between the two. Code files must already be safe to run again, so rerunning one that changed nothing is harmless, and a database whose code files last ran before its newest versioned migration reruns them once after upgrading. The rerun in the same `up` needs no clock, but `outdated` compares the times the processes that ran `up` recorded, so it relies on their clocks agreeing, as synchronised servers' do. The order stays: on a new database the code files run after every versioned migration, so a versioned migration cannot use a function one defines, and the docs say so. A directive in a versioned file asking for a code file first was rejected for now: the rerun covers the file that is lost, and defining what a data migration needs inside it keeps the migration working when the code file changes later.
