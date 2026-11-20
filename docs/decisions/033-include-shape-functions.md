---
id: 33
title: "Include shape functions"
area: relations
status: active
tags: [Include, ROW_NUMBER, ErrIncludeInEach, ErrNoKey, per-parent-limit, many-to-many]
issues: [160]
related: []
changed_by: []
code: [include.go, query.go]
tests: [tests/features/loading/shaped_include_test.go, tests/features/loading/include_test.go]
---

# 33. Include shape functions

**In short:** Include takes an optional shape function; Limit and Offset apply per parent using ROW_NUMBER, Each refuses Include, and join-key columns are always read.

## Decision

`Include` takes an optional function over the related query, instead of separate APIs for filtering, ordering, limiting, and nesting. `Limit` and `Offset` apply per parent, using `ROW_NUMBER`. `Each` refuses includes with `ErrIncludeInEach`, because a second query cannot run on a connection that is streaming rows inside a transaction. An include matches rows by a column on each side, so both queries read theirs even when `Column` or `Omit` left it out, the type column of a polymorphic relation included: without it every parent looked childless, with no error (#160). Through a join table or a table in between, one related row can belong to several parents and be inside the limit of some only, so the rows are numbered where the join table is joined to the related query, for each parent, and that query returns the pairs of parent and related row that were kept, by the related row's own key. A related model with no single-column key can be loaded through a table whole, and a `Limit` or `Offset` on it returns `ErrNoKey`.
