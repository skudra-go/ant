package developer_tooling

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/skudra-go/ant"
	"github.com/skudra-go/ant/tests/features/fixture"
)

// An interceptor sees every statement, which is how to add metrics or tracing.
func TestInterceptor(t *testing.T) {
	fixture.Each(t, func(t *testing.T, target fixture.Target) {
		ctx := t.Context()
		rec := &recorder{}
		db := fixture.Open[fixture.AppDB](t, target, ant.WithInterceptor(rec))
		alice, _, _ := fixture.Seed(t, db)
		require.NoError(t, db.Posts.Insert(ctx, &fixture.Post{UserID: alice.ID, Title: "x"}))

		t.Run("reads and writes", func(t *testing.T) {
			rec.reset()

			_, err := db.Users.All(ctx)
			require.NoError(t, err)
			require.Len(t, rec.statements(), 1)
			require.Contains(t, rec.statements()[0], `SELECT`)
		})

		t.Run("Include runs exactly one extra statement", func(t *testing.T) {
			rec.reset()

			_, err := db.Users.Include(&Users.Posts).All(ctx)
			require.NoError(t, err)
			require.Len(t, rec.statements(), 2, "not one per user")
		})

		t.Run("statements in transactions", func(t *testing.T) {
			rec.reset()

			require.NoError(t, db.Tx(ctx, func(tx *fixture.AppDB) error {
				_, err := tx.Users.All(ctx)

				return err
			}))

			require.Len(t, rec.statements(), 1)
		})

		t.Run("outcomes report errors", func(t *testing.T) {
			rec.reset()

			_, err := ant.Raw[any](db, `SELEC`).Exec(ctx)
			require.Error(t, err)
			require.Error(t, rec.done[0].Err)
		})

		t.Run("Before can refuse a statement", func(t *testing.T) {
			rec.refuse = errors.New("read-only mode")

			defer func() { rec.refuse = nil }()

			_, err := db.Users.All(ctx)
			require.ErrorContains(t, err, "read-only mode")
		})

		t.Run("Before can rewrite one", func(t *testing.T) {
			db := fixture.Open[fixture.AppDB](t, target, ant.WithInterceptor(rewriter{}))
			n, err := ant.Raw[int](db, `SELECT 1`).First(ctx)
			require.NoError(t, err)
			require.Equal(t, 2, *n)
		})
	})
}

type rewriter struct{}

func (rewriter) Before(ctx context.Context, s *ant.Statement) (context.Context, error) {
	s.SQL = `SELECT 2`

	return ctx, nil
}

func (rewriter) After(context.Context, *ant.Statement, ant.Outcome) {}

// Writing nothing costs nothing: an unchanged save sends no statement at all.
func TestSaveChangesStatements(t *testing.T) {
	fixture.Each(t, func(t *testing.T, target fixture.Target) {
		ctx := t.Context()
		rec := &recorder{}
		db := fixture.Open[fixture.AppDB](
			t,
			target,
			ant.WithTracking(true),
			ant.WithInterceptor(rec),
		)
		alice, _, _ := fixture.Seed(t, db)
		require.NoError(t, db.Posts.Insert(ctx, &fixture.Post{UserID: alice.ID, Title: "x"}))

		session := ant.Session(db)
		user, err := session.Users.Include(&Users.Posts).Find(ctx, alice.ID)
		require.NoError(t, err)

		rec.reset()
		require.NoError(t, session.SaveChanges(ctx))
		require.Empty(t, rec.statements())

		user.Age = 0

		rec.reset()
		require.NoError(t, session.SaveChanges(ctx))
		require.Len(t, rec.statements(), 1)
		require.Contains(t, rec.statements()[0], `"age"`)
		require.NotContains(t, rec.statements()[0], `"name"`, "only the changed column")
	})
}

// audit is an Interceptor that also sees each SaveChanges.
type audit struct {
	rewriter
	changes []ant.Change
	refuse  error
}

func (a *audit) Before(ctx context.Context, _ *ant.Statement) (context.Context, error) {
	return ctx, nil
}

func (a *audit) SavingChanges(_ context.Context, changes []ant.Change) error {
	a.changes = append(a.changes, changes...)

	return a.refuse
}

func TestSaveInterceptor(t *testing.T) {
	fixture.Each(t, func(t *testing.T, target fixture.Target) {
		ctx := t.Context()
		log := &audit{}
		db := fixture.Open[fixture.AppDB](
			t,
			target,
			ant.WithTracking(true),
			ant.WithInterceptor(log),
		)

		user := &User{Name: "a", Email: "a"}
		require.NoError(t, db.Users.Insert(ctx, user))
		require.Equal(t, ant.Added, log.changes[0].State)
		require.Equal(t, "User", log.changes[0].Model)

		log.changes = nil
		user.Age = 5

		require.NoError(t, db.SaveChanges(ctx))
		require.Equal(t, ant.Modified, log.changes[0].State)
		require.Equal(t, []string{"Age"}, log.changes[0].Changed)
		require.Same(t, user, log.changes[0].Entity)

		log.changes = nil

		require.NoError(t, db.SaveChanges(ctx))
		require.Empty(t, log.changes, "nothing to report")

		log.refuse = errors.New("audit store is down")
		user.Age = 6

		require.ErrorContains(t, db.SaveChanges(ctx), "audit store is down")

		log.refuse = nil
		got, err := db.Users.NoTracking().Find(ctx, user.ID)
		require.NoError(t, err)
		require.Equal(t, 5, got.Age)
	})
}
