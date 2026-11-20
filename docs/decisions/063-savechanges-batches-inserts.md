---
id: 63
title: "SaveChanges batches inserts"
area: writes
status: active
tags: [SaveChanges, batch-insert, insertBatchLimit, hooks, Touch, WithoutAutoTx]
issues: [22]
related: [22, 47, 75]
changed_by: []
code: [unit.go, upsert.go]
tests: [tests/features/change_tracking/save_changes_test.go, tests/features/writing/bulk_test.go]
---

# 63. SaveChanges batches inserts

**In short:** SaveChanges inserts new rows of one model together up to 200 per statement, falling back to row by row for models with insert hooks, Touch relations or WithoutAutoTx.

## Decision

This revises [decision 22](#22-inserts-run-one-statement-per-row). New rows of one model go in together, up to 200 a statement, which turns a thousand round trips into a few. A model with insert hooks or a `.Touch()` relation is written row by row, as is a save with `WithoutAutoTx`, because each of those needs a result per row. Updates stay one statement each: a shared statement could only set one list of columns.
