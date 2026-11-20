---
id: 39
title: "Global filters are lazy"
area: queries
status: active
tags: [global-filter, qb.Lazy, tenant, Find, identity-map, join-on]
issues: []
related: []
changed_by: []
code: [qb/qb.go, dialect/render.go, table.go]
tests: [tests/features/lifecycle/filters_test.go]
---

# 39. Global filters are lazy

**In short:** Global filters are lazy qb.Lazy nodes resolved with the terminal call's context, reach joins, Include, Has and subqueries, and Table.Find skips the identity map for filtered models.

## Decision

Filters can read the request context, such as a tenant, but subqueries are built before any context exists. A filter is a `qb.Lazy` node that the renderer resolves with the terminal call's context. So filters reach joins, `Include`, `Has`, and subqueries without anything having to remember them, and the same query object run under two tenants sees different rows. On a joined table, filters go in the `ON` clause, so they do not turn a left join into an inner one.

`Table.Find` skips the identity map for models with filters, because a tracked row may be one the filter now hides.
