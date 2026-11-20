---
id: 125
title: "Open refuses a model it would misread"
area: models
status: active
tags: [Open, tags, ErrBadSchema, relations, embedded, ManyToMany, PivotColumns, netip]
issues: [98, 99, 203, 204]
related: [21, 87]
changed_by: []
code: [internal/meta/meta.go, schema.go, ant.go, internal/errs/errs.go]
tests: [tests/features/model_definition/refused_test.go, tests/features/relations/configuration_test.go]
---
# 125. Open refuses a model it would misread

**In short:** Open and every use of a model refuse an unknown tag key, two fields on one column, a slice of pointers to a model, a struct embedded by pointer, and a func or chan field; Open also resolves every relation, and reports every broken model at once.

## Decision

Several shapes were read without a word and gave a schema or a model that was silently wrong: a misspelled tag key was ignored, so `uniqe` was no constraint (#98); two fields that map to one column kept the last, and the other was never read or written (#99); `[]*Post` became a column and an embedded `*Base` a relation with no key (#204). Each is now an `ErrBadSchema` naming the field, from `Open`, `SchemaOf` and every use of the model. A tag key that takes no value refuses one (`unique=false` was `unique`), and `prefix=` needs `embedded` or an anonymous struct, which now takes the prefix it was given. Fields of a kind no database stores, a func, a chan or an `unsafe.Pointer`, are refused too. A struct field with no tag is still one column: `netip.Addr` and other types the driver converts are structs, and refusing them would guess wrong.

`Open` promised to fail on a model it can't map, but relations were resolved on first use, so a relation with no foreign key, a many-to-many with no join table, or one on its own model with one column for both sides passed `Open` and failed the first request (#203). `Open` now resolves every relation of every model its tables reach, and the last two are errors of their own. It reports every problem it finds, joined, so a large model set is fixed in one go. A relation a model inherits from one it embeds, as `User.Posts` in a model that embeds `User`, also looks for the key its declaring model's conventions give, so that model opens and loads it; and a struct with no exported field, such as `netip.Addr`, is a value, so `*netip.Addr` is a nullable column rather than a relation. These make `Open` fail for models that opened before, which is the point, and goes in the release notes.
