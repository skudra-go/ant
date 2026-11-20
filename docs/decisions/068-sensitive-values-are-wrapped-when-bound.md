---
id: 68
title: "Sensitive values are wrapped when bound"
area: security
status: active
tags: [Sensitive, qb.Secret, redacted, readsSensitive, logging, LogValues]
issues: [77]
related: [46, 69]
changed_by: []
code: [sensitive.go, expr.go, cond.go, qb/qb.go]
tests: [tests/features/developer_tooling/sensitive_test.go]
---

# 68. Sensitive values are wrapped when bound

**In short:** Sensitive field values are wrapped in qb.Secret as they enter a statement, and expressions that read a sensitive column are detected by readsSensitive, so logs show [redacted].

## Decision

A `.Sensitive()` field's value is wrapped in `qb.Secret` as it enters the statement, so it prints as `[redacted]` wherever it travels. Looking the column up at logging time would miss conditions and raw paths.

Only the plain condition helpers wrapped them, so a value compared with an expression over the column (`ant.Col(&f).Eq(v)`, `ant.Lower(&f).Eq(v)`), given with it to `Func` or `SQL`, picked by a `Case` that reads it, or bound by `InTuples`, a keyset seek, `UpdateMany`, or `Values`, was logged in clear (#77). Now each binds through the field, or asks whether the expression reads a sensitive column of the models it refers to (`readsSensitive`), matching a column by table and name, or by name alone when it is unqualified, which can hide a value that needed no hiding but never the reverse. A subquery hides its own values. Carrying a flag on every `Expr` was rejected: each of the many builders that compose expressions would have to pass it on.
