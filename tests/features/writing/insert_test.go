package writing

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/skudra-go/ant"
	"github.com/skudra-go/ant/tests/features/fixture"
)

// Insert writes one or several entities. Every field is written as it is, so a
// zero value is stored as zero.
func TestInsert(t *testing.T) {
	fixture.Run(t, func(t *testing.T, db *fixture.AppDB) {
		ctx := t.Context()

		t.Run("one", func(t *testing.T) {
			alice := &fixture.User{Name: "Alice", Email: "alice@example.com", Age: 30}
			require.NoError(t, db.Users.Insert(ctx, alice))
			require.NotZero(t, alice.ID)
		})

		t.Run("several", func(t *testing.T) {
			bob := &fixture.User{Name: "Bob", Email: "bob@example.com"}
			carol := &fixture.User{Name: "Carol", Email: "carol@example.com"}
			require.NoError(t, db.Users.Insert(ctx, bob, carol))
			require.NotZero(t, bob.ID)
			require.NotZero(t, carol.ID)
		})

		t.Run("nothing to insert", func(t *testing.T) {
			require.NoError(t, db.Users.Insert(ctx))
		})

		t.Run("a zero value is stored", func(t *testing.T) {
			zero := &fixture.User{Name: "Zero", Email: "zero@example.com", Age: 0}
			require.NoError(t, db.Users.Insert(ctx, zero))
			found, err := db.Users.Find(ctx, zero.ID)
			require.NoError(t, err)
			require.Zero(t, found.Age)
		})

		t.Run("a slice of pointers spreads", func(t *testing.T) {
			users := []*fixture.User{
				{Name: "D", Email: "d@example.com"},
				{Name: "E", Email: "e@example.com"},
			}
			require.NoError(t, db.Users.Insert(ctx, users...))
		})

		t.Run("a nil entity", func(t *testing.T) {
			require.ErrorIs(t, db.Users.Insert(ctx, nil), ant.ErrNilEntity)
		})

		t.Run("a constraint failure is the database's error", func(t *testing.T) {
			first := &fixture.User{Name: "F", Email: "f@example.com"}
			require.NoError(t, db.Users.Insert(ctx, first))

			copycat := &fixture.User{Name: "G", Email: "f@example.com"}
			require.ErrorContains(t, db.Users.Insert(ctx, copycat), "duplicate key")
			require.Zero(t, copycat.ID)
		})

		t.Run("several are all or nothing", func(t *testing.T) {
			ok := &fixture.User{Name: "H", Email: "h@example.com"}
			clash := &fixture.User{Name: "I", Email: "h@example.com"}
			require.Error(t, db.Users.Insert(ctx, ok, clash))
			require.Zero(t, ok.ID, "the key of a rolled back row is cleared")

			_, err := db.Users.Where(ant.Eq(&Users.Email, "h@example.com")).First(ctx)
			require.ErrorIs(t, err, ant.ErrNotFound)
		})

		t.Run("a cancelled context", func(t *testing.T) {
			cancelled, cancel := context.WithCancel(ctx)
			cancel()

			err := db.Users.Insert(cancelled, &fixture.User{Name: "K", Email: "k@example.com"})
			require.ErrorIs(t, err, context.Canceled)
		})
	})
}
