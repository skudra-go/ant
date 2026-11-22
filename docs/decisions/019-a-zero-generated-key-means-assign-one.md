---
id: 19
title: "A zero generated key means \"assign one\""
area: models
status: active
tags: [generated-key, NotGenerated, zero-value, primary-key]
issues: []
related: [8]
changed_by: [150]
code: [config.go, unit.go, internal/meta/meta.go]
tests: [tests/features/keys_and_identity/generated_key_test.go]
---

# 19. A zero generated key means "assign one"

**In short:** A zero integer generated primary key means assign one; NotGenerated opts a model out and non-integer keys are never generated.

> **Amended.** Decision 150 refuses an insert of a zero non-integer key that nothing fills, with `ErrZeroKey`.

## Decision

Decision 8 of the brief forbids reading meaning into zero values, but an integer primary key needs a way to ask for a generated value. As EF Core does, ant treats zero in a generated key as "assign one". This is the only exception. `b.Key(&Tickets.ID).NotGenerated()` removes it for a model, and non-integer keys are never generated.
