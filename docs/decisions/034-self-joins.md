---
id: 34
title: "Self-joins"
area: relations
status: active
tags: [self-join, alias, Join, probes]
issues: []
related: []
changed_by: []
code: [builder.go, join.go]
tests: [tests/features/relations/self_reference_test.go]
---

# 34. Self-joins

**In short:** The second occurrence of a table in a join gets an alias; conditions on a self-joined model apply to the first side.

## Decision

The second occurrence of a table gets an alias, so each side scans into its own row. Probes cannot tell the two sides apart, so a condition on a self-joined model applies to the first side.
