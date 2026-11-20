---
id: 82
title: "A rename must land on a name the models have"
area: migrations
status: active
tags: [rename, diff, schema-diff, migrations, Snapshot, typo, data-loss]
issues: []
related: []
changed_by: []
code: [schema/diff.go, schema/rename.go, migrate/generate.go]
tests: [tests/features/migrations/rename_target_test.go, schema/diff_test.go]
---

# 82. A rename must land on a name the models have

**In short:** A --rename must target a table or column that the models have in the same schema and the snapshot does not, otherwise diff stops with an error listing the new names.

## Decision

`--rename` is there to keep data, but a table rename never checked its new name. `--rename users=peple` renamed the table to the typo, created `people` empty from the models, and dropped `peple` as a table the models no longer had, with every row in it. Column renames checked the models, but not the snapshot, so a column renamed onto one that already existed planned a statement the database refuses.

Now a rename's new name must be a table or column the models have, in the table's own schema, and one the snapshot does not have yet, which also catches two renames onto one name and a rename to the same name. Otherwise `diff` stops before writing anything, and the error lists the tables or columns that are new in the models, since a typo is usually one of them. A rename into another schema is refused for the same reason: the plan would move nothing, and drop the table.
