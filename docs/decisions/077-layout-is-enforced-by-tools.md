---
id: 77
title: "Layout is enforced by tools"
area: tooling
status: active
tags: [make-fmt, golines, wsl_v5, nlreturn, antfmt, lint, layout]
issues: []
related: []
changed_by: []
code: [tools/antfmt/format.go, tools/antfmt/main.go, .golangci.yml, Makefile]
tests: [tools/antfmt/format_test.go]
---

# 77. Layout is enforced by tools

**In short:** make fmt applies golines, wsl_v5, nlreturn and tools/antfmt, and make lint fails on anything they would change, enforcing airy Go layout.

## Decision

The owner wants code with room in it: a field per line, a blank line before a `return`, and an `if` kept apart from a statement it does not check. Reviewing that by hand does not last, so `make fmt` does it, and `make lint` fails on anything it would change.

`golines` breaks long lines at 100 columns, and `wsl_v5` and `nlreturn` handle blank lines. `tools/antfmt` covers what they leave: they only look at line length, so a short line of fields passes, and `wsl_v5` lets an `if` sit under a statement it has nothing to do with. antfmt only adds line breaks and trailing commas, so it cannot change what code means. It needs no dependencies, so it stays a small module beside `antvet`.

A literal with four or more keyed fields gets one per line, because three short ones read fine on a line, and test tables would grow a lot if they did not. Positional rows stay on one line for the same reason. Long strings and SQL stay whole.
