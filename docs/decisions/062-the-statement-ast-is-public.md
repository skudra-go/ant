---
id: 62
title: "The statement AST is public"
area: tooling
status: active
tags: [qb, AST, Dialect, public-package, stability]
issues: []
related: [74]
changed_by: []
code: [qb/qb.go]
tests: [qb/aliased_test.go]
---

# 62. The statement AST is public

**In short:** The statement AST lives in the public qb package so dialects outside the module can render it, outside any stability promise.

## Decision

`internal/qb` moved to `qb`, because a dialect outside this module has to render it. It stays outside the stability promise until one exists, since only a real one will show whether the shape is right.
