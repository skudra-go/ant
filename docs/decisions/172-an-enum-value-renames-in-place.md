---
id: 172
title: "An enum value renames in place"
area: migrations
status: active
tags: [enum, rename, RENAME VALUE, Hint, diff, schema-diff, migrations]
issues: [189]
related: [55, 82]
changed_by: []
code: [schema/diff.go, schema/rename.go, schema/change.go, schema/script.go, dialect/postgres/ddl.go, migrate/cli.go]
tests: [schema/diff_test.go, schema/probes_test.go, tests/features/migrations/diff_test.go]
---

# 172. An enum value renames in place

**In short:** An enum that loses a value and gains one is a rename hint, as a column is, whose statement is `ALTER TYPE ... RENAME VALUE`. `--rename enum.value=new_name`, or a yes at the prompt, takes it up, and the rows that hold the value read the new name.

## Decision

A value of an enum given a new name in the models planned `ADD VALUE` for the new one and a warning to rebuild the type by hand for the old one, which PostgreSQL cannot remove. The rows kept the old value, and `drift` failed until someone rebuilt the type. `--rename` took only tables and columns, so `--rename item_status.banned=blocked` reported that there was no table `item_status.banned` (#189). PostgreSQL renames a value with one statement that rewrites nothing.

- `Hint` has `Enum` and `Value`. A hint about a value of an enum has them set and no table, and `Flag`, `Change` and `String` read them. Every value the enum lost, paired with every value it gained, is a hint, as every dropped column is paired with every added column of its type.
- A rename's path `enum.value` names a value of an enum the snapshot has. A path that names a table's column as well as an enum's value is an error, as two readings of a path already are. In PostgreSQL a table and a type cannot share a name in one schema anyway.
- The new name must be a value the models' enum has, and the snapshot's enum lacks or has only in a value the same batch renames away, as decision 82 asks of a column. Two values may swap names, through `ant_rename_1` and on.
- Value renames run before any value is added, since `ADD VALUE ... BEFORE` may name a renamed one. A renamed value can be used in the same transaction, unlike an added one, so the plan keeps its transaction.

Rejected:

- Renaming when the enum lost one value and gained one at the same place, without asking: a drop and an add look the same, and decision 55 leaves the choice to a person.
- Rewriting the snapshot's defaults and checks that name the old value: the models already hold the new text, and the plan writes it after the rename, which PostgreSQL accepts.
