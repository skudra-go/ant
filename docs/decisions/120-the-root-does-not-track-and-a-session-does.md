---
id: 120
title: "The root does not track, and a session does"
area: tracking
status: pending
pr: 276
tags: [WithTracking, Session, ErrNoTracking, root-context, unit-of-work]
issues: [147, 148]
related: [29, 61, 79]
changes: [29, 61, 79]
code: [ant.go, options.go, entry.go, graph.go, unit.go, errors.go]
tests: [tests/features/change_tracking/sessions_test.go, tests/features/change_tracking/concurrent_use_test.go]
---

# 120. The root does not track, and a session does

**In short:** The context Open returns does not track and a Session does; SaveChanges and the helpers that give it work return ErrNoTracking on a context that does not track.

> **Pending.** Added by pull request #276, not merged yet. When it merges, set `status: active`, and mark 29, 61, 79 as amended by 120.

## Decision

`ant.Open` returned a context that tracked, and the quick start saved through it, so the natural first server used one tracker for every request: with requests one at a time nothing failed, but a read returned the instance another request had edited and not saved, a rejected edit was saved by the next request's `SaveChanges`, one `ErrConcurrency` stayed in the shared tracker and failed every later save, and the tracker grew with every row read (#147). The fix decision 61 chose, `WithTracking(false)` on the root and `ant.Session(db, ant.WithTracking(true))` per request, left the session that most code writes, `ant.Session(db)`, with tracking off, where `SaveChanges` saved nothing and returned nil (#148). Now the context `Open` and `Wrap` return does not track, a session tracks unless given `WithTracking(false)`, and on a context with tracking off `SaveChanges`, `Add`, `Attach`, `MarkModified`, `Remove` and `TrackGraph` return `ErrNoTracking` rather than doing nothing, so code written for a tracking root fails at once with an error that names `ant.Session`. `WithTracking(true)` on `Open` still makes a root that tracks, for a script or a test that does one thing at a time. A context with tracking off therefore never holds an entity, so the exception decision 79 made for `Add` and `Attach` on such a context, and the claims it needed, are gone. Keeping the root tracking and only warning was rejected, since the failures above are silent; refusing `SaveChanges` on a tracking root was rejected, since it would leave reads on the root stale.
