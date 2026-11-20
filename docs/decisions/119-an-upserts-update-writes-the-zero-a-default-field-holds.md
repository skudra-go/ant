---
id: 119
title: "An upsert's update writes the zero a default field holds"
area: writes
status: pending
pr: 275
tags: [upsert, default, InsertMany, WithConflict, drift, zero-value]
issues: [142, 192]
related: [90, 103]
changes: []
code: [upsert.go, migrate/operate.go, config.go]
tests: [tests/conformance_test.go, tests/features/migrations/drift_test.go]
---

# 119. An upsert's update writes the zero a default field holds

**In short:** An upsert's update writes the entity's value where the insert left a default field's zero to the database; drift notes that a model default sends zero as DEFAULT.

> **Pending.** Added by pull request #275, not merged yet. When it merges, set `status: active`.

## Decision

A zero field with a default is left to the database on insert, so `Upsert` and `InsertMany` with `WithConflict` sent it as `DEFAULT`, and their update branch then copied the row the insert offered, `excluded`, which held the database's default: a sync job that upserted `Active: false` set the user back to active, and `Score: 0` became 10 (#142). Now the update writes what the entity holds wherever the insert left a column to its default, zero or not, as `Update` and `SaveChanges` write a zero. Only the insert branch takes the default. `InsertMany` with a conflict that updates splits its rows by which such columns they leave zero, so each statement's update can bind the zero its rows hold. `Upsert` builds the clause from the entity after its `BeforeInsert` hook and stamps, as the insert is built, so both leave the same columns to the database. A zero time with a default such as `now()` is written as the zero time on the update branch, as `Update` writes it; name the columns to overwrite with `DoUpdate`, or assign them with `Set`, to keep or compute them instead. Keeping the stored value for such a column was rejected: an upsert that syncs `false` must be able to turn a flag off.

The same rule, that a default in a model sends a zero as `DEFAULT`, made an adopted `active boolean NOT NULL DEFAULT true` impossible to insert as `false` once the model declared the default `drift` asked for (#192). A default that only describes the schema would be new API, so for now `drift` follows a default the database has and the models lack with a note: such a bool or number field is a pointer with `.NotNull()` and `.Default(...)`, where nil takes the default. The `Default` and tag docs and the adoption prompt say the same.
