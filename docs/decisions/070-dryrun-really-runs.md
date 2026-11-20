---
id: 70
title: "DryRun really runs"
area: transactions
status: active
tags: [DryRun, Rollback, diagnostics, Statement, sequences]
issues: []
related: []
changed_by: []
code: [diagnostics.go, tx.go]
tests: [tests/features/developer_tooling/diagnostics_test.go]
---

# 70. DryRun really runs

**In short:** ant.DryRun runs the code in a transaction that always rolls back and returns the statements, rather than simulating results.

## Decision

`ant.DryRun` runs your code in a transaction that rolls back, rather than faking results. A simulated run cannot know what a trigger or a constraint does. The cost is stated in its documentation: sequences advance, and anything outside the database still happens.
