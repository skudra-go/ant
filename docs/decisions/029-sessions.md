---
id: 29
title: "Sessions"
area: tracking
status: amended
tags: [Session, WithTracking, tracker, unit-of-work, DbContext]
issues: []
related: [61, 79]
changed_by: [61, 79, 120]
code: [ant.go, options.go]
tests: [tests/features/change_tracking/sessions_test.go, tests/concurrency/sessions_test.go]
---

# 29. Sessions

**In short:** ant.Session returns a context with its own tracker on the same pool, as the unit of work; WithTracking(false) suits shared contexts.

> **Amended.** Decision 61 makes Session take options (Session(db, WithTracking(true))) and decision 79 limits a tracking context to one operation at a time. Decision 120 makes the context `Open` returns not track and a session track unless given `WithTracking(false)`, so `ant.Session(db)` is the unit of work and a shared root needs no `WithTracking(false)`.

## Decision

Applications usually keep one `db` for the life of the process, and a tracker on it would grow forever and mix up concurrent requests. `ant.Session(db)` returns a context with its own tracker on the same pool, which is the Go form of EF Core's short-lived `DbContext`. Use one per unit of work, or `ant.WithTracking(false)` on contexts that only use explicit updates.
