---
id: 32
title: "Navigation and foreign key changes"
area: tracking
status: active
tags: [foreign-key, navigation, move-child, unlink, SaveChanges]
issues: []
related: [93, 111]
changed_by: []
code: [unit.go, internal/track/track.go]
tests: [tests/features/change_tracking/relations_test.go, tests/features/change_tracking/graph_tracking_test.go]
---

# 32. Navigation and foreign key changes

**In short:** A relation holding what was loaded leaves the foreign key alone; a row moved between relations updates only its foreign key; an emptied loaded pointer unlinks.

> **Note.** Decision 111 refines the zero-foreign-key case.

## Decision

A relation that still holds what was loaded does not touch the foreign key, so `post.UserID = 5` works with `post.User` loaded. A row removed from one loaded relation and added to another is moved, not deleted, whether the other relation was loaded or not, and whether its parent is new in the same save or not. A row in a new position only updates its foreign key. A pointer to a parent that held one when last loaded or saved, and is empty now, unlinks; one that was empty leaves the foreign key to the application.
