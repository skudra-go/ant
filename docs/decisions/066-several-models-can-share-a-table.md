---
id: 66
title: "Several models can share a table"
area: models
status: active
tags: [Discriminator, table-splitting, b.Table, ErrBadSchema, Open, SchemaOf, Configure]
issues: [202, 205]
related: [54]
changed_by: []
code: [schema.go, ant.go, internal/meta/meta.go]
tests: [tests/features/model_definition/table_groups_test.go, tests/features/model_definition/configure_test.go, tests/features/model_definition/inheritance_test.go, internal/meta/meta_test.go]
---

# 66. Several models can share a table

**In short:** Models share a table only when stated (b.Table or b.Discriminator); naming-convention collisions are ErrBadSchema at Open and schema build, and Open finds tables in embedded and named struct fields.

## Decision

`b.Discriminator` and table splitting are the same fact to migrations: models on one table merge into one definition, and a column not every model has allows NULL. Query results stay one type per model. There is no `[]any` of mixed subtypes.

Sharing is always said: with `b.Table` (or a convention's `SetTable`) on at least one of the models, or with `b.Discriminator` on each. Two models that reach one table by the naming convention alone, as `billing.Account` and `auth.Account` both reach `accounts`, are refused when the context opens and when the schema is built, with both types named, since each would read the other's rows. Opening checks the context's `Table` fields, and building the schema also checks the models their relations reach.

`Open`, `Wrap`, and `SchemaOf` find the context's tables in its embedded and named struct fields too, which is how a large application groups each domain's tables (#202). A `Table` in an unexported field, or under one, cannot be bound, and is `ErrBadSchema` at `Open` rather than "context is not open" at first use. Likewise a `Configure` method declared on a model that ant would not call, because it takes the builder by value, a builder of another model, more arguments, or returns something, is an error at `Open` rather than configuration that silently never applies (#205). One that the model gets from a struct it embeds is run as it always was.
