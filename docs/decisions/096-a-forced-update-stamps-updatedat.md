---
id: 96
title: "A forced update stamps UpdatedAt"
area: tracking
status: active
tags: [MarkModified, TrackGraph, UpdatedAt, forced-update, SaveChanges]
issues: [60]
related: [89, 111]
changed_by: []
code: [unit.go, entry.go]
tests: [tests/features/change_tracking/states_test.go, tests/features/lifecycle/timestamps_test.go, tests/conformance_test.go]
---

# 96. A forced update stamps UpdatedAt

**In short:** MarkModified and TrackGraph Modified leave UpdatedAt to ant, which stamps it as on any other update, instead of saving the struct's zero value.

## Decision

`MarkModified`, and `TrackGraph` deciding `Modified`, write every updatable field, since there is no snapshot to compare with. `UpdatedAt` was one of them, so it counted as set by the application and was saved as the struct held it: the zero time, for a struct decoded from a request body (#60). A forced update now leaves `UpdatedAt` to ant, which stamps it as on any other update. An application that wants its own value there writes it with a bulk `Update`, where the one the write gives wins (decision 89).
