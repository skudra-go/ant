---
id: 64
title: "Through relations are read-only pivots"
area: relations
status: active
tags: [HasManyThrough, HasOneThrough, read-only, pivot, many-to-many]
issues: []
related: [44]
changed_by: []
code: [config.go, pivot.go, internal/meta/meta.go]
tests: [tests/features/relations/advanced_test.go, tests/conformance_test.go]
---

# 64. Through relations are read-only pivots

**In short:** HasManyThrough reuses the many-to-many loader with a join on the intermediate table and is marked read-only, so writing through it is not supported.

## Decision

`HasManyThrough` reuses the many-to-many loader with a join on the intermediate table, marked read-only, because writing through it has no one obvious meaning.
