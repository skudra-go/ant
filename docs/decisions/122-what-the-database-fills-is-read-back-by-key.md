---
id: 122
title: "What the database fills is read back by the row's key"
area: writes
status: active
tags: [readonly, generated, trigger, token, RETURNING, LastInsertId, NoKey, mysql]
issues: [109, 156]
related: [75, 93]
changed_by: []
code: [unit.go]
tests: [tests/readback_test.go]
---
# 122. What the database fills is read back by the row's key

**In short:** After an update, readonly columns are read back by key in the same transaction; without RETURNING, a model with no key keeps its zero values and a key the database fills other than through AUTO_INCREMENT is refused.

## Decision

After an insert ant reads back what the database filled: through `RETURNING`, or on MySQL by the key `LastInsertId` gives. After an update it read nothing, so a column a trigger keeps, such as `last_update`, stayed at the loaded value, and as a token it failed the entity's next save with a conflict nobody caused (#156). Now an update of a model with `readonly` columns (generated ones and those a trigger keeps, not computed or write-only ones) reads them back by key right after the write, in the same transaction, into the entity and so into its snapshot. A `SELECT` after the write was chosen over `RETURNING` on the `UPDATE`: SQLite's `RETURNING` leaves out what triggers change, and SQL Server refuses `OUTPUT` on a table with a trigger, so one path is right on all five databases. Models without such columns pay nothing.

On a database without `RETURNING` the read-back finds the row by its key, and for a model with no key its `WHERE` was empty, so a new row took the first stored row's values (#109). A model with no key now keeps the values it was given in the columns the database filled; refusing the insert was rejected, as it would make such models impossible to write there. A key the database fills other than through `AUTO_INCREMENT`, as a `Default` on the key, can't be learned without `RETURNING` at all, so that insert is refused with `ErrUnsupported` before it runs, where it used to write the row and then fail to find it.
