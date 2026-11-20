package schema_test

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/stretchr/testify/require"

	"github.com/skudra-go/ant/dialect/postgres"
	"github.com/skudra-go/ant/schema"
)

// A name past the limit is cut where a character starts, so it stays valid
// UTF-8, which PostgreSQL requires of a name (#112).
func TestLongNamesStayValidUTF8(t *testing.T) {
	d, err := schema.Of(postgres.Dialect)
	require.NoError(t, err)

	for _, table := range []string{
		strings.Repeat("п", 20),
		"a" + strings.Repeat("п", 20),
		strings.Repeat("日", 15),
	} {
		got := schema.IndexName(d, table, strings.Repeat("ж", 10))
		require.True(t, utf8.ValidString(got), "%q", got)
		require.LessOrEqual(t, len(got), d.MaxIdentifier())
		require.Regexp(t, `_[0-9a-f]{8}$`, got, "the hash keeps it unique")
	}

	short := schema.IndexName(d, "users", "email")
	require.Equal(t, "users_email_idx", short, "a name within the limit is as PostgreSQL makes it")
}
