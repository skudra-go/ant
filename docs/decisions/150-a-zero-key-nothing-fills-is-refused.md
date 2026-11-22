---
id: 150
title: "A zero key that nothing fills is refused"
area: models
status: active
tags: [primary-key, uuid, zero-value, GenerateKey, default, ErrZeroKey]
issues: [297]
related: [19]
changed_by: []
code: [unit.go, upsert.go, merge.go, errors.go]
tests: [tests/zero_key_test.go, tests/features/keys_and_identity/string_uuid_keys_test.go]
---

# 150. A zero key that nothing fills is refused

**In short:** `Insert`, `InsertMany`, `Upsert`, `FirstOrCreate`, `SaveChanges` and `PrepareBulk` refuse an entity whose single-column key is its type's zero value when nothing fills it (no `GenerateKey`, no default, not an integer), with `ErrZeroKey`. The message names the field and the ways to fill it.

## Decision

Decision 19 left a non-integer key to the application, and wrote an empty one as it was. A `uuid.UUID` key is the usual alternative to an integer, and one nothing generated went in as `00000000-0000-0000-0000-000000000000`. The second insert then failed with a bare duplicate-key error that said nothing about why (#297). An empty string key went the same way.

Now every path that inserts an entity refuses such a key before it writes, with `ErrZeroKey`. The message names the field (`Doc.ID is the zero uuid.UUID, and nothing generates it`) and gives the fixes: set it, generate it with `ant.GenerateKey(b, &Docs.ID, uuid.New)`, or give the column a default such as `default=gen_random_uuid()`. The check runs after the `BeforeInsert` hooks, so a hook that sets the key still works, and before anything is stamped, so a refused bulk insert leaves timestamps and versions unset.

A key is left alone when:

- its type is an integer, which the database generates, or for which zero is a key the application chose with `NotGenerated`;
- `GenerateKey` fills it in Go;
- the database fills it: a `default=` tag, or a generated column;
- it is composite, since a zero part of a composite key, such as tenant 0, is ordinary.

Rejected: generating a UUID for every zero `uuid.UUID` key. Which version (4 or 7), and whether the database or Go makes it, are the application's choices, and `GenerateKey` already states them in one line. Rejected too: leaving it to the database's duplicate-key error, which comes only on the second insert, and from a row that should never have been written.
