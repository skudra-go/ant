---
id: 9
title: "Owner review of Step 1"
area: process
status: historical
tags: [owner-review, Select, closure, step-1]
issues: []
related: [4, 5]
changed_by: []
code: [join.go]
tests: []
---

# 9. Owner review of Step 1

**In short:** The owner accepted decisions 4 and 5; only the Select closure runs in Go, while join, filters and ordering run in SQL.

> **Historical.** Review record; the rule about Select is carried by decision 5.

## Decision

Decisions 4 and 5 were accepted by the project owner on 2026-10-31. In decision 5, only the closure runs in Go. The join, filters, and ordering run in the database.
