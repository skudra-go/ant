---
id: 113
title: "A missing versions folder is an error"
area: migrations
status: active
tags: [migrate, fs.ErrNotExist, versions-folder, Config.FS, embed, history-table]
issues: [176]
related: []
changed_by: []
code: [migrate/source.go, migrate/operate.go]
tests: [tests/features/migrations/source_test.go]
---

# 113. A missing versions folder is an error

**In short:** The default migration source returns an error wrapping fs.ErrNotExist for a missing versions folder, except for diff, adopt, check and drift on projects with no migrations.

## Decision

A versions folder that was not there read as an empty one, so a `migrate` binary run from another directory, an image built without its migrations, or a `//go:embed migrations` passed as `Config.FS` without `Dir`, found nothing to apply, created the history table and exited 0, and the app failed at its first query (#176). Now the default source says there is no folder, where it looked, and, on disk, from which working directory, and the error wraps `fs.ErrNotExist`. `up`, `status`, `baseline`, `repair`, `script`, `snapshot`, `squash`, `Up` and `Status` refuse it, and `up`, `repair` and `baseline` read the migrations before they create the history table, so a refused run records nothing. The commands for a project with no migrations yet read it as empty: `diff` and `adopt`, which write the first migration and create the folder, `check`, which reports pending changes as it did, and `drift`, which compares the database with the models while a database is being adopted. A missing code folder is still fine, since repeatable migrations are optional. A custom `Source` decides for itself.
