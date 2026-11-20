---
id: 52
title: "New NOT NULL columns fill existing rows"
area: migrations
status: active
tags: [not-null, add-column, default, zero-value, gen_random_uuid, now, warning]
issues: []
related: [51]
changed_by: []
code: [schema/diff.go, dialect/postgres/ddl.go]
tests: [schema/diff_test.go, schema/probes_test.go]
---

# 52. New NOT NULL columns fill existing rows

**In short:** Diff adds a new NOT NULL column with its Go zero value as default then drops the default, using now() for timestamptz and gen_random_uuid() for uuid, warning when no sensible value exists.

## Decision

A plain `ADD COLUMN ... NOT NULL` fails on any table with rows, so a generated migration would work in development and break in production. Diff adds the column with its Go zero value as a default, then drops the default, so the column ends up as the model declares it. The value is in the file with a warning, and is easy to change.

`timestamptz` uses `now()`, and `uuid` uses `gen_random_uuid()`, because their zero values would be misleading. A type with no sensible value gets a warning instead.
