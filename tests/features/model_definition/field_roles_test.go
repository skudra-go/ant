package model_definition

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/skudra-go/ant"
	"github.com/skudra-go/ant/tests/features/fixture"
)

// Tags give a field a role in reads and writes.
type Account struct {
	ID       int64
	Email    string
	Password string `ant:"writeonly"`         // written, never read
	Slug     string `ant:"readonly"`          // filled by the database
	Status   string `ant:"default"`           // the column default when empty
	Created  string `ant:"immutable"`         // written once
	Fee      int    `ant:"default,immutable"` // tags combine
}

type AccountDB struct {
	ant.Context
	Accounts ant.Table[Account]
}

var Ac = ant.Fields[Account]()

func TestFieldRoles(t *testing.T) {
	ddl := `CREATE TABLE accounts (
		id BIGSERIAL PRIMARY KEY,
		email TEXT NOT NULL,
		password TEXT,
		slug TEXT GENERATED ALWAYS AS (lower(email)) STORED,
		status TEXT NOT NULL DEFAULT 'new',
		created TEXT NOT NULL,
		fee INT NOT NULL DEFAULT 5)`
	fixture.RunTrackedWith(t, ddl, func(t *testing.T, db *AccountDB) {
		ctx := t.Context()
		account := &Account{Email: "A@X", Password: "secret", Created: "now"}
		require.NoError(t, db.Accounts.Insert(ctx, account))

		stored := func() *Account {
			got, err := db.Accounts.NoTracking().Find(ctx, account.ID)
			require.NoError(t, err)

			return got
		}

		t.Run("readonly is filled from the database on insert", func(t *testing.T) {
			require.Equal(t, "a@x", account.Slug)
		})

		t.Run("default takes the column default when zero", func(t *testing.T) {
			require.Equal(t, "new", account.Status, "copied back to the entity")
			require.Equal(t, 5, account.Fee)
		})

		t.Run("default is overridden by a value", func(t *testing.T) {
			other := &Account{Email: "b", Status: "active", Created: "x"}
			require.NoError(t, db.Accounts.Insert(ctx, other))
			require.Equal(t, "active", other.Status)
		})

		t.Run("InsertMany keeps a value and uses the default otherwise", func(t *testing.T) {
			set, unset := &Account{
				Email:   "c",
				Status:  "set",
				Created: "x",
			}, &Account{
				Email:   "d",
				Created: "x",
			}
			_, err := db.Accounts.InsertMany(ctx, []*Account{set, unset})
			require.NoError(t, err)
			got, err := db.Accounts.NoTracking().Find(ctx, set.ID)
			require.NoError(t, err)
			require.Equal(t, "set", got.Status)
			got, err = db.Accounts.NoTracking().Find(ctx, unset.ID)
			require.NoError(t, err)
			require.Equal(t, "new", got.Status)
		})

		t.Run("writeonly is stored but never read", func(t *testing.T) {
			require.Empty(t, stored().Password)

			hash, err := ant.Raw[string](
				db,
				`SELECT password FROM accounts WHERE id = $1`,
				account.ID,
			).First(ctx)
			require.NoError(t, err)
			require.Equal(t, "secret", *hash)
		})

		t.Run("writeonly can change", func(t *testing.T) {
			_, err := db.Accounts.Where(ant.Eq(&Ac.ID, account.ID)).
				Update(ctx, ant.Set(&Ac.Password, "new"))
			require.NoError(t, err)
		})

		t.Run(
			"a tracked change to immutable or readonly fields writes nothing",
			func(t *testing.T) {
				account.Created = "changed"
				account.Slug = "changed"

				require.False(t, fixture.HasChanges(t, db))
				require.NoError(t, db.SaveChanges(ctx))
				require.Equal(t, "now", stored().Created)
			},
		)
	})
}
