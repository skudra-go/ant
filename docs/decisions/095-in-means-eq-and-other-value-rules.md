---
id: 95
title: "In means Eq, and other value rules"
area: values
status: amended
tags: [In, NotIn, Eq, null, any-array, secure.Value, UnixTime, utf8]
issues: [94, 107]
related: [89]
changed_by: [134]
code: [cond.go, dialect/render.go, json.go, secure/secure.go, internal/meta/meta.go]
tests: [tests/features/querying/in_test.go, tests/features/querying/not_in_test.go, tests/features/querying/is_null_test.go, json_test.go, secure/secure_test.go]
---

# 95. In means Eq, and other value rules

**In short:** In and NotIn fold Eq and Ne per value, so nil matches NULL; long PostgreSQL lists bind as one array; JSON and secure.Value refuse invalid UTF-8; UnixTime stores zero time as 0.

> **Amended.** `NULL` into a named `[]byte` and an `INTERVAL` into a `time.Duration` are no longer limits left to the drivers: ant reads both (decision 134).

## Decision

`Eq(nil)` matches NULL, but `In(nil, x)` followed SQL, where NULL is in nothing. Now `In` and `NotIn` are `Eq` and `Ne` of each value, folded: a nil value adds `IS NULL` to `In`, and `IS NOT NULL` to `NotIn`, whose other values exclude NULL rows as SQL does. The typed `Expr` methods follow the same rule: `Col(&f).Eq(nil)` is `IS NULL` and `Ne(nil)` is `IS NOT NULL`, where they used to bind `= NULL` and match nothing (#94).

A list longer than the parameters one statement takes goes on PostgreSQL as one array, `= ANY($1)` or `<> ALL($1)`, when its values are plain and of one type, which is what PostgreSQL makes of `IN` anyway. A list that fits stays a list, and other databases, which take no arrays, leave it to the driver to refuse.

`ant.JSON` and `secure.Value` refuse text that is not valid UTF-8, as a text column does: `encoding/json` would store U+FFFD in its place. Changing `secure.Value`'s encoding to keep the bytes was rejected, as it would not read the values stored before.

`secure.Value` writes `"[encrypted]"` to JSON and could not be read back from it, so a model with a secure field could not be a request body or round-trip through a cache (#107). It now decodes a plain value into `V`, and the placeholder leaves `V` as it was, so a model sent back as it was received keeps its value rather than storing the placeholder. Binding the table and column into the ciphertext, so that a value copied to another row fails to decrypt, changes the stored format and is left for a decision of its own.

`ant.UnixTime` stores the zero time as 0, as schemas that keep times as integers use 0 for no time, so 0, and the Unix epoch with it, reads back as the zero time. Storing the zero time as its true count of seconds was rejected, as it would read rows written before as 1970.

Some limits are `database/sql`'s or a driver's, and are documented in `docs/databases.md` rather than worked around: an `sql.Null[uint64]` above `math.MaxInt64`, `NULL` into a named `[]byte`, and an `INTERVAL` into a `time.Duration`.
