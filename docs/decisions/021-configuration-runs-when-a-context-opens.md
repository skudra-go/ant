---
id: 21
title: "Configuration runs when a context opens"
area: models
status: active
tags: [Configure, Open, Fields, probes, table-name]
issues: []
related: []
changed_by: []
code: [config.go, ant.go, schema.go]
tests: [tests/features/model_definition/configure_test.go]
---

# 21. Configuration runs when a context opens

**In short:** Configure runs once when a context opens, not inside ant.Fields, and the table name is shared by pointer so earlier conditions render the configured name.

## Decision

`Configure` cannot run inside `ant.Fields`, because it refers to probes that may not be initialized yet. It runs once, from `Open`, on first query, or from `Raw`. Errors in it stop `Open`.

AST nodes share a pointer to the table name, so a condition built before `Open` still renders the configured table.
