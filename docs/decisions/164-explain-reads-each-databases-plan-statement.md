---
id: 164
title: "Explain reads each database's plan statement, and plans the statement All runs"
area: queries
status: active
tags: [Explain, sqlite, mysql, mariadb, sqlserver, IncludeJoined, dialect]
issues: [110]
related: [163]
changed_by: []
code: [query.go, dialect/syntax.go, dialect/mysql/mysql.go, dialect/sqlite/sqlite.go, dialect/sqlserver/sqlserver.go]
tests: [tests/explain_test.go]
---

# 164. Explain reads each database's plan statement, and plans the statement All runs

**In short:** `Query.Explain` sends `EXPLAIN` on PostgreSQL, `EXPLAIN FORMAT=TREE` on MySQL, `EXPLAIN FORMAT=JSON` on MariaDB and `EXPLAIN QUERY PLAN` on SQLite, and returns the last column of each row, one line per row. SQL Server returns `ErrUnsupported`. The statement it plans has the joins of `IncludeJoined`.

## Decision

`Explain` prefixed `EXPLAIN` and scanned one string per row, which only PostgreSQL returns. SQLite's `EXPLAIN` is the bytecode, eight columns that fail the scan; MySQL and MariaDB return a table of ten to twelve columns (#110). It also built its statement without `addJoined`, so an `IncludeJoined` query was planned without its join.

`dialect.Syntax.Explain` names the command, and the plan's text is the last column of each row it returns: PostgreSQL's single `QUERY PLAN` column, MySQL's tree, MariaDB's JSON document, and the `detail` column of SQLite's query plan. MySQL's tree needs 8.0.16, newer than the 8.0 the dialect claims, and is the only text form MySQL has. MariaDB has no tree, so it gets the JSON form, which is one column. SQL Server shows a plan only through `SET SHOWPLAN_TEXT ON`, a session setting that must run alone in its batch on the connection that sends the query, so it lacks `dialect.ExplainPlan` and the call refuses before sending anything.

Rejected:

- Formatting MySQL's and MariaDB's tabular `EXPLAIN` as text: ant would pick the columns and their layout, and the result would differ from what either database prints.
- `SET SHOWPLAN_TEXT` on a pinned connection for SQL Server: inside a transaction the setting would change what the transaction's other statements do until it is turned off, and a failure between the two `SET`s would leave it on.
