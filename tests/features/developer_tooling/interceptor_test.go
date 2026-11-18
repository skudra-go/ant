package developer_tooling

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
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

// A session adds its own interceptor, such as a request's logger, to those of
// the context it came from. Sessions of one context do not see each other's.
func TestSessionInterceptors(t *testing.T) {
	fixture.Each(t, func(t *testing.T, target fixture.Target) {
		ctx := t.Context()

		// However many interceptors the context has: a list with room to grow
		// must not be grown in place by two sessions.
		for shared := range 6 {
			options := make([]ant.Option, shared)
			for i := range options {
				options[i] = ant.WithInterceptor(&recorder{})
			}

			db := fixture.Open[fixture.AppDB](t, target, options...)
			first, second := &recorder{}, &recorder{}
			a := ant.Session(db, ant.WithInterceptor(first))
			b := ant.Session(db, ant.WithInterceptor(second))

			count := func(tx *fixture.AppDB) error {
				_, err := tx.Users.Count(ctx)

				return err
			}

			require.NoError(t, a.Tx(ctx, count))
			require.Len(t, first.statements(), 1, "%d shared", shared)
			require.Empty(t, second.statements(), "%d shared", shared)

			require.NoError(t, b.Tx(ctx, count))
			require.Len(t, first.statements(), 1)
			require.Len(t, second.statements(), 1)
		}

		// A per-request logger sees the whole request, not only its transactions,
		// and the context's own interceptors still see the session's statements.
		t.Run("outside a transaction too", func(t *testing.T) {
			shared := &recorder{}
			db := fixture.Open[fixture.AppDB](t, target, ant.WithInterceptor(shared))
			alice, _, _ := fixture.Seed(t, db)

			var logs bytes.Buffer

			mine := &recorder{}
			s := ant.Session(
				db,
				ant.WithLogger(slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug}))),
				ant.WithInterceptor(mine),
			)

			shared.reset()

			steps := map[string]func() error{
				"Count": func() error {
					_, err := s.Users.Count(ctx)

					return err
				},
				"First": func() error {
					_, err := s.Users.OrderBy(&Users.ID).First(ctx)

					return err
				},
				"Insert": func() error {
					return s.Posts.Insert(ctx, &fixture.Post{UserID: alice.ID, Title: "s"})
				},
				"Update": func() error {
					_, err := s.Users.Where(ant.Eq(&Users.ID, alice.ID)).
						Update(ctx, ant.Set(&Users.Age, 31))

					return err
				},
				"Raw": func() error {
					_, err := ant.Raw[int](s, "SELECT 1").All(ctx)

					return err
				},
			}

			for name, step := range steps {
				before := len(mine.statements())

				require.NoError(t, step(), name)
				require.Greater(t, len(mine.statements()), before, name)
			}

			require.Equal(t, mine.statements(), shared.statements())

			require.Equal(
				t,
				len(mine.statements()),
				strings.Count(logs.String(), "ant: statement"),
				"the session's logger too",
			)

			_, err := db.Users.Count(ctx)
			require.NoError(t, err)
			require.Len(t, shared.statements(), len(mine.statements())+1, "the root's are its own")
		})

		// A session made inside a transaction, or inside a savepoint that Begin
		// opened there, sees the statements it runs in it.
		t.Run("inside a transaction and a savepoint", func(t *testing.T) {
			db := fixture.Open[fixture.AppDB](t, target)
			mine, nested := &recorder{}, &recorder{}

			var afterRelease error

			require.NoError(t, db.Tx(ctx, func(tx *fixture.AppDB) error {
				if _, err := ant.Session(tx, ant.WithInterceptor(mine)).Users.Count(ctx); err != nil {
					return err
				}

				sp, err := ant.Begin(ctx, tx)
				if err != nil {
					return err
				}

				inner := ant.Session(sp, ant.WithInterceptor(nested))
				if _, err := inner.Users.Count(ctx); err != nil {
					return err
				}

				if err := sp.Commit(); err != nil {
					return err
				}

				_, afterRelease = inner.Users.Count(ctx)

				return nil
			}))

			require.Len(t, mine.statements(), 1)
			require.Len(t, nested.statements(), 1)
			require.Error(t, afterRelease, "a released savepoint still refuses the session's statements")
		})
	})
}
