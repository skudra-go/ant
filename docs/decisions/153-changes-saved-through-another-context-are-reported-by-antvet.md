---
id: 153
title: "Changes saved through another context are reported by antvet"
area: tracking
status: active
tags: [SaveChanges, Session, Attach, tracker, antvet, HasChanges]
issues: [153]
related: [29, 83, 120]
changed_by: []
code: [tools/antvet/analyzer/sessions.go]
tests: [tools/antvet/analyzer/testdata/src/example/sessions.go]
---

# 153. Changes saved through another context are reported by antvet

**In short:** An entity loaded through one context and saved with another context's `SaveChanges` is not saved, and `SaveChanges` returns nil (#153). antvet reports it when the two contexts surely have trackers of their own: a session and the context it was made from, or two sessions. `Attach` does not refuse an entity another tracker holds, because no tracker can tell whether another is still in use. `HasChanges` tells a session with nothing to save from one with changes.

## Decision

The common form is one identifier: `u, _ := db.Users.Find(ctx, id)` through the root, which is in scope everywhere, then `s.SaveChanges(ctx)` on the session. The root does not track, so nothing holds `u`'s snapshot, and nothing is written. Two sessions fail the same way.

Refusing this at run time needs ant to know, when a session saves or attaches, that another tracker holds the entity and is still in use. A session has no end: it is dropped, not closed. A registry of trackers could ask only whether the garbage collector has collected one yet, so an `Attach` of an entity loaded by a finished request's session would fail or not depending on when the collector last ran. A registry of every tracked entity would also cost allocations on every load. So `Attach` keeps taking an entity another tracker holds, as it did.

The mistake is visible in the source, so antvet checks it. Within one function body, in the order of the source, it follows an entity loaded with `Find`, `First`, `Single` or `Last` through a context variable, a change to one of its fields, and a `SaveChanges` on a second context variable. It reports the save when the two contexts differ for certain: one is `ant.Session` of the other, or both come from `ant.Session`. A transaction's context shares its parent's tracker, and two parameters may be one context, so those are not compared. An entity passed anywhere, as an argument, a value or a result, may be attached or saved there, and is left alone. So is one whose own context saves it later, as a root that tracks does.

Rejected: making `SaveChanges` fail when nothing changed. A save with nothing to do is ordinary, as after a request that changed nothing, and `HasChanges` already answers the question for code that wants to know.
