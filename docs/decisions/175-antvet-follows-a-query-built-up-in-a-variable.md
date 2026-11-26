---
id: 175
title: "antvet follows a query built up in a variable"
area: tooling
status: active
tags: [antvet, analyzer, subquery, variable]
issues: [220]
related: [37, 83]
changed_by: []
code: [tools/antvet/analyzer/analyzer.go]
tests: [tools/antvet/analyzer/testdata/src/example/variables.go]
---

# 175. antvet follows a query built up in a variable

**In short:** A query held in a variable of the function is judged as its chain would be, when every use of the variable runs the query or builds it up into another variable used the same way. Any other use leaves it alone, as before.

## Decision

A filter endpoint builds its query in steps, `q := db.Issues.Where(...)`, then `q = q.Where(...)` under an `if`, then `q.All(ctx)`. Decision 83 ended the outer queries at a statement, so antvet said nothing about a condition on another model there, and the query failed at run time (#220). Within one function the analyzer sees every use of a variable it declares, so it can tell where the query goes.

- The variable's type fixes the models its query reads: `*Query[T]` reads T, and a join changes the type to `Join2` and on, which carries the joined models.
- Each use is judged where it stands. One that runs the query with no join to come, as `q.All(ctx)` or `ant.SumOf(ctx, q, ...)` does, is fine, and so is one in a chain assigned to a variable that is followed in turn, `q` itself included. Being assigned to is no use. Closures count like any other code, and the order of the uses does not matter, as for probes.
- Any other use may put the query where conditions on other models are valid, so the variable is not judged at all: a subquery such as `ant.Exists(q)`, a join to come, an argument to a function of one's own, a return, a field, or its address.
- Only variables declared in the function are followed. A parameter, a named result, which a bare `return` hands back, and a variable of the package hold what code elsewhere gives or takes.

Rejected:

- Judging every query by its type alone: a query held in a variable and used as a subquery may name its outer query's models, as decision 37 allows, so that would report valid code.
- Following a query across functions: that is the data flow decision 83 left out, for a check that has to stay free of false reports.
