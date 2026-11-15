package tests

import (
	"context"
	"database/sql"
	"errors"
	"slices"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/skudra-go/ant"
	"github.com/skudra-go/ant/internal/testdb"
)

func TestTxCommits(t *testing.T) {
	testdb.ForEach(t, func(t *testing.T, target testdb.Target) {
		app := openApp(t, target)
		err := app.Tx(t.Context(), func(tx *AppDB) error {
			bob := &User{Name: "Bob", Email: "bob@example.com"}
			if err := tx.Users.Insert(t.Context(), bob); err != nil {
				return err
			}

			return tx.Posts.Insert(t.Context(), &Post{UserID: bob.ID, Title: "First!"})
		})
		require.NoError(t, err)
		require.Equal(t, 1, countRows(t, target.DB, "users"))
		require.Equal(t, 1, countRows(t, target.DB, "posts"))
	})
}

func TestTxRollsBack(t *testing.T) {
	testdb.ForEach(t, func(t *testing.T, target testdb.Target) {
		app := openApp(t, target)
		boom := errors.New("boom")
		err := app.Tx(t.Context(), func(tx *AppDB) error {
			if err := tx.Users.Insert(
				t.Context(),
				&User{Name: "Bob", Email: "bob@example.com"},
			); err != nil {
				return err
			}

			return boom
		})
		require.ErrorIs(t, err, boom)
		require.Zero(t, countRows(t, target.DB, "users"))
	})
}

// touchingPost is a post whose writes touch its user's UpdatedAt.
type touchingPost struct {
	ant.Base
	UserID    int64
	Title     string
	Published bool
	User      *User
}

func (touchingPost) Configure(b *ant.ModelBuilder[touchingPost]) {
	b.Table("posts")
	b.BelongsTo(&touchingPosts.User).Touch()
}

var touchingPosts = ant.Fields[touchingPost]()

type touchDB struct {
	ant.Context
	Users ant.Table[User]
	Posts ant.Table[touchingPost]
}

// failTouch fails the UPDATE of users that Touch runs, while armed.
type failTouch struct {
	armed atomic.Bool
}

var errTouch = errors.New("touch failed")

func (f *failTouch) Before(
	ctx context.Context,
	s *ant.Statement,
) (context.Context, error) {
	if f.armed.Load() && strings.HasPrefix(s.SQL, "UPDATE") && strings.Contains(s.SQL, "users") {
		return ctx, errTouch
	}

	return ctx, nil
}

func (f *failTouch) After(context.Context, *ant.Statement, ant.Outcome) {}

// A single write to a model with a Touch relation is two statements, the write
// and the parent's UPDATE, so a failed touch undoes the write.
func TestTxTouchFailureUndoesWrite(t *testing.T) {
	testdb.ForEach(t, func(t *testing.T, target testdb.Target) {
		ctx := t.Context()
		f := &failTouch{}
		db, err := ant.Open[touchDB](
			ctx,
			target.Dialect,
			target.DSN,
			ant.WithTracking(true),
			ant.WithInterceptor(f),
		)
		require.NoError(t, err)
		t.Cleanup(func() { db.Close() })

		user := &User{Name: "Bob", Email: "bob@example.com"}
		require.NoError(t, db.Users.Insert(ctx, user))

		// Insert: the row is not left behind, and a retry stores it once.
		post := &touchingPost{UserID: user.ID, Title: "old"}

		f.armed.Store(true)
		require.ErrorIs(t, db.Posts.Insert(ctx, post), errTouch)
		require.Zero(t, post.ID)
		require.Zero(t, countRows(t, target.DB, "posts"))

		f.armed.Store(false)
		require.NoError(t, db.Posts.Insert(ctx, post))
		require.Equal(t, 1, countRows(t, target.DB, "posts"))

		// SaveChanges on an update: the stored title stays, and a retry saves it.
		post.Title = "new"

		f.armed.Store(true)
		require.ErrorIs(t, db.SaveChanges(ctx), errTouch)

		stored, err := db.Posts.NoTracking().Find(ctx, post.ID)
		require.NoError(t, err)
		require.Equal(t, "old", stored.Title)

		f.armed.Store(false)
		require.NoError(t, db.SaveChanges(ctx))

		stored, err = db.Posts.NoTracking().Find(ctx, post.ID)
		require.NoError(t, err)
		require.Equal(t, "new", stored.Title)

		// SaveChanges on a delete: the row stays.
		require.NoError(t, ant.Remove(ctx, db, post))
		f.armed.Store(true)
		require.ErrorIs(t, db.SaveChanges(ctx), errTouch)
		require.Equal(t, 1, countRows(t, target.DB, "posts"))

		f.armed.Store(false)
		require.NoError(t, db.SaveChanges(ctx))
		require.Zero(t, countRows(t, target.DB, "posts"))
	})
}

