---
id: 61
title: "Sessions take options"
area: tracking
status: amended
tags: [Session, WithTracking, SaveChanges, WithReplicas, WithLogger, savepoint, unit-of-work]
issues: [210]
related: [46, 48]
changed_by: [120]
code: [ant.go, options.go, tx.go]
tests: [tests/concurrency/sessions_test.go, tests/features/change_tracking/sessions_test.go, tests/features/transactions/replicas_test.go]
---

# 61. Sessions take options

**In short:** ant.Session accepts options, so Session(db, WithTracking(true)) is the unit of work, and logger, interceptor, diagnostics, detailed-errors and replica options on a session wrap its executors again.

> **Amended.** Decision 120 makes the context `Open` returns not track and a session track unless given `WithTracking(false)`, so `ant.Session(db)` is the unit of work, and `SaveChanges` on a context with tracking off returns `ErrNoTracking` rather than nil. The rest, that a session takes options and wraps its executors and replicas again, holds.

## Decision

`ant.Session(db)` copied a context's settings, including `WithTracking(false)`, so `SaveChanges` on such a session did nothing and returned nil. An application needs tracking off on its shared context, or it grows without end, so this is the normal setup. `Session` now takes options, and `ant.Session(db, ant.WithTracking(true))` is the unit of work. The blog example found this.

The executor that runs statements carries the interceptors and the error detail, and a session copied db's, so `WithLogger`, `WithInterceptor`, `WithDiagnostics` and `WithDetailedErrors` on a session reached only the statements of transactions it opened, which wrap the executor again, and `WithReplicas` reached none (#210). A session now wraps its executors again with its own settings, under the guard of a savepoint it was made in, so a released savepoint still refuses its statements, and wraps the replicas it adds. Refusing those options on a session was rejected: a logger or tracer per request is what they are for.
