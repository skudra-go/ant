---
id: 137
title: "Stored tsvector columns have a separate Match helper"
area: dialects
status: active
tags: [postgres, Match, MatchVector, tsvector, full-text]
issues: [173]
related: [43]
changed_by: []
code: [dialect/postgres/functions.go]
tests: [tests/features/value_handling/postgres_test.go]
---

# 137. Stored tsvector columns have a separate Match helper

**In short:** `Match` converts a text column with `to_tsvector`; `MatchVector` applies the full-text operator directly to a precomputed `tsvector` column.

## Decision

PostgreSQL's `to_tsvector` accepts text, not an already computed `tsvector`. `MatchVector` makes that column kind explicit at the call site and applies `@@ websearch_to_tsquery` directly, so stored vectors can use their index. `Match` keeps its existing text-column behavior; both helpers use the same text search configuration and websearch syntax.
