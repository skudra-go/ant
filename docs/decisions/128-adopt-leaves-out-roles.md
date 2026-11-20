---
id: 128
title: "adopt leaves out what names a role"
area: migrations
status: active
tags: [adopt, pg_dump, OWNER TO, GRANT, REVOKE, baseline]
issues: [191]
related: [59]
changed_by: []
code: [migrate/generate.go]
tests: [tests/features/migrations/adopt_test.go]
---
# 128. adopt leaves out what names a role

**In short:** adopt drops a dump's OWNER TO, GRANT, REVOKE and ALTER DEFAULT PRIVILEGES lines, and says how many it dropped.

## Decision

A dump made without `--no-owner --no-privileges` gives tables to the role that owned them and grants to roles of that server, so the baseline failed on any database owned by another role, such as CI's or a managed service's (#191). `adopt` already dropped psql's `\restrict` lines, and now drops the one-line ownership and privilege statements `pg_dump` writes, and says how many, with the advice to put the grants an application needs in a migration of their own. Keeping them, with a warning, was rejected: the baseline is the file every new database runs, and a role it names is rarely there.
