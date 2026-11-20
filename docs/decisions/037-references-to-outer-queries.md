---
id: 37
title: "References to outer queries"
area: queries
status: active
tags: [subquery, outer-reference, probes, terminal-call, Where]
issues: []
related: [18]
changed_by: []
code: [builder.go, subquery.go, query.go]
tests: [tests/features/querying/subquery_test.go]
---

# 37. References to outer queries

**In short:** Probe checks run at the terminal call, so Where can precede Join and subqueries can refer to outer queries.

## Decision

Probe checks run at the terminal call, not as conditions are added. That lets `Where` come before `Join`, and lets a subquery refer to its outer query: unresolved models pass to the outer query, which checks them in turn.
