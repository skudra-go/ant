package change_tracking

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/skudra-go/ant"
	"github.com/skudra-go/ant/tests/features/fixture"
)

// statementCount counts the statements a context runs.
type statementCount struct{ n atomic.Int64 }

func (c *statementCount) Before(ctx context.Context, _ *ant.Statement) (context.Context, error) {
	c.n.Add(1)

	return ctx, nil
}

func (*statementCount) After(context.Context, *ant.Statement, ant.Outcome) {}

// HasChanges says whether SaveChanges would write anything: a changed field,
// and an entity added to, taken out of, or moved between the relations that
// were loaded. It plans the save without running a statement or changing an
// entity.
func TestHasChanges(t *testing.T) {
	fixture.Each(t, func(t *testing.T, target fixture.Target) {
		c := new(statementCount)
		db := fixture.Open[AppDB](t, target, ant.WithTracking(true), ant.WithInterceptor(c))
		ctx := t.Context()
		alice, bob, _ := fixture.Seed(t, db)
		require.NoError(t, db.Posts.InsertOnly(ctx,
			&Post{UserID: alice.ID, Title: "A"},
			&Post{UserID: alice.ID, Title: "B"}))

		load := func(t *testing.T) (*AppDB, *User, *User) {
			t.Helper()

			session := ant.Session(db)
			users, err := session.Users.Include(&U.Posts, byTitle).
				Where(ant.In(&U.ID, alice.ID, bob.ID)).
				OrderBy(&U.Name).
				All(ctx)
			require.NoError(t, err)

			return session, users[0], users[1]
		}

		// pending asserts what HasChanges says, that it ran no statement, and
		// that SaveChanges agrees with it.
		pending := func(t *testing.T, session *AppDB, want bool) {
			t.Helper()

			before := c.n.Load()

			require.Equal(t, want, fixture.HasChanges(t, session))
			require.Equal(t, before, c.n.Load(), "HasChanges ran a statement")

			require.NoError(t, session.SaveChanges(ctx))
			require.Equal(t, want, c.n.Load() > before, "SaveChanges disagreed with HasChanges")
			require.False(t, fixture.HasChanges(t, session), "left after SaveChanges")
		}

		t.Run("nothing changed", func(t *testing.T) {
			session, _, _ := load(t)
			pending(t, session, false)
		})

		t.Run("a field changed", func(t *testing.T) {
			session, a, _ := load(t)
			a.Age++

			pending(t, session, true)
		})

		t.Run("a post appended", func(t *testing.T) {
			session, a, _ := load(t)
			a.Posts = append(a.Posts, Post{Title: "C"})

			pending(t, session, true)
		})

		t.Run("a post taken out", func(t *testing.T) {
			session, a, _ := load(t)
			a.Posts = a.Posts[1:]

			pending(t, session, true)
		})

		t.Run("a post moved to another user", func(t *testing.T) {
			session, a, b := load(t)
			b.Posts = append(b.Posts, a.Posts[0])
			a.Posts = a.Posts[1:]

			pending(t, session, true)

			titles := storedTitles(t, db, bob.ID)
			require.Len(t, titles, 1)
		})

		t.Run("a post's user changed", func(t *testing.T) {
			session := ant.Session(db)
			posts, err := session.Posts.IncludeOne(&P.User).OrderBy(&P.Title).All(ctx)
			require.NoError(t, err)
			require.NotEmpty(t, posts)

			p := posts[0]
			other := alice.ID

			if p.UserID == alice.ID {
				other = bob.ID
			}

			userID := p.UserID
			p.User = &User{Base: ant.Base{ID: other}}

			require.True(t, fixture.HasChanges(t, session))
			require.Equal(t, userID, p.UserID, "HasChanges set the foreign key")
			pending(t, session, true)

			moved, err := db.Posts.NoTracking().Find(ctx, p.ID)
			require.NoError(t, err)
			require.Equal(t, other, moved.UserID)
		})
	})
}
