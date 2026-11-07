# Auditing

ant offers four places to record what happened, and which one fits depends on what you need to know.

## What changed, field by field

A `SaveInterceptor` sees every `SaveChanges` before it writes, as whole entities and field names. An error cancels the save, so it also suits rules that span a unit of work.

```go
type AuditTrail struct{ log *slog.Logger }

func (a *AuditTrail) Before(ctx context.Context, _ *ant.Statement) (context.Context, error) {
    return ctx, nil
}

func (a *AuditTrail) After(context.Context, *ant.Statement, ant.Outcome) {}

func (a *AuditTrail) SavingChanges(ctx context.Context, changes []ant.Change) error {
    for _, c := range changes {
        a.log.Info("saving", "model", c.Model, "state", c.State.String(), "fields", c.Changed)
    }
    return nil
}

db, err := ant.Open[AppDB](ctx, postgres.Dialect, dsn, ant.WithInterceptor(&AuditTrail{log}))
```

To record old values too, take the entry with `entry, err := ant.Entry(ctx, db, user)` and read them with `ant.Original(entry, &U.Age)`.

## An audit row in the same transaction

To make a record that cannot disagree with the data, write it in the same transaction as the change. Both commit, or neither does.

```go
err := db.Tx(ctx, func(tx *AppDB) error {
    if err := tx.Orders.Insert(ctx, order); err != nil {
        return err
    }
    return tx.AuditLog.Insert(ctx, &AuditEntry{Action: "order.created", Subject: order.ID})
})
```

A hook runs inside the write too, and an error from an After hook undoes it, but a hook only receives a `context.Context`, with no way to reach the transaction. Use one to check or to refuse, and not to write rows.

## What happened, after it happened

An `Observer` runs after the commit, and never hears about a rollback, which suits notifications and cache invalidation.

```go
ant.Observe(db, ant.Observer[Order]{
    Inserted: func(ctx context.Context, o *Order) { notify(o) },
})
```

An observer runs in the process and is gone if it crashes. For anything that must not be lost, write an outbox row in the transaction instead, as the [ledger example](https://github.com/skudra-go/ant-examples/tree/main/examples/ledger) does, and let a worker send it.

## Every statement

An `Interceptor` sees every statement, including bulk writes and raw SQL, which the others do not. It works in SQL rather than entities.

## Which one

| You need | Use |
| --- | --- |
| Field-level history of `SaveChanges` | `SaveInterceptor` |
| A record that cannot disagree with the data | A row in the same `Tx` |
| A reaction after the commit | `Observer` |
| Delivery that survives a crash | An outbox row and a worker |
| Bulk writes and raw SQL too | `Interceptor`, or a database trigger |

Bulk `Update` and `Delete`, `InsertMany`, and `Raw` run no hooks and no `SaveInterceptor`, because they load no entities. A trigger is the only thing that sees everything, including a person in `psql`.

For a whole audit service, see the [audit example](https://github.com/skudra-go/ant-examples/tree/main/examples/audit).
