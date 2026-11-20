---
id: 117
title: "The migrate program takes the application's options"
area: migrations
status: pending
pr: 274
tags: [WithNaming, WithConvention, migrate-config, SchemaOf, naming]
issues: [81]
related: [67]
changes: []
code: [migrate/config.go, migrate/cli.go, schema.go, conventions.go]
tests: [tests/features/migrations, tests/features/model_definition/inheritance_test.go]
---

# 117. The migrate program takes the application's options

**In short:** migrate.Config.Options carries the application's Open options to SchemaOf and Open, and one WithConvention option given twice adds one rule.

> **Pending.** Added by pull request #274, not merged yet. When it merges, set `status: active`.

## Decision

`WithNaming` and `WithConvention` take effect when a context opens, but the migrate commands built the models' schema with `SchemaOf`, which took no options, and opened with `WithTracking(false)` alone. So `diff` wrote tables and columns under the default names while the application queried its own (#81). `migrate.Config.Options` now carries the options the application opens with, and `Main` and `Run` pass them to `SchemaOf`, which takes options now, and to `Open`. One rule from `WithConvention` given twice, as to `Open` and to `SchemaOf`, or to a `Session`, is added once, so a rule that is not safe to repeat, such as one that prefixes table names, still runs once on each model. Reading the options from a hook in the models package was rejected: the options already exist where the application opens its context.
