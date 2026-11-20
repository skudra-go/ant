---
id: 14
title: "The `Dialect` interface exposes an internal type"
area: dialects
status: superseded
tags: [qb, Dialect, Render, public-ast, third-party-dialect]
issues: []
related: [62]
changed_by: [62]
code: [qb/qb.go, dialect/dialect.go]
tests: [qb/aliased_test.go]
---

# 14. The `Dialect` interface exposes an internal type

**In short:** The Dialect interface renders qb.Statement from a package that is now public, so third-party dialects can implement it.

> **Superseded.** Decision 62 moved internal/qb to public qb; ROADMAP v0.4 marks it done.

## Decision

`Render` takes a `qb.Statement`, and the brief places `qb` under `internal`. The interface is pluggable inside this module, but a third-party module cannot implement it. The roadmap makes the AST public in v0.4.
