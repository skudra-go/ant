---
id: 93
title: "Tracking follows what a save wrote"
area: tracking
status: active
tags: [change-tracking, relations, many-to-many, Include, SaveChanges, InsertOnly, Remove, version]
issues: []
related: []
changed_by: []
code: [unit.go, entry.go, internal/track/track.go, include.go, graph.go]
tests: [tests/features/change_tracking/probes_test.go, tests/features/change_tracking/graph_tracking_test.go, tests/features/change_tracking/relations_test.go, tests/features/change_tracking/states_test.go]
---

# 93. Tracking follows what a save wrote

**In short:** Change tracking records relations a save wrote, so later saves do not rewrite join rows and removed, moved or replaced children are handled correctly.

## Decision

Several gaps in change tracking came from relations being remembered only when loaded:

- An entity inserted with its relations is tracked as if loaded with them, as EF Core does, so a later save does not write its join rows again, and a child removed from its slice is deleted. `InsertOnly`, which ignores relations, records none.
- A pointer to a parent is remembered once a save writes it, and remembered empty once cleared, so setting it again and clearing it is seen.
- A child moved to a parent added in the same save, or to one whose relation was never loaded, is moved, not deleted.
- An element of a many-to-many slice set to another target unlinks the old one and lets go of its entry, as the element's memory now holds the new one.
- Relations loaded below a slice element, by a nested `Include`, are recorded on the element's entry.
- A row put back into a loaded relation after this context deleted it is written, so the save fails rather than doing nothing, since only the database knows the row is gone.
- `Remove` of an entity that was added and never saved cancels its insert.
- Two instances of one versioned row written in one save carry the version forward from one to the other.

A later `Include` reads a tracked parent's relation afresh, as `Load` does, and lets go of the entries of the elements it replaced, so what the slice shows is what a save writes. Keeping pending relation changes through a query was tried, and rejected: it skipped the query's nested includes, and hid rows other writers added. Save first to keep a pending change. Replacing children with new ones under the same unique values still fails with a unique violation and changes nothing: inserts run before deletes, so a child can move to a parent saved in the same unit, and a constraint the model does not declare is not ant's to see. Saving the removal first works.
