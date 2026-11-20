---
id: 90
title: "Merge keeps what other writes keep"
area: writes
status: active
tags: [Merge, created-at, updated-at, version, Discriminator, generated-key]
issues: []
related: [80, 89]
changed_by: []
code: [merge.go]
tests: [tests/features/writing/merge_test.go, tests/features/lifecycle/timestamps_test.go]
---

# 90. Merge keeps what other writes keep

**In short:** Merge fills UpdatedAt on match and CreatedAt, UpdatedAt, version and discriminator on insert unless mapped, but not Go-generated keys.

## Decision

`Merge` builds its statement from what it is given, so it left out what `Insert` and `Update` fill in. Now a matched update sets `UpdatedAt`, and an insert sets `CreatedAt`, `UpdatedAt`, the version, and the model's discriminator, unless a mapping gives the column. A key from a Go generator is not filled: one statement inserts many rows, and a value made once would be the same for all of them.
