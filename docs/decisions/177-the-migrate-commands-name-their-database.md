---
id: 177
title: "The migrate commands name their database"
area: migrations
status: active
tags: [migrate, dsn, DATABASE_URL, output]
issues: [342]
related: [11, 60]
changed_by: []
code: [migrate/cli.go, migrate/config.go]
tests: [tests/features/migrations/target_test.go]
---

# 177. The migrate commands name their database

**In short:** The commands that apply, record, repair, list or seed migrations show the connected server address and database name, along with the DSN source.

## Decision

An application may build its connection from its own configuration while a migrate command silently uses a different `DATABASE_URL` (#342). `up`, `status`, `baseline`, `repair` and `seed` now print the database name, the address of the server the driver reached, and which DSN source supplied the connection. The address comes from the connected socket, so it works for both URL and keyword connection strings and reflects the endpoint actually reached when the driver falls back to another server. The notice includes only the quoted database name, the quoted address when available, and a fixed source label. If the name cannot be read, the command reports it as unknown and continues. `up --dry-run` keeps its existing output.

The order remains `--dsn`, `DATABASE_URL`, then `Config.DSN`, and the program still does not load `.env`: either change would alter existing programs. A program can load its own configuration and pass its DSN, or call `migrate.Up` on the connection it already opened.
