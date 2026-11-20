---
id: 65
title: "Scoped relations use the same filter path"
area: relations
status: active
tags: [Where, Morph, Latest, Oldest, ErrNoForeignKey, polymorphic, Include, Has]
issues: [198]
related: [44]
changed_by: []
code: [config.go, internal/meta/meta.go, include.go, errors.go]
tests: [tests/features/relations/ambiguous_test.go, tests/features/relations/advanced_test.go]
---

# 65. Scoped relations use the same filter path

**In short:** Relation Where, Morph, Latest and Oldest add their conditions in Include, Has and joins, and two unscoped relations sharing a foreign key by convention return ErrNoForeignKey.

## Decision

`.Where` on a relation, `.Morph`, `.Latest`, and `.Oldest` all add conditions wherever the relation is used, in `Include`, `Has`, and joins. A polymorphic relation has no foreign key, since one column cannot point at several tables.

Two relations of one model to the same target cannot both be found by convention: `Posts []Article` and `Edited []Article` would both take `Article.AuthorID`, so `Edited` would load, and on save delete, the articles the author wrote (#198). That is `ErrNoForeignKey` naming both, wherever either is used and when the schema is built, until one of them sets `ForeignKey`. A relation that scopes or picks rows with `.Where`, `.Latest`, or `.Oldest` shares the key on purpose and is not refused.
