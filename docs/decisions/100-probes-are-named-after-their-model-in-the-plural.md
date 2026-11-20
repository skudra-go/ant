---
id: 100
title: "Probes are named after their model, in the plural"
area: models
status: active
tags: [probe, naming, Fields, convention, plural, tests]
issues: []
related: []
changed_by: []
code: [ant_test.go, example_test.go]
tests: [example_test.go, ant_test.go]
---

# 100. Probes are named after their model, in the plural

**In short:** Package-level probes are named after the model in the plural (Users for User) as a convention only; ant accepts any name.

## Decision

The docs, examples and tests named probes with one or two letters, such as `U` for `User`. Two models that start with the same letter needed made-up names (`Pl` for `Plan`, `G` for `Page`, `St` for both `Student` and `Seat`), and a reader could not guess them.

Decision: name a package-level probe after its model in the plural, with the rule ant uses for default table names: `Users` for `User`, `Categories` for `Category`, `Persons` for `Person`. Type names are unique, so probe names are too, and `&Users.Age` reads like the column `users.age`. It is a convention, not a rule: ant accepts any name. A probe declared inside a function keeps a short local name, because a local `users` would be shadowed by `users, err := ...`. `ant_test.go` keeps `U` and `P`, since `example_test.go` in the same package already declares `Users` and `Posts` for other models. A probe of a model whose name is an adjective reads oddly (`Taggeds`); renaming such test models is left for later.
