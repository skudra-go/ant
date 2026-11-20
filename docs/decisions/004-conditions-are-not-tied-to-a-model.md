---
id: 4
title: "Conditions are not tied to a model"
area: queries
status: active
tags: [Cond, ErrWrongModel, antvet, probes, go-vet]
issues: []
related: [18]
changed_by: []
code: [cond.go, builder.go, tools/antvet/analyzer]
tests: [ant_test.go, tools/antvet/analyzer/analyzer_test.go]
---

# 4. Conditions are not tied to a model

**In short:** Cond is untyped by model; wrong-model probes surface as errors at the terminal call, and an antvet analyzer checks calls statically.

## Decision

The target API uses `ant.Gt(&Users.Age, 18)` with no model type, and `Where` takes plain `Cond` values. Adding a model parameter to `Cond` would make condition calls uglier.

Decision: keep `Cond` untyped by model. Probes from the wrong model, and non-probe pointers, are returned as errors at the terminal call. A `go vet` analyzer is already planned for v0.9.
