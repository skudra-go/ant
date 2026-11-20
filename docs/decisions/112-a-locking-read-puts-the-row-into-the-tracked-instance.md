---
id: 112
title: "A locking read puts the row into the tracked instance"
area: queries
status: active
tags: [ForUpdate, ForShare, Reload, identity-map, refreshLocked, ConcurrencyError]
issues: [223]
related: [28]
changed_by: []
code: [query.go, unit.go]
tests: [tests/features/querying/locks_test.go, tests/conformance_test.go, tests/concurrency/sessions_test.go]
---

# 112. A locking read puts the row into the tracked instance

**In short:** ForUpdate and ForShare reads refresh the already tracked instance like Reload, keeping unsaved edits only while the row is unchanged, else raising ConcurrencyError.

## Decision

A context holds one instance per row, so a tracked query hands back the instance it already holds and drops the row it read (decision 28). That included `ForUpdate` and `ForShare`: the lock was taken, but the caller got the values of the earlier read, so "read under a lock, change, save" wrote a value computed from stale data, and a checkpoint went backwards (#223). Now a locking read puts each row it returns into the instance already tracked for it, as `Reload` does: the values, and the snapshot and state, with the same reversal if the transaction rolls back, since the row may hold the transaction's own writes. The instance stays the one the context holds, so other code that holds it sees the row too. An instance with unsaved changes, edits or a removal, keeps them while its row is still as it was loaded, as when an edit is made before the lock is taken, and is a `ConcurrencyError` once the row moved, since those changes were made to values the row no longer holds; overwriting them would lose them, and keeping them over the new row would hide the conflict the lock is there to catch. An entity added, or marked modified, holds what the application gave it to write rather than values it loaded, so it is left as it is. A read with no lock still returns the instance as it is, and the rows of a joined include are not locked, so they are not refreshed either. Returning a second, fresh instance was rejected: the context would then hold two instances of one row, and a change to the one it does not track would not be saved.
