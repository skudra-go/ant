---
id: 48
title: "Replicas"
area: transactions
status: active
tags: [WithReplicas, replica, OnPrimary, WithStickyWrites, read-write-split]
issues: []
related: [61]
changed_by: []
code: [run.go, options.go, tx.go]
tests: [tests/features/transactions/replicas_test.go, tests/concurrency/callbacks_test.go]
---

# 48. Replicas

**In short:** WithReplicas routes plain reads outside transactions to replicas in turn; locking reads, transactions, OnPrimary and WithStickyWrites recent writes use the primary, and a failing replica errors.

## Decision

`WithReplicas` sends plain reads outside transactions to replicas in turn. A locking read, a transaction, `OnPrimary`, or a recent write with `WithStickyWrites` uses the primary. A failing replica is an error for that read, not a silent fallback.
