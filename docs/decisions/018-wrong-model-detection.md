---
id: 18
title: "Wrong-model detection"
area: queries
status: active
tags: [ErrWrongModel, probes, Where, terminal-call]
issues: []
related: [4, 37]
changed_by: []
code: [builder.go, query.go, errors.go]
tests: [ant_test.go, tests/features/querying/order_by_test.go, tests/features/querying/eq_col_test.go]
---

# 18. Wrong-model detection

**In short:** Probes in Where, ordering, relations and assignments are checked against the query's models, and a mismatch is ErrWrongModel at the terminal call.

## Decision

`Where`, ordering, relations, and assignments compare each probe's model with the models in the query. A mismatch is `ErrWrongModel` at the terminal call.
