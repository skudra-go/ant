package transactions

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/skudra-go/ant"
	"github.com/skudra-go/ant/tests/features/fixture"
)

// Accounts is a service built at startup. It holds the root, not a transaction.
type Accounts struct {
	db *fixture.AppDB
}

func (a Accounts) Open(ctx context.Context, name string) error {
	return a.db.Users.Insert(
		ctx,
		&fixture.User{
			Name:  name,
			Email: name + "@example.com",
		},
	)
}

// Tx passes on the caller's ctx, so a service that holds the root writes outside
// the transaction. ant.InTx binds the ctx to it, and then the service's writes
// commit or roll back with the transaction.
func TestInTx(t *testing.T) {
	fixture.Run(t, func(t *testing.T, db *fixture.AppDB) {
		ctx := t.Context()
		accounts := Accounts{db: db}

		err := db.Tx(ctx, func(tx *fixture.AppDB) error {
			ctx := ant.InTx(ctx, tx)

			if err := accounts.Open(ctx, "temp"); err != nil {
				return err
			}

			return errStop // rolls back the service's insert too
		})
		require.ErrorIs(t, err, errStop)

		count, err := db.Users.Count(ctx)
		require.NoError(t, err)
		require.Zero(t, count)
	})
}
