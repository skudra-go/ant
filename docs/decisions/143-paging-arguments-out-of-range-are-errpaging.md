---
id: 143
title: "Paging arguments out of range are ErrPaging"
area: queries
status: active
tags: [Page, Chunk, ChunkByKey, After, Limit, Offset, ErrPaging, ErrCursor, errors]
issues: [172]
related: [81]
changed_by: []
code: [errors.go, paging.go, builder.go]
tests: [tests/features/performance/paging_test.go, tests/features/performance/chunk_test.go, tests/features/querying/limit_offset_test.go, tests/conformance_test.go]
---

# 143. Paging arguments out of range are ErrPaging

**In short:** a page number, or a page or chunk size, below 1, and a negative `Limit` or `Offset`, wrap one sentinel, `ErrPaging`, so a handler can answer `?page=0` as a client's mistake.

## Decision

`Page(ctx, 0, 10)` returned an error that no sentinel matched, so a handler could tell a bad request from a database failure only by its text, while a bad cursor had `ErrCursor` (#172). Every paging argument ant checks now wraps `ErrPaging`: `Page`'s number and size, `After`'s size, `Chunk`'s and `ChunkByKey`'s size, and a negative `Limit` or `Offset` on a query, a join or a grouped query. The messages keep the value, as before.

Wrapping `ErrCursor` instead was rejected: a cursor is a token the query wrote, and its errors say the token can't be used, while these are numbers the caller chose. One sentinel for all of them, rather than one per call, matches how a handler answers them: the same 400.
