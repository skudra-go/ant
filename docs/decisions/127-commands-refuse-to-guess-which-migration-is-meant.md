---
id: 127
title: "Commands refuse to guess which migration is meant"
area: migrations
status: active
tags: [baseline, repair, diff, check, snapshot, renumber, ErrChecksum, ErrStaleSnapshot]
issues: [178, 179, 190]
related: [58, 59]
changed_by: []
code: [migrate/engine.go, migrate/operate.go, migrate/generate.go, migrate/cli.go]
tests: [tests/features/migrations/team_test.go, tests/features/migrations/adopt_test.go]
---
# 127. Commands refuse to guess which migration is meant

**In short:** A version applied under another name and with other contents is a different migration, which repair refuses; diff and check refuse a snapshot written for a file that is gone; baseline with no version records one migration and asks for a version for more.

## Decision

Three commands guessed, and a wrong guess left a database without a migration it never ran. History matched by version only, so after two branches both wrote 0002 and one was renumbered, a database that ran the other branch's 0002 saw an edited file, and `repair` recorded the file as applied (#178). A version whose history row has another name and other contents is now reported as applied as that other migration, and `repair` refuses it, naming both; a new name with the same contents is still the same migration. `diff` found nothing to write when the snapshot had been written for a file since deleted, and `check`'s advice, run `snapshot`, made everything pass with the change in no file (#179); both now refuse such a snapshot and say to put back the one it replaced. `baseline` with no version recorded every migration, so a database adopted after more were written skipped them (#190). With no version it now records only when one migration is left to record, and otherwise names the version to give; `adopt` names it too.
