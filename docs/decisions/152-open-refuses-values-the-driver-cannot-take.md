---
id: 152
title: "Open refuses values the driver can't take"
area: models
status: active
tags: [Open, ErrNoColumnType, driver, slice, map, struct, PlainValues, Table]
issues: [300]
related: [136]
changed_by: []
code: [schema.go, internal/meta/read.go, dialect/syntax.go, dialect/mysql/mysql.go, dialect/sqlite/sqlite.go, dialect/sqlserver/sqlserver.go]
tests: [tests/plain_values_test.go]
---

# 152. Open refuses values the driver can't take

**In short:** On a dialect whose driver converts only what `database/sql` does (MySQL, MariaDB, SQLite and SQL Server, `Syntax.PlainValues`), `Open` refuses a field whose type `database/sql` can't write or read: a slice, map, array or plain struct with no `driver.Valuer` and `sql.Scanner`. The message names the field and suggests `ant.JSON[T]`. On PostgreSQL pgx converts such values, so they are left to it. `Open` also refuses a table declared as `*ant.Table[T]`.

## Decision

A model with `Addr Address`, `Tags []string` or `Attrs map[string]string` opened without complaint on SQLite, and the first insert failed with `sql: converting argument $1 type: unsupported type main.Address, a struct`, which names no model or field (#300). A `*ant.Table[T]` field was left nil and panicked on first use. The docs promise that a model ant can't map fails at `Open`.

Which values work depends on the driver, not on ant. Checking on all five databases: pgx writes a plain struct to a `jsonb` column and reads it back, writes a `[]string` to a `text[]`, and a map to `jsonb`, but the MySQL, SQLite and SQL Server drivers refuse every slice, map and struct without a `driver.Valuer`, through `database/sql`. So the check belongs to the dialect: `dialect.Syntax.PlainValues` says the driver converts nothing more than `database/sql`, and a dialect that doesn't set it, as PostgreSQL and a third-party dialect by default, keeps today's behavior.

With the flag set, each field of every model `Open` sees, relation targets included, is checked both ways. Writing needs a `driver.Valuer` (on the type or its pointer), or a kind `database/sql` passes: a number, a string, a bool, `[]byte`, a time, or a pointer to one. Reading needs an `sql.Scanner`, one of those kinds, or a type ant reads itself (decision 136's readers, such as a map read from JSON). A field that is only read (`readonly`, `computed`, or a read-only model) needs only to be read, and a `writeonly` one only written, so a read-only `map[string]any` from a JSON column still opens. Every refused field is listed in one error, with `ErrNoColumnType`, as `EnsureCreated` reports a type with no column.

On PostgreSQL, `Open` still accepts a plain struct with no column type: it works against a `jsonb` column, and `EnsureCreated` and `diff` already refuse it when they have to make the column.

A driver can take a type `database/sql` can't, as go-mssqldb takes `civil.Date`. Such a field is refused on that dialect, though the driver could write it; it can't be read into a model through `database/sql` either, so a model with one only ever half-worked. A type of one's own with `Value` and `Scan` works on every database.

Rejected: asking the driver at `Open` through a connection's `NamedValueChecker`. It needs a live connection, which `Wrap` does not promise, and it says nothing about reading. Rejected too: binding a `*ant.Table[T]` field by allocating it. A value field is what every example shows, and two spellings for one thing invite a nil from a context built by hand.
