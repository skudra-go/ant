package keys_and_identity

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/skudra-go/ant"
	"github.com/skudra-go/ant/tests/features/fixture"
)

// The database generates integer keys only. A key of another type is the
// application's to give, or ant.GenerateKey makes it in Go, or a database
// default fills it. Inserting one at its zero value is an error, because the
// next insert of it would collide.
type Slug string

type Page struct {
	ID    Slug
	Title string
}

type Account struct {
	ID    string // a UUID column
	Owner string
}

// Doc's uuid.UUID key is made in Go, by uuid.New, when it is zero.
type Doc struct {
	ID    uuid.UUID
	Title string
}

func (Doc) Configure(b *ant.ModelBuilder[Doc]) { ant.GenerateKey(b, &Docs.ID, uuid.New) }

// Draft's key is the database's default, read back after the insert.
type Draft struct {
	ID    uuid.UUID `ant:"default=gen_random_uuid()"`
	Title string
}

var Docs = ant.Fields[Doc]()

type SiteDB struct {
	ant.Context
	Pages    ant.Table[Page]
	Accounts ant.Table[Account]
	Docs     ant.Table[Doc]
	Drafts   ant.Table[Draft]
}

func TestStringAndUUIDKeys(t *testing.T) {
	ddl := `CREATE TABLE pages (id TEXT PRIMARY KEY, title TEXT NOT NULL);
	        CREATE TABLE accounts (id UUID PRIMARY KEY, owner TEXT NOT NULL);
	        CREATE TABLE docs (id UUID PRIMARY KEY, title TEXT NOT NULL);
	        CREATE TABLE drafts (id UUID PRIMARY KEY DEFAULT gen_random_uuid(), title TEXT NOT NULL)`
	fixture.RunWith(t, ddl, func(t *testing.T, db *SiteDB) {
		ctx := t.Context()

		const id = "0192f3a4-7c3d-7b1e-8f00-1234567890ab"

		t.Run("string key", func(t *testing.T) {
			require.NoError(t, db.Pages.Insert(ctx, &Page{ID: "home", Title: "Home"}))
			found, err := db.Pages.Find(ctx, Slug("home"))
			require.NoError(t, err)
			require.Equal(t, "Home", found.Title)
		})

		t.Run("a plain string finds a named key", func(t *testing.T) {
			_, err := db.Pages.Find(ctx, "home")
			require.NoError(t, err)
		})

		t.Run("an empty key is refused", func(t *testing.T) {
			err := db.Pages.Insert(ctx, &Page{ID: "", Title: "Empty"})
			require.ErrorIs(t, err, ant.ErrZeroKey)
			require.ErrorContains(t, err, "Page.ID")
		})

		t.Run("a uuid.UUID key from GenerateKey", func(t *testing.T) {
			doc := &Doc{Title: "a"}
			require.NoError(t, db.Docs.Insert(ctx, doc))
			require.NotEqual(t, uuid.Nil, doc.ID)
		})

		t.Run("a uuid.UUID key from a default", func(t *testing.T) {
			draft := &Draft{Title: "a"}
			require.NoError(t, db.Drafts.Insert(ctx, draft))
			require.NotEqual(t, uuid.Nil, draft.ID, "read back")
		})

		t.Run("UUID key", func(t *testing.T) {
			require.NoError(t, db.Accounts.Insert(ctx, &Account{ID: id, Owner: "Alice"}))
			found, err := db.Accounts.Find(ctx, id)
			require.NoError(t, err)
			require.Equal(t, id, found.ID)
		})

		t.Run("an invalid UUID is the database's error", func(t *testing.T) {
			require.Error(t, db.Accounts.Insert(ctx, &Account{ID: "not-a-uuid"}))
		})

		t.Run("a key of the wrong type", func(t *testing.T) {
			//antvet:ignore a key of the wrong type, on purpose: the test checks the error
			_, err := db.Pages.Find(ctx, 42)
			require.ErrorIs(t, err, ant.ErrKeyType)
		})
	})
}
