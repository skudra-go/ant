package querying

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/skudra-go/ant"
	"github.com/skudra-go/ant/tests/features/fixture"
)

// ant.Like matches a pattern: % is any text, and _ is one character.
func TestLike(t *testing.T) {
	fixture.Run(t, func(t *testing.T, db *fixture.AppDB) {
		fixture.Seed(t, db)
		ctx := t.Context()

		cases := []struct {
			name, pattern string
			want          []string
		}{
			{"prefix", "A%", []string{"Alice"}},
			{"suffix", "%l", []string{"Carol"}},
			{"contains", "%o%", []string{"Bob", "Carol"}},
			{"one character", "B_b", []string{"Bob"}},
			{"no wildcard is an exact match", "Bob", []string{"Bob"}},
			{"case matters on PostgreSQL", "alice", []string{}},
		}

		for _, c := range cases {
			t.Run(c.name, func(t *testing.T) {
				got, err := db.Users.Where(ant.Like(&Users.Name, c.pattern)).
					OrderBy(&Users.Name).
					All(ctx)
				require.NoError(t, err)
				require.Equal(t, c.want, fixture.Names(got))
			})
		}
	})
}
