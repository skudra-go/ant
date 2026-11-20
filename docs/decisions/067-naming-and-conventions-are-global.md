---
id: 67
title: "Naming and conventions are global"
area: models
status: active
tags: [WithNaming, WithConvention, naming, global, Open, model-registry]
issues: []
related: []
changed_by: []
code: [conventions.go, internal/meta/meta.go, ant.go]
tests: [tests/features/model_definition/inheritance_test.go]
---

# 67. Naming and conventions are global

**In short:** WithNaming and WithConvention apply once per model in the first Open, since a model is reflected once per process.

## Decision

`WithNaming` and `WithConvention` apply once per model, in the first `Open`, because a model is reflected once per process. Per-connection naming would need a second model registry, for a need nobody has.
