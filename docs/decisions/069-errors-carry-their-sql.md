---
id: 69
title: "Errors carry their SQL"
area: errors
status: active
tags: [FailedSQL, WithDetailedErrors, exec.Error, error-message, placeholders]
issues: []
related: [45, 46]
changed_by: []
code: [errors_kind.go, internal/exec/exec.go, options.go]
tests: [tests/features/developer_tooling/diagnostics_test.go, tests/features/developer_tooling/errors_test.go]
---

# 69. Errors carry their SQL

**In short:** exec.Error holds the statement with placeholders, exposed via ant.FailedSQL, and it only enters the message when WithDetailedErrors is set.

## Decision

`exec.Error` holds the statement, with placeholders and no values, so `ant.FailedSQL` can log it. It stays out of the message unless `WithDetailedErrors` is set, because messages are what reach users.
