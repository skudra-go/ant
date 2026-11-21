---
id: 139
title: "A field can't ask for two things ant can't both do"
area: models
status: active
tags: [Open, tags, ErrBadSchema, readonly, writeonly, immutable, computed, version, token, embedded, DeletedAt, Generated]
issues: [208]
related: [125]
changed_by: []
code: [internal/meta/meta.go]
tests: [tests/features/model_definition/conflicting_tags_test.go]
---

# 139. A field can't ask for two things ant can't both do

**In short:** Open refuses a field whose tags and configuration contradict each other, a column key on an embedded struct, and two fields that take a role a model has once, each as `ErrBadSchema` naming the field.

## Decision

Decision 125 refused tag keys ant can't read. Keys it can read could still contradict each other, and ant then did one of the two things they asked for without a word (#208): `readonly,writeonly` gave a `NOT NULL` column that no insert wrote, two `version` fields kept the last, and `computed,unique` dropped the constraint.

A field's roles say whether ant writes it and whether it reads it back. `readonly`, `computed` and a column given `.Generated(...)` are never written, and `writeonly` is never read. Open now refuses each pair that asks for the opposite: one never written with `writeonly` or `immutable`, which is written on insert; a `version`, which ant writes on every update, with any of those or with `immutable`; a `version` or `token`, which ant reads back to compare, with `writeonly`; and `computed`, which has no column, with `token`, `unique`, `default` or `.Generated`. The check runs once `Configure` has, since `.Generated` makes a column readonly and `.Unique` and `.Default` add to the tags. A column key on an embedded struct, as `ant:"embedded,unique"`, has no column to describe, and is refused too. Two `version` fields, or two `ant.DeletedAt` fields, are refused, as a model has one of each.

Keys that only repeat each other, such as `computed,readonly` or `version,token`, and pairs that fit, such as `readonly,token` for a token the database keeps or `writeonly,immutable` for a value written once, still open. A field tagged `-` has no column, so the rest of its tag is not checked.

Picking one of the two keys was rejected: either choice is a guess, and the model that results is wrong in a way that shows only at the first write.
