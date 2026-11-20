---
id: 105
title: "Read-mostly registries are copy-on-write snapshots"
area: performance
status: active
tags: [atomic.Pointer, copy-on-write, registry, keyring, Observe, contention, lock-free]
issues: [135]
related: [101]
changed_by: []
code: [internal/meta/meta.go, secure/secure.go, hooks.go]
tests: [tests/benchmarks/parallel_test.go, tests/concurrency/callbacks_test.go, internal/meta/meta_test.go]
---

# 105. Read-mostly registries are copy-on-write snapshots

**In short:** The model registry, probe list, secure keyring and observers are immutable values behind atomic.Pointer, read lock-free and replaced by copying under a writer mutex.

## Decision

The model registry, the probe list, the `secure` keyring and a context's observers change a handful of times, mostly at startup, but are read on every query, every encrypted value and every write. Each read took a shared `sync.RWMutex`. Read locks do not block one another, but each takes and releases a counter on one cache line that every core then fights over: resolving a field pointer took 19 ns on one core and 26 ns per call on twelve, less total throughput than one core (#135). `notify` also called observers while it held its read lock, so an observer that called `Observe` deadlocked.

Decision: each is an immutable value behind an `atomic.Pointer`. A reader loads it and takes no lock. A writer copies it, changes the copy and stores it, under a plain mutex where two writers could otherwise lose each other's change (the registry, which also guards the `Pinner` of decision 101, and the observers); `SetKeys` replaces the whole keyring, so it needs none. Copying costs a write O(n), once per model type or registration. Resolving a field pointer now takes 2.8 ns per call on twelve cores. `sync.Map` was rejected: `Resolve` scans the probe list by address range, which a map cannot index, and one mechanism for all four is easier to check. The naming conventions (read once per model) and `dialect.StateOf` (read only on errors) keep their locks, since they are off the hot path.

`tests/benchmarks/parallel_test.go` runs resolution, query building and a read from every core, so contention shows in the numbers. Those runs also showed that building a query scales poorly past four cores because of allocation and GC, not locks; that is separate work.
