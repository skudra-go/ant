---
id: 148
title: "A query that fills computed fields stays tracked"
area: tracking
status: active
tags: [With, computed, tracking, identity-map, Session, SaveChanges]
issues: [295]
related: [6, 112]
changed_by: []
code: [query.go, joined.go]
tests: [tests/with_tracking_test.go]
---

# 148. A query that fills computed fields stays tracked

**In short:** Rows that `Query.With` filled computed fields on are tracked like any other rows. The tracker never compares or writes a computed field, and an entity the context already holds gets the new value and keeps its unsaved changes.

## Decision

`With` made a query a shaped one, like `Column` or `Omit`, so its rows weren't tracked. A page that loads a rider with a count of rides and lets the user rename them lost the rename: `HasChanges` said false and `SaveChanges` wrote nothing, with no error (#295). Nothing listed `With` among the reads that aren't tracked.

A `With` query reads every stored column, so its row is whole. Computed fields are already left out of the tracker's snapshot and of every write, so tracking the row compares and saves only the columns. `Column`, `Omit` and derived sources (`From`, `FromRaw`, set operations) stay untracked, because their rows are partial or may match no stored row.

The identity map returns the instance a context already holds for a row. A `With` query copies its computed values onto that instance, so the caller sees the count it asked for, and the instance keeps any unsaved edits. A plain query afterwards leaves the computed value as it is, since it doesn't read it. A joined read (`IncludeJoined`) does the same.

`With` on a field that isn't computed is not a case to handle: its alias repeats a column's name, and the read fails with an error that says so.

Rejected: adding `With` to the documented list of untracked reads. Nothing about a computed value makes the row's columns unsafe to track, and the silent lost edit is the failure the list would only describe.
