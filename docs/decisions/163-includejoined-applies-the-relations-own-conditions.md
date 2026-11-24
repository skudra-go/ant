---
id: 163
title: "IncludeJoined applies the relation's own conditions in its join"
area: queries
status: active
tags: [IncludeJoined, Morph, Where, Latest, Oldest, has-one, scope]
issues: [310]
related: [17, 136]
changed_by: []
code: [joined.go]
tests: [tests/joined_relations_test.go]
---

# 163. IncludeJoined applies the relation's own conditions in its join

**In short:** `IncludeJoined` loads the same row `IncludeOne` does. Its left join's ON clause holds the relation's `Where` conditions and its `Morph` type, and for a has-one with `Latest` or `Oldest` it keeps only the row the pick chooses.

## Decision

The join was built from the key and the target's model filters alone, so a note on a video got the essay with the same id (#310), a scoped relation got rows its scope excludes, and a picked has-one returned each parent once per child.

The relation's scope goes into the ON clause the way `Join` applies it: a condition on the target reads the joined alias, and a `Morph` condition on the owner's side reads the owner, even when the owner and the target are one table. A pick adds `joined.key = (SELECT key FROM target WHERE fk = owner.key AND <scope> <filters> ORDER BY <pick>, key LIMIT 1)`. The key breaks ties, except when the pick already orders by the key, since SQL Server refuses a column twice in an ORDER BY. A correlated scalar subquery with `LIMIT` (`OFFSET ... FETCH` on SQL Server) runs on all five databases, where a `LATERAL` join does not.

A picked has-one whose target has no single-column key cannot name its row that way, so `IncludeJoined` refuses it with `ErrNotImplemented` and points to `IncludeOne`.

Rejected:

- Refusing every picked has-one: `IncludeJoined` on one already works when a parent has one child, and callers use it.
- A `ROW_NUMBER` derived table, as `IncludeOne` reads a limited include: the joined columns would come from a subquery, and the read path scans the target's own columns.
