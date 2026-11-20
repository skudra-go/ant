---
id: 123
title: "An After hook's edit is a change still to save"
area: hooks
status: active
tags: [hooks, AfterInsert, AfterUpdate, snapshot, SaveChanges, Upsert]
issues: [108]
related: [47, 93]
changed_by: []
code: [hooks.go, unit.go, upsert.go, internal/track/track.go]
tests: [tests/features/lifecycle/hooks_test.go]
---
# 123. An After hook's edit is a change still to save

**In short:** The tracker takes as stored the values a write sent, copied before AfterInsert or AfterUpdate runs, so what such a hook sets on its entity is saved by the next SaveChanges.

## Decision

The tracker took its snapshot from the entity after the After hooks ran, so a hook that set a field, such as a slug built from the new key, made the tracker believe the row held it: `HasChanges` was false, and the value never reached the database (#108). Now the values the write sent are copied before an After hook runs, and the tracker takes that copy as stored, for `Insert`, `SaveChanges` and `Upsert` alike. A hook's edit is then a change like any other, which the next `SaveChanges` writes. Writing it in the same save was rejected: it would run the write twice, and run the hooks again, with no end for a hook that always changes its entity. Documenting that After hooks must not change their entity was rejected as a trap. The copy is made only for a model with such a hook, and only when hooks run.
