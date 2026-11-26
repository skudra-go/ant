---
id: 173
title: "drift names the types the models use and do not create"
area: migrations
status: active
tags: [drift, adopt, domain, extension, types, migrations]
issues: [193]
related: [73]
changed_by: []
code: [migrate/operate.go, dialect/postgres/external.go, schema/dialect.go]
tests: [tests/features/migrations/drift_test.go]
---

# 173. drift names the types the models use and do not create

**In short:** After its verdict, `drift` lists the types that columns use and the models do not create, such as a domain, a composite type or an extension's type, with the columns that use each, and says how to make them. They are not drift, so the exit code stays the same.

## Decision

Models can name any type with `.Type(...)`, but they create only their enums and sequences. A database adopted from a dump has the rest, so `drift` passed, while a migration made from the models alone failed on an empty database at the first column of such a type (`type "year" does not exist`, #193). Only the live database knows what a type name is, and `drift` is the command that reads it.

- The database answers: each column type is read through `to_regtype` with the connection's search path, as a migration's `CREATE TABLE` would find it, and a type outside `pg_catalog` and `information_schema` that is not an enum of the models is listed. An array is read as its element, so `year[]` is `year`. Tables that `Config.Ignore` names, and models that are unmanaged or read-only, are left out, as migrations leave them.
- It is a note, not a difference: the database matches the models, and a type a hand-written migration creates is fine. The note says to create such types in a migration of one's own, or to adopt the dump.
- A dialect that can tell implements `schema.ExternalTypes`. PostgreSQL does, and it is the only database with migrations.

Rejected:

- Failing `drift` over them: the database has nothing to fix, and a CI job that checks drift would fail on a database that is right.
- Creating the types from the models: a domain's checks, a composite type's fields and an extension's version are not in the models, and reading them out of the database is the reverse engineering the roadmap rules out.
