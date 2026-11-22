---
id: 151
title: "DeletedAt is a time or null in JSON"
area: models
status: active
tags: [DeletedAt, soft-delete, JSON, MarshalJSON]
issues: [299]
related: []
changed_by: []
code: [internal/meta/meta.go]
tests: [json_test.go]
---

# 151. DeletedAt is a time or null in JSON

**In short:** `ant.DeletedAt` marshals to `null` for a live row and to the RFC 3339 time for a deleted one, as a `*time.Time` does. It unmarshals `null`, a time, and the `{"V":...,"Valid":...}` object it was marshalled as before.

## Decision

`DeletedAt` is a struct around `sql.Null[time.Time]`, so `encoding/json` wrote its fields: every live row in a response carried `{"V":"0001-01-01T00:00:00Z","Valid":false}`, and a request body with a time in it failed to decode (#299). A model is often written straight to a response, and soft-deletable models are common, so the shape reached clients.

It now has `MarshalJSON` and `UnmarshalJSON`, with the shape of a `*time.Time`. `null` decodes to a live row, even into a value that held a time, as it sets a `*time.Time` to nil. The old object still decodes, so a client or a stored document that sends it back keeps working. The time keeps the offset it came with; ant writes it in UTC, as it does any time.

This changes what existing responses carry, which a client may parse. The old shape exposed `sql.Null`'s fields, which no API means to publish, so the change is a fix, listed in the release notes.

Rejected: leaving it to a DTO or a `json:"-"` tag. Every soft-deletable model would need one, and the default would stay wrong.
