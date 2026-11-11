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

var Accounts = ant.Fields[Account]()

// AccountRow is a source row for Merge.
type AccountRow struct {
	ID int64
}

var AccountRows = ant.Fields[AccountRow]()

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
			_, err := db.Accounts.Where(ant.Eq(&Accounts.ID, account.ID)).
				Update(ctx, ant.Set(&Accounts.Password, "new"))
			require.NoError(t, err)
		})

		// Fields ant never updates are refused by name in an explicit write too,
		// before anything runs, rather than changed behind the tag's back.
		t.Run("an explicit write refuses immutable and readonly fields", func(t *testing.T) {
			byID := db.Accounts.Where(ant.Eq(&Accounts.ID, account.ID))

			_, err := byID.Update(ctx, ant.Set(&Accounts.Created, "rewritten"))
			require.ErrorContains(t, err, "Account.Created is immutable")

			_, err = byID.Update(ctx, ant.Set(&Accounts.Slug, "rewritten"))
			require.ErrorContains(t, err, "Account.Slug is readonly")

			_, err = db.Accounts.UpdateMany(ctx, []*Account{account}, ant.F(&Accounts.Created))
			require.ErrorContains(t, err, "Account.Created is immutable")

			_, err = db.Accounts.Upsert(
				ctx,
				&Account{ID: account.ID, Email: "A@X", Created: "x"},
				ant.OnConflict(&Accounts.ID).DoUpdate(ant.F(&Accounts.Created)),
			)
			require.ErrorContains(t, err, "Account.Created is immutable")

			_, err = db.Accounts.Upsert(
				ctx,
				&Account{ID: account.ID, Email: "A@X", Created: "x"},
				ant.OnConflict(&Accounts.ID).Set(ant.Set(&Accounts.Fee, 9)),
			)
			require.ErrorContains(t, err, "Account.Fee is immutable")

			source := ant.Values(db, AccountRow{ID: account.ID})

			_, err = ant.Merge(&db.Accounts, source, ant.EqCol(&Accounts.ID, &AccountRows.ID)).
				WhenMatched(ant.Set(&Accounts.Created, "rewritten")).
				Exec(ctx)
			require.ErrorContains(t, err, "Account.Created is immutable")

			require.Equal(t, "now", stored().Created)
		})

		t.Run("an upsert leaves immutable fields out of its update", func(t *testing.T) {
			_, err := db.Accounts.Upsert(
				ctx,
				&Account{ID: account.ID, Email: "A@X", Created: "upserted"},
				ant.OnConflict(&Accounts.ID),
			)
			require.NoError(t, err)
			require.Equal(t, "now", stored().Created)
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
