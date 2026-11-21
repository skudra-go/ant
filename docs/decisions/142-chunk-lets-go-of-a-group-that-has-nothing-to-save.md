---
id: 142
title: "Chunk lets go of a group that has nothing to save"
area: tracking
status: active
tags: [Chunk, ChunkByKey, tracking, memory, Session, SaveChanges, ForgetAlone]
issues: [151]
related: [79, 121]
changed_by: []
code: [paging.go, internal/track/track.go]
tests: [tests/chunk_tracking_test.go, tests/features/performance/chunk_test.go]
---

# 142. Chunk lets go of a group that has nothing to save

**In short:** on a context that tracks, `Chunk` and `ChunkByKey` stop tracking what a group began tracking once its callback returns with nothing left to save, unless an entity tracked before the group holds some of it in a loaded relation.

## Decision

The docs said `Chunk` holds one group in memory at a time, but on a session every row of every group stayed tracked with its snapshot, so a job walking a large table grew without bound, and the documented pattern for jobs that change rows, `ChunkByKey` with `SaveChanges` in the callback, was exactly that case (#151).

After each callback that returns nil, ant plans a save as `HasChanges` does. If it would write nothing, the tracker forgets every entry that began after the group's read began: the group's rows, what its includes loaded, and what the callback itself loaded or added and then saved. If the callback left anything to save, nothing is let go, and the next save writes it as before, so an edit made in the callback and saved later is never lost. Entities tracked before the read stay tracked, even when a group returns them again.

An entity tracked before the group can hold the group's rows in a loaded relation, as when the group's `Include` reloads the posts of a user loaded earlier. Let go, those posts would look taken out of the relation, and the next save missed a post the application did take out. So a group held that way is not let go. A callback that returns an error also keeps its group, as the read stops there.

Rejected:

- Only fixing the sentence in the docs, and telling jobs to clear the tracker per group: the documented pattern would still grow, and `ClearTracked` also forgets what the job tracked before the read.
- Letting go of each unchanged entity on its own: an entity with no change of its own can still be needed by another's pending save, through a relation or a foreign key, and the plan says only whether the save writes anything.

What changes for an application: an entity from a group that was let go is no longer tracked, so editing it after `Chunk` returns and then calling `SaveChanges` saves nothing. Edit a group's entities, and save them, inside the callback.
