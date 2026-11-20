---
id: 61
title: "Sessions take options"
area: tracking
status: active
tags: [Session, WithTracking, SaveChanges, WithReplicas, WithLogger, savepoint, unit-of-work]
issues: [210]
related: [46, 48]
changed_by: []
code: [ant.go, options.go, tx.go]
tests: [tests/concurrency/sessions_test.go, tests/features/change_tracking/sessions_test.go, tests/features/transactions/replicas_test.go]
---

# 61. Sessions take options

**In short:** ant.Session accepts options, so Session(db, WithTracking(true)) is the unit of work, and logger, interceptor, diagnostics, detailed-errors and replica options on a session wrap its executors again.

> **Note.** Pull request #276 would change it, in decision 120, which is pending.

## Decision

`ant.Session(db)` copied a context's settings, including `WithTracking(false)`, so `SaveChanges` on such a session did nothing and returned nil. An application needs tracking off on its shared context, or it grows without end, so this is the normal setup. `Session` now takes options, and `ant.Session(db, ant.WithTracking(true))` is the unit of work. The blog example found this.

The executor that runs statements carries the interceptors and the error detail, and a session copied db's, so `WithLogger`, `WithInterceptor`, `WithDiagnostics` and `WithDetailedErrors` on a session reached only the statements of transactions it opened, which wrap the executor again, and `WithReplicas` reached none (#210). A session now wraps its executors again with its own settings, under the guard of a savepoint it was made in, so a released savepoint still refuses its statements, and wraps the replicas it adds. Refusing those options on a session was rejected: a logger or tracer per request is what they are for.
