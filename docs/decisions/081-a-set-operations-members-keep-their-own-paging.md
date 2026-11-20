---
id: 81
title: "A set operation's members keep their own paging"
area: queries
status: active
tags: [Union, set-operations, derived-table, limit, offset, Chunk, ChunkByKey, Page, After, ErrCursor]
issues: [92]
related: []
changed_by: []
code: [paging.go, subquery.go, query.go, dialect/render.go]
tests: [tests/conformance_test.go]
---

# 81. A set operation's members keep their own paging

**In short:** Each member of Union and other set operations is built alone, a paged member becomes a derived table, and Chunk, ChunkByKey and Page read inside the query's own Limit and Offset.

## Decision

`Union` built its statement from the left query and appended the right one, so the left query's `ORDER BY`, `LIMIT`, and `OFFSET` were written after the right member, where SQL applies them to the whole set. The youngest two users united with the oldest came back as the youngest of all three, a `Delete` on such a set removed rows outside it, and an ordering by a table's column failed on PostgreSQL, which orders a set only by its output columns. SQLite writes members without parentheses, so there the right member's paging escaped too, and SQL Server and SQLite refuse an ordered member outright.

Each member is now built on its own. A paged member becomes a derived table, `SELECT * FROM (... LIMIT 2) AS users`, which every database reads as one member. A member that orders without paging loses its order, since order cannot change which rows a set holds. `Recursive` takes its starting rows the same way. `TestConformanceSetMembers` checks every pair of six member shapes under each of the four operations, on all five databases.

The paging terminals keep the query's own paging too (#92). `Chunk`, `ChunkByKey`, and `Page` used to overwrite a `Limit` and `Offset` the caller had set, so a shared "newest thousand" query walked the whole table when chunked, and `Page` returned a `Total` counted within the limit beside items read without it. They now read inside that window: the offset is where the first group starts and the limit is where the last one ends, in key order for `ChunkByKey`, which orders by key. `After` cannot do the same, because a cursor holds a place and not a count of the rows before it, so it refuses such a query with `ErrCursor`.
