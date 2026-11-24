---
id: 166
title: "Find reads the row of an instance tracked before a bulk delete"
area: tracking
status: active
tags: [Find, Delete, ForceDelete, DeleteReturning, Prune, Merge, identity map, tracking]
issues: [154]
related: [6]
changed_by: []
code: [table.go, query.go, merge.go, internal/track/track.go]
tests: [tests/bulk_delete_tracking_test.go]
---

# 166. Find reads the row of an instance tracked before a bulk delete

**In short:** After `Delete`, `ForceDelete`, `DeleteReturning`, `Prune` or a deleting `Merge` removes rows of a model, `Table.Find` stops answering from the instances of that model tracked so far and reads the row, so a deleted row is `ErrNotFound`. The instances stay tracked, as `Delete` documents.

## Decision

`Find` returns a tracked instance without a query. `DeleteByID` forgets the instance of the row it deletes, but a bulk delete does not know which rows it removed, so `Find` went on returning a row the same context had just deleted, while `All` returned none (#154).

The tracker records, per model, its sequence number at the last bulk delete that removed a row. An instance tracked by then may be of a removed row, so `Find` reads the row for it, and gets the same tracked instance back if the row is still there. An instance tracked after the delete is answered from the map as before. `DeleteByID` names its row and forgets it, so it records nothing. A rollback of the delete leaves the record, which costs a query and not a wrong answer.

Rejected:

- Forgetting every tracked instance of the model: an instance with unsaved changes would lose them without a word, and the rows that were not deleted would get new instances on the next read.
- Reading the deleted keys first, or with `RETURNING`, to forget only those rows: a second statement, or a result set, on every bulk delete, and MySQL has no `DELETE ... RETURNING`.
- Recording only `Delete`: `ForceDelete`, `DeleteReturning`, `Prune` and `Merge` remove rows the same way.

A `Raw` statement that deletes rows is not seen, and neither is a delete through another context, whose instances are its own.
