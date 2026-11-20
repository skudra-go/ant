---
id: 17
title: "Implemented ahead of the skeleton"
area: process
status: historical
tags: [skeleton, ErrNotImplemented, history]
issues: []
related: []
changed_by: []
code: [errors.go, internal/errs/errs.go]
tests: [ant_test.go]
---

# 17. Implemented ahead of the skeleton

**In short:** Records which skeleton bodies were real while the rest returned ErrNotImplemented.

> **Historical.** Skeleton phase is over; ErrNotImplemented remains only for IncludeJoined through a pivot (joined.go:36).

## Decision

Some bodies are real because later behavior or the acceptance criteria depend on them: `Open`, table binding, probe resolution, condition and assignment construction, and builder error accumulation. Everything that runs SQL against a model returns `ErrNotImplemented`.
