---
id: 5
title: "`Select` closures run client-side"
area: queries
status: active
tags: [Select, Join2, closure, Project, client-side]
issues: []
related: [9]
changed_by: []
code: [join.go, projection.go]
tests: [tests/features/relations/join_test.go, tests/features/relations/project_test.go]
---

# 5. `Select` closures run client-side

**In short:** Join Select closures run in Go once per row over all mapped columns of each joined table; Project and Into prune columns.

## Decision

EF Core translates projections to SQL through expression trees. Go has no equivalent, so a closure such as `func(p *Post, u *User) Summary` cannot be inspected.

Decision: load the mapped columns of each table in the join and call the closure once per row. Column pruning needs a separate explicit API, to be proposed on the roadmap.
