package model_definition

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/skudra-go/ant"
	"github.com/skudra-go/ant/tests/features/fixture"
)

// `ant:"-"` and unexported fields are not columns.
type Session struct {
	ID     int64
	Token  string
	Cached string `ant:"-"`
	secret string
}

type SessionDB struct {
	ant.Context
	Sessions ant.Table[Session]
}

var Sessions = ant.Fields[Session]()

func TestExcludeField(t *testing.T) {
	ddl := `CREATE TABLE sessions (id BIGSERIAL PRIMARY KEY, token TEXT NOT NULL)`
	fixture.RunWith(t, ddl, func(t *testing.T, db *SessionDB) {
		ctx := t.Context()

		t.Run("excluded fields are not written or read", func(t *testing.T) {
			session := &Session{Token: "abc", Cached: "kept in memory", secret: "kept in memory"}
			require.NoError(t, db.Sessions.Insert(ctx, session))

			found, err := db.Sessions.Find(ctx, session.ID)
			require.NoError(t, err)
			require.Equal(t, "abc", found.Token)
			require.Empty(t, found.Cached)
			require.Empty(t, found.secret)
		})

		t.Run("an excluded field cannot be queried", func(t *testing.T) {
			_, err := db.Sessions.Where(ant.Eq(&Sessions.Cached, "x")).All(ctx)
			require.ErrorIs(t, err, ant.ErrNotProbe)
		})
	})
}
