---
id: 45
title: "Errors are classified, not wrapped"
area: errors
status: active
tags: [IsUniqueViolation, SQLState, IsForeignKeyViolation, error-classification]
issues: []
related: [69, 74]
changed_by: []
code: [errors_kind.go, dialect/syntax.go]
tests: [tests/features/developer_tooling/errors_test.go]
---

# 45. Errors are classified, not wrapped

**In short:** IsUniqueViolation and siblings read SQLSTATE from the error chain instead of ant wrapping driver errors in sentinels, leaving the driver error intact.

## Decision

Roadmap v0.4 planned sentinels such as `ErrUniqueViolation`, which would need every error path to wrap driver errors, including those that only appear while reading rows. Instead `ant.IsUniqueViolation(err)` and its siblings read the SQLSTATE from the error chain, so nothing can be missed, and the driver's own error stays intact.
