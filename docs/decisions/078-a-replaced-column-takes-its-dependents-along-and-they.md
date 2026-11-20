---
id: 78
title: "A replaced column takes its dependents along, and they come back"
area: migrations
status: active
tags: [generated-column, DropColumn, Again, Replaces, CONCURRENTLY, DROP-EXPRESSION, foreign-key]
issues: []
related: [94]
changed_by: []
code: [schema/diff.go, schema/change.go, dialect/postgres/ddl.go]
tests: [schema/diff_test.go, schema/probes_test.go, tests/features/migrations/generated_test.go]
---

# 78. A replaced column takes its dependents along, and they come back

**In short:** A plan that replaces a generated column drops and recreates its indexes, constraints, comment and dependent foreign keys around it, avoids CONCURRENTLY, and uses DROP EXPRESSION when a column stops being generated.

## Decision

PostgreSQL 17 can change a generated column's expression in place, with `SET EXPRESSION`, but ant supports 13 and later, and a migration is written without knowing which server will run it. So a new expression, or a column that becomes generated, is a `DROP COLUMN` and an `ADD COLUMN`. The plan writes them as a `DropColumn` with `Again` set, before other columns change, and an `AddColumn` after (decision 94). `AlterColumn.Replaces` still says when, for code that builds its own changes, and the PostgreSQL dialect follows it exactly.

The drop takes indexes, unique and check constraints, the primary key, and the comment with it, and is refused while a foreign key in another table points at the column. A snapshot still lists all of these, so `check` passed while the database had lost them, and only `drift` would have found it. The plan now drops each of them before the column and makes them again after. An object counts as using the column if it names it in its columns, its included columns, an expression, or a predicate. Expressions are matched as text, by whole identifier, in double quotes, or spelled in Unicode escapes such as `U&"sl\0075g"`, and erring toward a match costs only a rebuild that was not needed. That is also why an escaped name is decoded with every character in it as the escape, rather than the one `UESCAPE` picks: comments may stand between the two, and a wrong guess can only find a match too many.

A foreign key depends on the unique key it points at, and PostgreSQL will not drop that key while the foreign key exists. Which key a foreign key chose is only known to the server, so whenever a plan drops a primary key, unique constraint, or unique index on the same columns, in any order, the foreign key is dropped first and made again last. That covers a unique index rebuilt because a replaced column is one of its included columns, and one changed for any other reason.

A plan that replaces a column builds and drops every index without `CONCURRENTLY`, not only those on the column. A single online statement takes the transaction away from the whole file, and half a migration could leave the column replaced with its indexes and constraints gone, which is the failure this decision is about. For indexes on the replaced column nothing is lost, since the replacement holds an exclusive lock on the table until the end anyway. An index elsewhere does block writes while it builds, so the file warns about each one that asked to be concurrent, and says to move it to a migration of its own. Splitting the file automatically was considered and rejected: moving online statements later breaks a foreign key that needs a new unique index, and a later `DROP INDEX` can find the index already gone with its column.

A column that stops being generated is not replaced: `DROP EXPRESSION` (PostgreSQL 13) keeps its values and everything on it. Before this, it was dropped and added empty, which lost the data, or failed on a `NOT NULL` column with rows.
