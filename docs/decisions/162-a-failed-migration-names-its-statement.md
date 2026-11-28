---
id: 162
title: "A failed migration names its statement even when the driver sends no position"
area: migrations
status: active
tags: [migration, failure, error, line, statement, Explain, Split, DDL]
issues: [303]
related: [56]
changed_by: []
code: [migrate/engine.go]
tests: [tests/features/migrations/up_test.go]
---

# 162. A failed migration names its statement even when the driver sends no position

**In short:** A transactional file migration runs statement by statement inside its transaction, so a failure names the statement and its line even when PostgreSQL sends no position, which it does not for most DDL and constraint errors.

## Decision

`up` runs a file migration as one string inside its transaction. A failure then says `failed and was rolled back` and nothing else, unless the driver sends a position: PostgreSQL sends one for expression and cast errors but not for most DDL and constraint errors, which are the common failures of a generated file. A file can have twenty statements, and the troubleshooting page reads the line and the statement off that message (#303).

A transactional file migration now runs its statements one at a time, inside the same transaction, which leaves the all-or-nothing rollback as it was. The cost is one round trip per statement instead of one per file, which a large data migration notices: 5,000 statements over a 1 ms link take about 5 s longer. That is the price of naming the line. The failing statement and its line come from the statement that raised the error. A comment before a statement stays with it, as `Split` keeps it, so the line is the first that carries code, not the first comment: a `--` line and a block comment, over however many lines it runs, are both passed over. When the driver does send a position, it is relative to the statement, so the line is that statement's first line plus the lines the position is into it.

Two things keep the old path. A Go migration, and a `-- ant:no-transaction` file, run as before: the first is code, the second has no transaction to run inside. A file whose statements `Split` cannot place in the script, because it rebuilt them so that they are not substrings, runs whole as it did, so nothing regresses; no line is then named unless the driver sends a position.

Rejected:

- Running the file statement by statement after a failure, inside the same rolled-back transaction, to find the one that fails: it would run every earlier statement twice, and a statement that changed the session could fail differently the second time.
- Matching the object the error names against the statements: the message names relations and columns, not always uniquely, and a match is a guess where running the statements one by one is a fact.
