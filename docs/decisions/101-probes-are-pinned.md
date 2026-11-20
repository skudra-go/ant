---
id: 101
title: "Probes are pinned"
area: models
status: active
tags: [probe, Pinner, runtime.Pinner, gc, Resolve, field-pointer]
issues: []
related: [105]
changed_by: []
code: [internal/meta/meta.go]
tests: [internal/meta/meta_test.go]
---

# 101. Probes are pinned

**In short:** Probe pins each model's probe with runtime.Pinner under the registry lock, never unpinning, so field-pointer address resolution stays valid under a moving collector.

## Decision

A field pointer resolves to its column by its address inside the model's probe, so the probe must not move for the life of the process. That held only because Go's collector does not move heap objects today, which the language does not promise.

Decision: `Probe` pins each probe with `runtime.Pinner` when it allocates it, under the registry's lock, since a `Pinner` is not safe for concurrent use. The probe's address now stays fixed even under a moving collector, and the pin tells the current collector about the pointer arithmetic ant does on it. The `Pinner` is a package variable and is never unpinned: probes live as long as the process, and the runtime panics if a `Pinner` that still holds pinned objects becomes unreachable. Pinning happens once per model type, so resolving a field pointer costs the same as before. `TestProbeSurvivesGC` still checks that a probe's address survives collections.