// Begin inside a transaction opens a savepoint, so the inner Commit and
// Rollback end only the inner part, as a nested Tx does.
func TestTxNestedBegin(t *testing.T) {
	testdb.ForEach(t, func(t *testing.T, target testdb.Target) {
		ctx := t.Context()
		app := openApp(t, target)
		stop := errors.New("stop")

		insert := func(db *AppDB, name string) {
			t.Helper()
			require.NoError(t, db.Users.Insert(ctx, &User{Name: name, Email: name + "@x"}))
		}

		// The inner Commit keeps the inner work in the outer transaction, whose
		// rollback still undoes it.
		err := app.Tx(ctx, func(tx *AppDB) error {
			inner, err := ant.Begin(ctx, tx)
			require.NoError(t, err)
			insert(inner, "inner")
			require.NoError(t, inner.Commit())
			require.ErrorIs(t, inner.Commit(), ant.ErrTxDone)
			require.ErrorIs(t, inner.Rollback(), ant.ErrTxDone)

			// Ended, the inner context no longer runs statements.
			_, err = inner.Users.Count(ctx)
			require.ErrorIs(t, err, ant.ErrTxDone)
			insert(tx, "outer")

			return stop
		})
		require.ErrorIs(t, err, stop)
		require.Zero(t, countRows(t, target.DB, "users"))

		// The inner Rollback undoes the inner work alone.
		err = app.Tx(ctx, func(tx *AppDB) error {
			insert(tx, "before")

			inner, err := ant.Begin(ctx, tx)
			require.NoError(t, err)

			undone := &User{Name: "undone", Email: "undone@x"}
			require.NoError(t, inner.Users.Insert(ctx, undone))
			require.NoError(t, inner.Rollback())
			require.Zero(t, undone.ID, "the rollback takes back the generated key")
			require.ErrorIs(t, inner.Commit(), ant.ErrTxDone)
			insert(tx, "after")

			return nil
		})
		require.NoError(t, err)

		found, err := app.Users.OrderBy(&Users.Name).All(ctx)
		require.NoError(t, err)
		require.Equal(t, []string{"after", "before"}, names(found))

		// From Begin inside Begin, and the outer transaction then commits both.
		outer, err := ant.Begin(ctx, app)
		require.NoError(t, err)

		inner, err := ant.Begin(ctx, outer)
		require.NoError(t, err)
		insert(inner, "nested")
		require.NoError(t, inner.Commit())
		require.NoError(t, outer.Commit())
		require.Equal(t, 3, countRows(t, target.DB, "users"))
	})
}

// With WithoutAutoTx, the rows a failed save wrote before the failure stay, and
// the tracker records them, so a retry writes only the rest.
func TestWithoutAutoTxRetrySavesTheRest(t *testing.T) {
	testdb.ForEach(t, func(t *testing.T, target testdb.Target) {
		ctx := t.Context()

		app, err := ant.Open[AppDB](ctx, target.Dialect, target.DSN, ant.WithoutAutoTx())
		require.NoError(t, err)
		t.Cleanup(func() { app.Close() })

		taken := &User{Name: "taken", Email: "taken@x"}
		require.NoError(t, app.Users.Insert(ctx, taken))

		first := &User{Name: "first", Email: "first@x"}
		clash := &User{Name: "clash", Email: "taken@x"}

		require.NoError(t, ant.Add(ctx, app, first))
		require.NoError(t, ant.Add(ctx, app, clash))

		require.Error(t, app.SaveChanges(ctx))
		require.Equal(t, 2, countRows(t, target.DB, "users"), "the first insert stays")
		require.NotZero(t, first.ID, "and keeps its key")

		entry, err := ant.Entry(ctx, app, first)
		require.NoError(t, err)
		require.Equal(t, ant.Unchanged, entry.State(), "the tracker knows it was saved")

		clash.Email = "clash@x"

		require.NoError(t, app.SaveChanges(ctx))
		require.Equal(t, 3, countRows(t, target.DB, "users"), "the retry inserts only the rest")
	})
}

// endsTx ends the transaction on the server, as a deadlock does on MySQL and SQL
// Server, just before the INSERT that writes match, and fails that INSERT.
type endsTx struct {
	match string
	tx    atomic.Pointer[sql.Tx]
}

var errDeadlock = errors.New("deadlock")

func (e *endsTx) Before(ctx context.Context, s *ant.Statement) (context.Context, error) {
	tx := e.tx.Load()
	if tx == nil || !strings.HasPrefix(s.SQL, "INSERT") || !slices.Contains(s.Args, any(e.match)) {
		return ctx, nil
	}

	e.tx.Store(nil)

	if _, err := tx.ExecContext(ctx, "ROLLBACK"); err != nil {
		return ctx, err
	}

	return ctx, errDeadlock
}

func (*endsTx) After(context.Context, *ant.Statement, ant.Outcome) {}

// When the database ends the whole transaction under a save's savepoint, as a
// deadlock does on MySQL and SQL Server, the transaction is over: ant refuses
// to run anything more in it, rather than running it outside any transaction,
// and the saves made in it before are undone in the tracker.
func TestSavepointLostEndsTx(t *testing.T) {
	testdb.ForEach(t, func(t *testing.T, target testdb.Target) {
		ctx := t.Context()
		ends := &endsTx{match: "Second"}

		app, err := ant.Open[AppDB](ctx, target.Dialect, target.DSN, ant.WithInterceptor(ends))
		require.NoError(t, err)
		t.Cleanup(func() { app.Close() })

		first := &User{Name: "First", Email: "first@x"}
		second := &User{Name: "Second", Email: "second@x"}

		var retried error

		err = app.Tx(ctx, func(tx *AppDB) error {
			if err := tx.Users.Insert(ctx, first); err != nil {
				return err
			}

			ends.tx.Store(ant.CurrentTx(tx))

			require.NoError(t, ant.Add(ctx, tx, second))
			require.ErrorIs(t, tx.SaveChanges(ctx), errDeadlock)

			// The caller retries, as decision 31 allows, and moves on.
			retried = tx.SaveChanges(ctx)

			return nil
		})

		require.ErrorIs(t, retried, ant.ErrTxDone, "the retry ran outside the transaction")
		require.ErrorIs(t, err, ant.ErrTxDone, "the lost transaction committed")
		require.Zero(t, countRows(t, target.DB, "users"))
		require.Zero(t, first.ID, "the lost insert keeps no key")
	})
}
