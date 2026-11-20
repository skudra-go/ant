---
id: 1
title: "`ant.Ordered` replaces `cmp.Ordered`"
area: queries
status: active
tags: [Ordered, time, OrderBy, Gt, cmp-ordered]
issues: []
related: []
changed_by: []
code: [cond.go, query.go]
tests: [ant_test.go]
---

# 1. `ant.Ordered` replaces `cmp.Ordered`

**In short:** Ordering and comparison conditions accept time.Time as well as cmp.Ordered types through ant.Ordered.

## Decision

The brief declares `Gt[V cmp.Ordered]` and `OrderBy[V cmp.Ordered]`. `time.Time` does not satisfy `cmp.Ordered`, so `OrderBy(&Users.CreatedAt)` and `Gt(&Users.CreatedAt, t)` do not compile, although `ant.Base` supplies those columns. EF Core orders by dates.

Decision: define `type Ordered interface{ cmp.Ordered | time.Time }` and use it for ordering and comparison conditions. Other orderable types, such as UUID or decimal types, remain an open extension.
