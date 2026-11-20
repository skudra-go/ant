---
id: 109
title: "Numbers inside an interface read as json.Number"
area: values
status: active
tags: [json.Number, UseNumber, JSON, secure.Value, float64, snowflake]
issues: [201]
related: []
changed_by: []
code: [internal/jsontext/jsontext.go, json.go, secure/secure.go]
tests: [json_test.go, tests/features/value_handling/json_test.go]
---

# 109. Numbers inside an interface read as json.Number

**In short:** ant.JSON and secure.Value decode with UseNumber, so numbers inside interfaces are json.Number and keep integers past 2^53 exactly.

## Decision

`ant.JSON` and `secure.Value` decoded with `json.Unmarshal`, which reads every number inside an interface, as in a `map[string]any`, as a `float64`. A `float64` holds integers exactly only up to 2^53, so snowflake IDs and 64-bit order numbers lost their low digits, and since a change to any key writes the whole document, a tracked save stored the rounded value (#201). Both now decode with `UseNumber`, from the database and in `UnmarshalJSON` alike, so such a number is a `json.Number`, which writes back exactly as it was read. A typed field, such as an `int64` or a `float64`, decodes as before. Tracking compares a `json.Number` with a number of a predeclared type as stored, by the text JSON writes for each, so putting `2.0` where a loaded map held `json.Number("2")` is no change, and a `float64` that rounds a larger integer is one. Reading numbers as `float64` when they fit and as `json.Number` otherwise was rejected: one map would then hold both types, and code would have to check each value. `anttest` fixtures decoded with `UseNumber` already.
