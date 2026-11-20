---
id: 111
title: "A replaced entity keeps what the body could not give"
area: tracking
status: active
tags: [UpdatedAt, foreign-key, SaveChanges, PUT, relation, replaced-entity]
issues: [152]
related: [32, 96]
changed_by: []
code: [unit.go]
tests: [tests/features/lifecycle/timestamps_test.go, tests/features/change_tracking/relations_test.go, tests/features/change_tracking/probes_test.go]
---

# 111. A replaced entity keeps what the body could not give

**In short:** An update stamps a zero UpdatedAt, and a zero required foreign key on a child in a loaded relation is set from its parent instead of written as zero.

## Decision

A PUT handler that loads a row and overwrites it with the struct it decoded, `*loaded = input`, or puts decoded children in place of a loaded relation, `me.Posts = posts`, leaves the fields the body did not hold at their zero values (#152). `CreatedAt` and immutable fields were already safe, since an update never writes them. `UpdatedAt` was not: the save saw it changed, took it for the application's own value, and stored 0001-01-01. A forced update already left it to ant (decision 96). Now any update stamps an `UpdatedAt` that holds its zero value, and keeps one the application set to a real time, as an insert does.

The children's foreign keys were written as zero too, since a child still in its loaded relation is not linked again, so that a foreign key changed directly moves it (decision 32). A required foreign key at its zero value names no row, so it can only mean the key was not given: such a key is now set from the relation, from the parent whose slice holds the child or the loaded parent a pointer still holds, as a moved child's is. A nullable one left empty is NULL, no parent, which the application may mean, so clearing it directly still wins over a loaded parent. A child decoded with a nullable key and put in a loaded slice is unlinked as before; set its key, or put the loaded child's fields on it, to keep it. Linking every member again on each save was rejected: it would overwrite the foreign keys an application changes to move a child.
