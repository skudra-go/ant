package model_definition

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/skudra-go/ant"
	"github.com/skudra-go/ant/tests/features/fixture"
)

// A model is an ordinary struct. It needs no base type, tags, or generated code.
type Book struct {
	ID    int64
	Title string
	Pages int
}

type LibraryDB struct {
	ant.Context
	Books ant.Table[Book]
}

var Books = ant.Fields[Book]()

func TestPlainStruct(t *testing.T) {
	ddl := `CREATE TABLE books (id BIGSERIAL PRIMARY KEY, title TEXT NOT NULL, pages INT NOT NULL)`
	fixture.RunWith(t, ddl, func(t *testing.T, db *LibraryDB) {
		book := &Book{Title: "The Go Programming Language", Pages: 380}
		require.NoError(t, db.Books.Insert(t.Context(), book))

		found, err := db.Books.Where(ant.Gt(&Books.Pages, 100)).First(t.Context())
		require.NoError(t, err)
		require.Equal(t, *book, *found)
	})
}
