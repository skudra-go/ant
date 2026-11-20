---
id: 73
title: "Drift compares what models declare"
area: migrations
status: active
tags: [drift, enum, sequence, tables, extension]
issues: []
related: [57, 72]
changed_by: []
code: [dialect/postgres/inspect.go, migrate/operate.go, schema/diff.go]
tests: [tests/features/migrations/drift_test.go]
---

# 73. Drift compares what models declare

**In short:** Drift reports enums and sequences only when a model declares them, but reports any table no model describes.

## Decision

`drift` reports enums and sequences only if a model declares them, so a type created by hand, or by an extension, is not reported for removal. Tables work the other way, because a table no model describes is what drift exists to find.
