package developer_tooling

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/skudra-go/ant"
	"github.com/skudra-go/ant/tests/features/fixture"
)

// The Is functions classify database errors without string matching.
func TestErrorKinds(t *testing.T) {
	fixture.Run(t, func(t *testing.T, db *fixture.AppDB) {
		ctx := t.Context()
		alice, _, _ := fixture.Seed(t, db)

		unique := db.Users.Insert(ctx, &User{Name: "x", Email: alice.Email})
		require.True(t, ant.IsUniqueViolation(unique))
		require.False(t, ant.IsForeignKeyViolation(unique))
		require.Equal(t, "23505", ant.SQLState(unique))

		foreign := db.Posts.Insert(ctx, &fixture.Post{UserID: -1, Title: "x"})
		require.True(t, ant.IsForeignKeyViolation(foreign))

		_, notNull := ant.Raw[any](db, `INSERT INTO users (name) VALUES (NULL)`).Exec(ctx)
		require.True(t, ant.IsNotNullViolation(notNull))

		_, err := ant.Raw[any](db, `ALTER TABLE users ADD CHECK (age < 1000)`).Exec(ctx)
		require.NoError(t, err)

		_, check := db.Users.Where(ant.Eq(&Users.ID, alice.ID)).
			Update(ctx, ant.Set(&Users.Age, 2000))
		require.True(t, ant.IsCheckViolation(check))

		require.Empty(t, ant.SQLState(ant.ErrNotFound))
		require.False(t, ant.IsUniqueViolation(nil))
	})
}
