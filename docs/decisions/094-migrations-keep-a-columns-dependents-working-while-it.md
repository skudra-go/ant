---
id: 94
title: "Migrations keep a column's dependents working while it changes"
area: migrations
status: active
tags: [postgres, migrations, generated-column, enum, rename, foreign-key, identity, NullsFirst, NullsLast, IndexBuilder]
issues: []
related: []
changed_by: []
code: [schema/diff.go, schema/script.go, schema/change.go, schema/rename.go, schema/dialect.go, schema.go]
tests: [schema/diff_test.go, schema/retype_test.go, schema/probes_test.go, tests/features/migrations/generated_test.go, tests/features/migrations/rename_target_test.go, tests/features/migrations/modeling_test.go]
---

# 94. Migrations keep a column's dependents working while it changes

**In short:** Migration plans order PostgreSQL column changes so defaults, identities, generated columns, foreign keys, renames, enums and sequences keep working, and refuse over-long names.

## Decision

The plan's order now follows what PostgreSQL needs while a column changes:

- A default is dropped before a type change and set again after, since the old one may not cast, and its text keeps the old type's cast, which `drift` would see and which would hold an enum the plan drops. An identity is dropped before a default is set, and added after `NOT NULL` and after the default goes. An enum casts to another type through text.
- A generated column is dropped before a column it reads is dropped or changes type, and added after, as is one that becomes generated or gets a new expression; one that stops being generated drops its expression first, keeping its values. New generated columns come after the columns they read, whatever the declaration order.
- A foreign key on a column whose type changes, on either side, is dropped first and made again last.
- Index and constraint renames run after the drops, so a name can move from one that goes, and renames that swap names go through a spare one. Table and column renames are applied as a batch: table renames first, so a column can be named under its table's old or new name, swaps through a spare name, a name with a dot found as it is or quoted, and `schema.table` as a new name moving a table to a schema the models have, which a hint now suggests.
- Renaming a column rewrites the expressions that name it in the snapshot, as PostgreSQL does in the database, so nothing is rebuilt for it. What the rewrite misses is rebuilt, as before.
- A new enum value goes `BEFORE` the value that follows it in the models. One the plan uses, in a default, a check, a generated column, or an index, makes the file run without a transaction, since PostgreSQL lets a transaction use a value it added only once committed; the literal is matched as text, and erring toward a match costs only the transaction. An order PostgreSQL cannot change is a warning, as a removed value is.
- A sequence's increment and start change in place, keeping its current value.
- A new `NOT NULL` column of `time`, `timetz`, `interval`, `inet`, `cidr`, `macaddr`, `money`, `bit(n)`, or an enum gets a zero value for existing rows, the enum its first value the plan can use.
- An index part says where NULLs go when that is not its direction's default, `IndexBuilder.NullsFirst` and `NullsLast`, and `Inspect` reads it back, with quoted names unescaped.
- A name longer than the database keeps is refused when the script is written, rather than cut, where two names could become one.
- Comments in a script escape line breaks, which a name may hold, and which would end a comment and make the rest SQL.
