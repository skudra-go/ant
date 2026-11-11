package transactions

import (
	"context"
	"database/sql"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/skudra-go/ant"
	"github.com/skudra-go/ant/tests/features/fixture"
)

// counter counts statements by where they ran.
type counter struct {
	mu                sync.Mutex
	primary, replicas int
}

func (c *counter) Before(ctx context.Context, s *ant.Statement) (context.Context, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if s.Replica {
		c.replicas++
	} else {
		c.primary++
	}

	return ctx, nil
}

func (c *counter) After(context.Context, *ant.Statement, ant.Outcome) {}

func (c *counter) take() (primary, replicas int) {
	c.mu.Lock()
	defer c.mu.Unlock()

	primary, replicas = c.primary, c.replicas
	c.primary, c.replicas = 0, 0

	return primary, replicas
}

// Reads go to replicas, and anything that must see the latest data does not.
func TestReplicas(t *testing.T) {
	fixture.Each(t, func(t *testing.T, target fixture.Target) {
		ctx := t.Context()
		replica, err := sql.Open(target.Dialect.DriverName(), target.DSN)
		require.NoError(t, err)

		seen := &counter{}
		db := fixture.Open[AppDB](t, target, ant.WithReplicas(replica), ant.WithInterceptor(seen))
		fixture.Seed(t, db)
		seen.take()

		t.Run("a read uses a replica", func(t *testing.T) {
			_, err := db.Users.All(ctx)
			require.NoError(t, err)

			primary, replicas := seen.take()
			require.Zero(t, primary)
			require.Equal(t, 1, replicas)
		})

		t.Run("writes use the primary", func(t *testing.T) {
			require.NoError(t, db.Users.Insert(ctx, user("write")))

			_, err := db.Users.Where(ant.Eq(&Users.Name, "write")).
				Update(ctx, ant.Set(&Users.Age, 1))
			require.NoError(t, err)

			_, replicas := seen.take()
			require.Zero(t, replicas)
		})

		t.Run("a lock uses the primary", func(t *testing.T) {
			_, err := db.Users.Where().ForUpdate().All(ctx)
			require.NoError(t, err)

			_, replicas := seen.take()
			require.Zero(t, replicas)
		})

		t.Run("a transaction uses the primary", func(t *testing.T) {
			require.NoError(t, db.Tx(ctx, func(tx *AppDB) error {
				_, err := tx.Users.All(ctx)

				return err
			}))

			_, replicas := seen.take()
			require.Zero(t, replicas)
		})

		t.Run("OnPrimary asks for it", func(t *testing.T) {
			_, err := db.Users.All(ant.OnPrimary(ctx))
			require.NoError(t, err)

			_, replicas := seen.take()
			require.Zero(t, replicas)
		})
	})
}

// With WithStickyWrites, a context reads its own writes.
func TestStickyWrites(t *testing.T) {
	fixture.Each(t, func(t *testing.T, target fixture.Target) {
		ctx := t.Context()
		replica, err := sql.Open(target.Dialect.DriverName(), target.DSN)
		require.NoError(t, err)

		seen := &counter{}
		db := fixture.Open[AppDB](
			t,
			target,
			ant.WithReplicas(replica),
			ant.WithInterceptor(seen),
			ant.WithStickyWrites(time.Hour),
		)

		seen.take()

		t.Run("before any write, reads use the replica", func(t *testing.T) {
			_, err := db.Users.All(ctx)
			require.NoError(t, err)

			_, replicas := seen.take()
			require.Equal(t, 1, replicas)
		})

		t.Run("after a write, they use the primary", func(t *testing.T) {
			require.NoError(t, db.Users.Insert(ctx, user("sticky")))
			seen.take()

			_, err := db.Users.All(ctx)
			require.NoError(t, err)

			_, replicas := seen.take()
			require.Zero(t, replicas)
		})

		t.Run("another session has not written", func(t *testing.T) {
			_, err := ant.Session(db).Users.All(ctx)
			require.NoError(t, err)

			_, replicas := seen.take()
			require.Equal(t, 1, replicas)
		})
	})
}
