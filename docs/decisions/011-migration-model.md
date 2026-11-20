---
id: 11
title: "Migration model"
area: migrations
status: amended
tags: [migrate, Snapshot, diff, versions, repeatable, no-down, Main, fs.FS]
issues: []
related: [50, 55]
changed_by: [55]
code: [migrate/cli.go, migrate/source.go, migrate/config.go, schema/script.go]
tests: [tests/features/migrations/diff_test.go, tests/features/migrations/up_test.go, tests/features/migrations/repeatable_test.go, tests/features/migrations/source_test.go]
---

# 11. Migration model

**In short:** Migrations diff Go structs against a stored snapshot into plain SQL files, run versioned and repeatable folders from an fs.FS, through migrate.Main, with no down migrations.

> **Amended.** Decision 55 adds a prompt, --rename and the exact RENAME statement to the rename warning; the CLI also gained drift, squash, adopt and more (migrate/cli.go:122-265). Down files are ignored (source.go:130).

## Decision

Status: approved by the owner on 2026-10-31. The owner asked for structs as the source of truth, a CLI, two folders, and no lock-in.

- **Source of truth:** the Go structs. A command compares them with a stored snapshot and writes the difference as plain SQL, which the developer can edit.
- **Snapshot, not a shadow database:** like EF Core, diffing uses a snapshot file in source control. Generating a migration needs no database and no permission to create one.
- **Two folders:** `migrations/versions` holds sequential files that run once. `migrations/code` holds repeatable SQL that reruns when its checksum changes, after all versioned files, as in Evolve and Flyway.
- **Plain SQL only:** no proprietary format. Any tool or person can read the files, and leaving ant keeps them usable.
- **Pluggable storage:** the runner reads from an `io/fs.FS` by default, so `go:embed` works. A small interface replaces file handling entirely. Unset options use defaults.
- **The CLI lives in the user's module:** Go cannot load structs from another module at runtime. A small `main` calls `migrate.Main[AppDB]`, so commands run as `go run ./cmd/migrate diff add_bio`.
- **Commands:** `diff`, `up`, `status`, and `check`. `check` fails when models and snapshot differ, for use in CI.
- **Renames:** a diff cannot tell a rename from a drop and add, so it writes a drop and add with a warning comment, and the developer edits the SQL.
- **No down migrations:** rollback is out of scope. To undo a change, write a new forward migration.
