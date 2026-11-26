---
id: 176
title: "ant's structs have camelCase JSON keys"
area: models
status: active
tags: [json, Base, Page, CursorPage, Point, Partition, metrics, breaking]
issues: [308]
related: []
changed_by: []
code: [ant.go, paging.go, dialect/postgres/functions.go, dialect/postgres/partitions.go, metrics/metrics.go]
tests: [tests/features/model_definition/json_keys_test.go]
---

# 176. ant's structs have camelCase JSON keys

**In short:** The structs ant hands back for callers to encode carry camelCase json tags: `ant.Base` (`id`, `createdAt`, `updatedAt`), `ant.Page`, `ant.CursorPage`, `postgres.Point`, `postgres.Partition` and `metrics.Sample`. It changes their JSON, so it shipped as a breaking change.

## Decision

A field of an embedded struct cannot be tagged from the outside, so every model that embedded `ant.Base` answered `{"ID":1,"CreatedAt":...}`, and a JSON API had to drop `ant.Base` or write a response type per model (#308). `ant.Page` had the same problem (#345). The owner's rule: anything in ant with JSON keys of its own uses camelCase, so ant picks one casing everywhere rather than leave Go's field names in its users' APIs.

- Tagged: the structs callers put in a response or a log as data. That is the model base, both page types, the PostgreSQL point and partition, and a metrics sample (`duration` stays nanoseconds, as `time.Duration` encodes).
- Not tagged: the error types, which #343 and #344 are reshaping, and whose `Err` field encodes as `{}` anyway. Also builders and handles with no exported fields, and the snapshot format, which already uses camelCase keys. The reports ant hands to Go code, such as `migrate.Entry`, `migrate.Applied`, `schema.Summary`, `ant.Alert` and `ant.Change`, have no json tags yet, so they still encode with Go's field names. Whether they take camelCase tags too is left to the owner.
- A key of an embedded struct is now `id`, `createdAt` or `updatedAt`. `encoding/json` drops a key that two embedded structs at the same depth both give, so a model that embeds `ant.Base` beside another struct with a field tagged `json:"id"` encodes neither. A field the model declares itself wins over the embedded one, as before.
- `encoding/json` matches keys without regard to case when it decodes, so JSON written with the old keys, such as a request body or a value stored in an `ant.JSON[T]` column, still reads.
- A JSON column written before keeps the old keys in the database, so a containment or path condition that names a key of an embedded `ant.Base` or a `postgres.Point`, such as `JSONContains` with such a value, does not match those rows until they are written again.

Rejected:

- Leaving `ant.Base` without tags and documenting the three-field version: every user who returns models as JSON would have to repeat it.
- snake_case, as in `created_at`: the owner chose camelCase.
