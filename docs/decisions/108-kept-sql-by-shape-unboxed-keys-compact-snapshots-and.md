---
id: 108
title: "Kept SQL by shape, unboxed keys, compact snapshots, and moved elements followed"
area: performance
status: active
tags: [shape-cache, IntKey, Snapshot, reconcile, adopt, Static, Include, identity-map]
issues: [138]
related: [107]
changed_by: []
code: [shape.go, internal/meta/meta.go, internal/track/track.go, unit.go]
tests: [shape_internal_test.go, tests/benchmarks/budget_test.go, tests/features/change_tracking/probes_test.go]
---

# 108. Kept SQL by shape, unboxed keys, compact snapshots, and moved elements followed

**In short:** Simple Find/All/First/Single and one-entity INSERT SQL is cached per shape, integer keys are unboxed, snapshots are compact, and moved relation elements are followed by key.

## Decision

Four follow-ups to 107 (#138).

**Statements by shape.** A Find, an All, First or Single with up to six plain conditions on the queried table (a comparison, IS NULL, BETWEEN) and up to three orderings on its columns, and the INSERT of one entity, have the same SQL every time they run with the same shape. Their SQL is kept, for each context (`sharedState`), in a copy-on-write map keyed by a struct of the shape: the model, each condition's column and operator, each ordering, whether a row count and an offset are given, and which static filters apply. A shape is learned by rendering its statement once with markers in place of its values, so the order the dialect binds them in (SQL Server binds OFFSET before the row count) comes from the dialect, not from ant. The values of a later run are converted with `dialect.Arg`, which is what Render does to a value it binds. Anything else goes the usual way: joins, OR, IN, raw SQL, chosen columns, a comment, a hint, a lock, Debug, a derived source, and a global filter that reads the context. `meta.Filter.Static` marks a filter whose condition does not depend on the context and binds nothing; only soft delete's is. A test renders each shape both ways on MySQL, MariaDB, SQLite, SQL Server and a numbered-placeholder dialect and requires the same text and values. At most 1024 shapes are kept for each context.

**Integer keys unboxed.** The tracker's identity holds an integer key as an `int64`, with no interface, and reads it with no reflection (`meta.Model.IntKey`), so tracking a row allocates nothing beyond the row itself.

**Compact snapshots.** A snapshot has a layout of its own (`meta.Snapshot`, made with `reflect.StructOf`): every column but the computed ones, packed, with no relations. Every other column stays, because `Original`, the diff of a write-only column and the rollback of a reload read them. So the gain is the relations and computed columns of a model, not half of it. The tracker reads a loaded value through `Entry.OriginalValue` and `Entry.OriginalKey`, and `Scratch` builds an entity from a snapshot when a whole one is needed.

**Moved elements followed.** A tracked element of a relation slice is known by its address. When an append moves the slice, `unit.reconcile` matches the elements of a parent whose members were recorded at load by key. A parent with no record, as one detached and attached again, lost the edits to its moved elements: the entries compared their old memory, which nothing changes, with their snapshots. `unit.adopt` now does the same for those: an element of a tracked parent's relation that no entry holds, whose row a tracked element held at an address that no tracked parent's relation holds any longer, is that element moved, and its entry goes with it. A row left behind twice is not guessed at. This is what lets tracked Includes have slices with no spare room.
