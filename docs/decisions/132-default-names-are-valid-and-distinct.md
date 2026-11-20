---
id: 132
title: "Names ant writes are valid, quoted, and distinct"
area: migrations
status: active
tags: [naming, IndexName, UTF-8, Enum, Sequence, quoting, ErrBadSchema]
issues: [100, 112]
related: [53]
changed_by: []
code: [schema.go, schema/naming.go, schema/script.go]
tests: [schema/naming_test.go, tests/features/model_definition/refused_test.go, tests/features/migrations/modeling_test.go]
---
# 132. Names ant writes are valid, quoted, and distinct

**In short:** A column of an enum, or a nextval of a sequence, quotes a name that is not plain lower case; a long name is cut on a character; two tables, keys or indexes of one schema with one name are ErrBadSchema.

## Decision

`CREATE TYPE` and `CREATE SEQUENCE` quoted their names, but a column using the enum and a `nextval` default did not, so PostgreSQL folded `UserStatus` to `userstatus` and the `CREATE TABLE` failed (#100). Where a column uses the name it is now quoted unless it is plain lower case, which is also how the database gives it back, so existing schemas and snapshots don't change. A name past the identifier limit was cut by bytes, which could leave invalid UTF-8; it is cut where a character starts (#112). Default names join columns with underscores, as PostgreSQL's do, so an index on `a_b` and one on `(a, b)` were both `t_a_b_idx`, and the second failed when the migration ran. Two tables, keys or indexes of one schema with one name are now `ErrBadSchema` naming both; adding a suffix to the second was rejected, as which one gets it would depend on order, and the names would differ from PostgreSQL's.
