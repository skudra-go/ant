---
id: 72
title: "Partitions are not migrations"
area: migrations
status: amended
tags: [partitions, MonthlyPartitions, drift, postgres, partitioned-table]
issues: []
related: [57, 73]
changed_by: [130]
code: [dialect/postgres/partitions.go, dialect/postgres/inspect.go]
tests: [tests/features/migrations/modeling_test.go]
---

# 72. Partitions are not migrations

**In short:** Migrations create a partitioned parent and postgres.MonthlyPartitions manages its months, while drift ignores partitions.

> **Amended.** Decision 130: diff, check and drift compare how the parent is partitioned, and a change to it is a step to take by hand. Drift still ignores the partitions themselves.

## Decision

Migrations create a partitioned parent, and `postgres.MonthlyPartitions` manages its months. Partitions come and go on a schedule, which a file history cannot describe, so `drift` ignores them.
