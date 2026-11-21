---
id: 137
title: "Stored tsvector columns have a separate Match helper, and Match writes its configuration"
area: dialects
status: active
tags: [postgres, Match, MatchVector, tsvector, full-text, regconfig, indexes]
issues: [173]
related: [43]
changed_by: []
code: [dialect/postgres/functions.go]
tests: [tests/features/value_handling/postgres_test.go]
---

# 137. Stored tsvector columns have a separate Match helper, and Match writes its configuration

**In short:** `Match` converts a text column with `to_tsvector`, its configuration written into the statement so an expression index serves it; `MatchVector` applies the full-text operator directly to a precomputed `tsvector` column.

## Decision

PostgreSQL's `to_tsvector` accepts text, not an already computed `tsvector`. `MatchVector` makes that column kind explicit at the call site and applies `@@ websearch_to_tsquery` directly, so stored vectors can use their index. Both helpers use the same text search configuration and websearch syntax.

`Match` bound its configuration as `$1::regconfig`. PostgreSQL resolves a text search configuration's name only when the statement runs, so the planner never matched `to_tsvector($1::regconfig, title)` to an index on `to_tsvector('english', title)`, even with the parameter's value at hand, and every search read the whole table (#173). `Match` now writes the configuration as a literal, `'english'::regconfig`, which the planner matches to the index under a generic plan as under a custom one. A name with a quote or a backslash could end the literal, and a `?` would read as a placeholder, so `Match` refuses such a name; a configuration's name holds none of them in practice. `MatchVector` binds its configuration still, as the query side of `@@` does not decide which index serves it.

Detecting a `tsvector` column inside `Match` was rejected: a condition does not know the column's type, and guessing it from the model's `.Type` would miss a column the database made.
