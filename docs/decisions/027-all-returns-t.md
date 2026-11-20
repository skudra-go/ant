---
id: 27
title: "`All` returns `[]*T`"
area: tracking
status: active
tags: [All, []*T, identity-map, Each, Raw]
issues: []
related: [6]
changed_by: []
code: [query.go, raw.go, internal/track/track.go]
tests: [tests/features/change_tracking/identity_test.go, tests/features/querying/all_test.go, tests/features/querying/each_test.go]
---

# 27. `All` returns `[]*T`

**In short:** All, First, Single and Find return pointers, enabling edits through range and an identity map; Each and Raw return untracked values.

## Decision

Decision 6 said `All` returns `[]T`. That made `for _, u := range users { u.Age++ }` silently lose the change, because `u` is a copy, and it made an identity map impossible, because one row could not be one instance. `All`, `First`, `Single`, and `Find` now all return pointers. `Each` and `Raw` still return values, and neither is tracked.
