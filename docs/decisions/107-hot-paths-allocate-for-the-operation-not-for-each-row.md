---
id: 107
title: "Hot paths allocate for the operation, not for each row"
area: performance
status: active
tags: [allocations, Configure, dialect.Appender, qb.ParamOf, budget, sync.Pool, identity-map]
issues: [138]
related: [101, 108]
changed_by: []
code: [internal/meta/meta.go, dialect/render.go, qb/qb.go, internal/track/track.go, query.go]
tests: [tests/benchmarks/budget_test.go, tests/benchmarks/fake_test.go, dialect/appender_test.go]
---

# 107. Hot paths allocate for the operation, not for each row

**In short:** Model-derived lists are built once at Configure and shared, rows come from blocks, statements render into pooled buffers, and allocation budgets are enforced by tests.

## Decision

Allocation was most of ant's cost over plain `database/sql` (#138). A tracked read allocated 16 times for each row where an untracked one allocated 3, `SaveChanges` 4 to 13 times for each tracked entity even when one had changed, `InsertMany` 32 times for each row, and any query 50 to 90 times before it reached the database. The causes were the same few: lists derived from a model rebuilt on every call, one object for each row where a block would do, keys read through reflection and boxed twice, and a copy of every list of a query at every step of its chain.

Decision: what is derived from a model is built once, when its configuration ends (`Model.Configure` marks that), and shared: the field lists, the default select list, each field's comparer. A shared list is clipped, so an append copies it, and a caller that changes one in place copies it first. Entries and snapshots of a load, the rows of an INSERT and the nodes of a save come from blocks. A query's chain step shares with the query it came from the lists it only appends to, and copies those whose elements are changed in place (`joins`, `joined`, `order`, `omitted`); `addSource` copies the sources before it marks them nullable. The identity map is kept for each model, with a map of their own for integer keys, and the tracker keeps its entries in order instead of sorting them on every save. Statements are written into a pooled buffer, and a dialect may implement `dialect.Appender` to write identifiers and placeholders into it; `Quote` and `Placeholder` remain the interface, and a dialect without the appender renders the same text. A statement may hold a `*qb.Param` wherever it holds a `qb.Param`, so the values of many rows share one allocation; code that asks whether an expression is a bound value uses `qb.ParamOf`.

The rows of an Include are cut to size from a block, tracked or not. A slice with no room moves on its next append, and the tracker follows each tracked element there by its key (decision 108).

`tests/benchmarks/fake_test.go` is a `database/sql` driver that answers from memory, which makes the counts exact, and `budget_test.go` holds them as tests, for each row and for the operation apart from its rows. A budget is raised only on purpose. The budgets skip themselves under the race detector, which drops a random share of what goes back into a `sync.Pool`, so counts there move from run to run; CI's database jobs run them without it. `docs/api.txt` gains `dialect.Appender`, `dialect.AppendQuoteWith`, `dialect.AppendNumbered` and `qb.ParamOf`; package `ant` is unchanged.
