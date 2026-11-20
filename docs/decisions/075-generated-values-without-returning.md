---
id: 75
title: "Generated values without RETURNING"
area: writes
status: active
tags: [LastInsertId, mysql, mariadb, Upsert, on-duplicate-key, generated-keys]
issues: []
related: [63, 74]
changed_by: []
code: [upsert.go, unit.go, dialect/mysql/mysql.go]
tests: [tests/features/writing/upsert_test.go, tests/matrix/matrix_test.go]
---

# 75. Generated values without RETURNING

**In short:** MySQL reads keys from LastInsertId so rows with generated keys are written one at a time, and Upsert on MySQL/MariaDB decides from the row count and reads the row back by conflict columns.

## Decision

MySQL gets keys from `LastInsertId`, which reports one key per statement, so rows with generated keys are written singly there. `ON DUPLICATE KEY` returns the old row even when it changed nothing, so on MySQL and MariaDB, `Upsert` decides from the row count, and reads the row back by the conflict's columns.
