---
id: 55
title: "Renames"
area: migrations
status: active
tags: [rename, --rename, prompt, drift, warning]
issues: []
related: [11]
changed_by: []
code: [schema/rename.go, schema/diff.go, migrate/cli.go, migrate/generate.go]
tests: [schema/diff_test.go, migrate/internal_test.go, tests/features/migrations/rename_target_test.go]
---

# 55. Renames

**In short:** A drop and add with a warning stays the default, plus a terminal prompt, --rename table.old=new, the exact RENAME statement in the warning, and automatic renames of identically defined constraints and indexes.

## Decision

Decision 11 chose a drop and add with a warning. That stays the default, because a diff cannot know. Three things were added, since a warning alone loses data if missed: a terminal prompt, `--rename table.old=new`, and the exact `RENAME` statement in the warning, for editing the file by hand. Renames of objects follow by themselves: a constraint or index with the same definition under a new name is renamed, not rebuilt.
