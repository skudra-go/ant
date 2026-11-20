---
id: 13
title: "MariaDB shares the `mysql` package"
area: dialects
status: active
tags: [mariadb, mysql, SupportsReturning, RETURNING]
issues: []
related: [74]
changed_by: []
code: [dialect/mysql/mysql.go]
tests: [dialect/mysql/mysql_test.go]
---

# 13. MariaDB shares the `mysql` package

**In short:** MariaDB is a second dialect in the mysql package that differs by SupportsReturning.

## Decision

The brief's layout has no `dialect/mariadb`. `mysql.MariaDB` lives beside `mysql.Dialect`. It differs in `SupportsReturning`, which was confirmed against live servers: `INSERT ... RETURNING` works on MariaDB 11.8 and is a syntax error on MySQL 8.4.
