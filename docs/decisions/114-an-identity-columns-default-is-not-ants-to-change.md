---
id: 114
title: "An identity column's default is not ant's to change"
area: migrations
status: active
tags: [identity, serial, nextval, diff, DROP-DEFAULT, drift, postgres]
issues: [225]
related: []
changed_by: []
code: [schema/diff.go, dialect/postgres/inspect.go, dialect/postgres/ddl.go]
tests: [schema/identity_test.go, tests/features/migrations/diff_test.go, tests/features/migrations/drift_test.go]
---

# 114. An identity column's default is not ant's to change

**In short:** When a column is an identity on both sides, diff ignores a difference in its default, so a serial key's nextval default is never dropped or set.

## Decision

ant generates a key either as an identity or through a serial's `nextval` default, and inspect reads both as an identity, so `drift` accepts a model of a serial key with or without `.Default("nextval(...)")`, which the adoption guide asks for. But `diff` compared that default like any other: removing it from the model, since it looks redundant, wrote `DROP DEFAULT`, and every insert then failed on the NULL key, and adding it wrote a `SET DEFAULT` that PostgreSQL refuses on an identity (#225). Now, when a column is an identity on both sides, a difference in its default is no change, and a retype leaves the serial's default alone. A column that is not an identity keeps its default as the model says. Refusing such a default in the model was rejected: models written as the adoption guide says would stop opening.
