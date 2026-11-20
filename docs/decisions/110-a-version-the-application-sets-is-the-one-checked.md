---
id: 110
title: "A version the application sets is the one checked"
area: tracking
status: active
tags: [version, optimistic-concurrency, ErrConcurrency, SaveChanges, ConcurrencyError, Attach]
issues: [143]
related: []
changed_by: []
code: [unit.go, entry.go, errors.go]
tests: [tests/features/lifecycle/concurrency_test.go, tests/conformance_test.go]
---

# 110. A version the application sets is the one checked

**In short:** A tracked save checks the version the entity holds when the application changed it, so a stale or ahead version is ErrConcurrency.

## Decision

The usual optimistic concurrency over HTTP sends the version to the client, and on a PUT loads the row, applies the body with the version in it, and saves. `SaveChanges` checked the version in the snapshot, the one it loaded, and moved the version on from the entity's, so a stale client's version passed the check, and the stored version moved back to the client's plus one (#143). `Attach` and `MarkModified` already checked the entity's own version. Now a tracked save does too, when the application changed it: an update or delete checks the version the entity holds, so a stale one is `ErrConcurrency`, and one ahead of the row is as well. An unchanged version is checked as before, with what an earlier write of the same save gave the row. Other tokens are the application's to move, so a changed one is still checked against its loaded value and written as the new one. Refusing an edited version outright was rejected: copying the client's version in is how the pattern is written.
