---
id: 53
title: "Names follow PostgreSQL's defaults"
area: migrations
status: active
tags: [naming, pkey, fkey, index-name, identifier-limit, hash]
issues: []
related: [50]
changed_by: []
code: [schema/naming.go]
tests: [schema/diff_test.go, schema/probes_test.go]
---

# 53. Names follow PostgreSQL's defaults

**In short:** Constraints and indexes are named like PostgreSQL does (users_pkey, users_email_key, posts_user_id_fkey, posts_user_id_idx), with long names shortened by a hash.

## Decision

Constraints and indexes are named `users_pkey`, `users_email_key`, `posts_user_id_fkey`, and `posts_user_id_idx`, as PostgreSQL itself names them, so a table written by hand usually needs no configuration to adopt. A name past the identifier limit is shortened with a hash, because the server would cut it silently and two names could become one.
