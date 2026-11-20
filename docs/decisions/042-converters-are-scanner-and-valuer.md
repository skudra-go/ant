---
id: 42
title: "Converters are Scanner and Valuer"
area: values
status: active
tags: [Scanner, valuer, JSON, Array, converter, custom-types]
issues: []
related: []
changed_by: []
code: [json.go, dialect/postgres/functions.go]
tests: [tests/features/model_definition/custom_types_test.go, tests/features/value_handling/json_test.go, json_test.go]
---

# 42. Converters are Scanner and Valuer

**In short:** Value conversion uses sql.Scanner and driver.Valuer on named types, with ant.JSON[T] and postgres.Array[T] built that way; no Convert API exists.

## Decision

The roadmap planned `Convert(to, from)` functions. Go's `sql.Scanner` and `driver.Valuer` already do this, work in conditions without extra machinery, and need no ant API, so named types and those two interfaces are the mechanism. `ant.JSON[T]` and `postgres.Array[T]` are built that way.
