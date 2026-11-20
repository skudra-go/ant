---
id: 15
title: "Integration failures do not fail CI yet"
area: tooling
status: historical
tags: [ci, github-actions, continue-on-error]
issues: []
related: []
changed_by: []
code: [.github/workflows/pr-ci.yml]
tests: []
---

# 15. Integration failures do not fail CI yet

**In short:** Records that integration CI jobs used continue-on-error until v0.1 passed them.

> **Historical.** pr-ci.yml has no continue-on-error; lint and test are required gates.

## Decision

The brief expects API tests to fail with `ErrNotImplemented`, so the integration jobs use `continue-on-error`. Remove this when v0.1 starts passing them.

SQLite has its own job, because it needs no service container.
