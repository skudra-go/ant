package model_definition

import (
	"database/sql"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/skudra-go/ant"
	"github.com/skudra-go/ant/tests/features/fixture"
)

// A nullable column is a pointer or sql.Null[T]. A plain field cannot hold
// NULL, so ant never has to guess what a zero value means.
type Profile struct {
	ID      int64
	Bio     *string
	Age     sql.Null[int64]
	Website string
}

type ProfileDB struct {
	ant.Context
	Profiles ant.Table[Profile]
}

var Profiles = ant.Fields[Profile]()

func TestNullable(t *testing.T) {
	ddl := `CREATE TABLE profiles (id BIGSERIAL PRIMARY KEY, bio TEXT, age BIGINT, website TEXT)`
	fixture.RunWith(t, ddl, func(t *testing.T, db *ProfileDB) {
		ctx := t.Context()

		t.Run("values round trip", func(t *testing.T) {
			p := &Profile{Bio: new("Gopher"), Age: sql.Null[int64]{V: 30, Valid: true}}
			require.NoError(t, db.Profiles.Insert(ctx, p))

			found, err := db.Profiles.Find(ctx, p.ID)
			require.NoError(t, err)
			require.Equal(t, "Gopher", *found.Bio)
			require.EqualValues(t, 30, found.Age.V)
			require.True(t, found.Age.Valid)
		})

		t.Run("NULL round trips", func(t *testing.T) {
			p := &Profile{}
			require.NoError(t, db.Profiles.Insert(ctx, p))

			found, err := db.Profiles.Find(ctx, p.ID)
			require.NoError(t, err)
			require.Nil(t, found.Bio)
			require.False(t, found.Age.Valid)
		})

		t.Run("empty is not NULL", func(t *testing.T) {
			p := &Profile{Bio: new("")}
			require.NoError(t, db.Profiles.Insert(ctx, p))

			found, err := db.Profiles.Find(ctx, p.ID)
			require.NoError(t, err)
			require.NotNil(t, found.Bio)
			require.Empty(t, *found.Bio)
		})

		t.Run("conditions treat nil and invalid as NULL", func(t *testing.T) {
			p := &Profile{Website: "nil-check"}
			require.NoError(t, db.Profiles.Insert(ctx, p))

			_, err := db.Profiles.Where(ant.Eq(&Profiles.Website, "nil-check"), ant.Eq(&Profiles.Bio, nil)).
				Single(ctx)
			require.NoError(t, err)

			_, err = db.Profiles.Where(
				ant.Eq(&Profiles.Website, "nil-check"),
				ant.Eq(&Profiles.Age, sql.Null[int64]{}),
			).
				Single(ctx)
			require.NoError(t, err)
		})

		t.Run("NULL in a plain field is an error", func(t *testing.T) {
			_, err := ant.Raw[Profile](db, `SELECT NULL::text AS website`).First(ctx)
			require.Error(t, err)
		})
	})
}
