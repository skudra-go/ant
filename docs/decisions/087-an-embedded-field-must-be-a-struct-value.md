---
id: 87
title: "An embedded field must be a struct value"
area: models
status: active
tags: [embedded, pointer, reflection, meta, model-error]
issues: []
related: []
changed_by: []
code: [internal/meta/meta.go, internal/scan/scan.go, raw.go]
tests: [tests/features/model_definition/embedded_test.go, internal/meta/meta_test.go]
---

# 87. An embedded field must be a struct value

**In short:** ant refuses an embedded field that is a pointer or scalar with an error naming the field, from every entry point that uses the model.

## Decision

`ant:"embedded"` on a pointer made opening a context panic, deep in reflection. Supporting one would need its columns nullable, and a rule for when columns that are all NULL mean a nil pointer rather than a zero struct, so ant refuses it instead, with an error that names the field, as it does a scalar, or a struct that holds such a field. The model keeps that error, and every use of it returns it: opening a context, a query, and a raw query or a projection that scans into the struct, which would otherwise read the fields ant did walk and leave those after the pointer empty.
