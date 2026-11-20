---
id: 2
title: "`Tx` is a generic method on `Context`"
area: transactions
status: active
tags: [Tx, generic-method, Context, ErrTxType]
issues: []
related: []
changed_by: []
code: [tx.go]
tests: [tests/features/transactions/tx_test.go, tests/tx_test.go]
---

# 2. `Tx` is a generic method on `Context`

**In short:** Tx is a generic method on Context that infers the user's context type from the closure and returns ErrTxType on mismatch.

## Decision

`db.Tx(ctx, func(tx *AppDB) error {...})` must hand back the user's type, but `Tx` is promoted from the embedded `ant.Context`, which does not know that type.

Decision: `func (c *Context) Tx[DB any](ctx context.Context, f func(*DB) error) error`. `DB` is inferred from the closure, and promotion through embedding compiles. A closure with the wrong context type is a runtime error returned by `Tx`.
