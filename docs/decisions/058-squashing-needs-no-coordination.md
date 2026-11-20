---
id: 58
title: "Squashing needs no coordination"
area: migrations
status: active
tags: [squash, ant:replaces, baseline, --keep, migrations]
issues: []
related: [59]
changed_by: []
code: [migrate/migration.go, migrate/engine.go, migrate/cli.go, migrate/generate.go]
tests: [tests/features/migrations/squash_test.go]
---

# 58. Squashing needs no coordination

**In short:** A baseline with -- ant:replaces lists what it replaces: databases that have them record it without running, new ones run it, partway databases are refused; squash warns and --keep joins old files.

## Decision

`-- ant:replaces 0006` at the top of a baseline says what it replaces. A database that has those migrations records the baseline without running it, and a new one runs it, so nobody has to run a command per environment, and rows for deleted files stay valid. A database partway through is refused, because neither path is right for it.

A squash writes from the models, which loses anything written by hand, so it warns, and `--keep` joins the old files instead.
