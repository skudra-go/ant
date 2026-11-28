package migrations

import (
	"bytes"
	"fmt"
	"io"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/skudra-go/ant/dialect/postgres"
	"github.com/skudra-go/ant/migrate"
	"github.com/skudra-go/ant/tests/features/fixture"
)

// The CLI names the connected database and the source it chose without
// printing a DSN, even when more than one source is set.
func TestDatabaseTargetAndDSNPriority(t *testing.T) {
	fixture.Serial(t)

	each(t, func(t *testing.T, p *project) {
		database := scalar[string](p, "SELECT current_database()")
		u, err := url.Parse(p.target.DSN)
		require.NoError(t, err)

		query := u.Query()
		query.Set("application_name", "private_application_value_342")
		u.RawQuery = query.Encode()
		valid := u.String()

		port := u.Port()
		if port == "" {
			port = "5432"
		}

		query.Set("host", u.Hostname())
		query.Set("port", port)
		query.Set("dbname", database)

		if u.User != nil {
			query.Set("user", u.User.Username())

			if password, ok := u.User.Password(); ok {
				query.Set("password", password)
			}
		}

		escape := strings.NewReplacer("\\", "\\\\", "'", "\\'")

		var keywords []string

		for key := range query {
			keywords = append(keywords, key+"='"+escape.Replace(query.Get(key))+"'")
		}

		keywordDSN := strings.Join(keywords, " ")
		unused := "postgres://private_user:private_password@127.0.0.1:1/other?sslmode=disable"

		cases := []struct {
			name   string
			args   []string
			env    string
			config string
			source string
		}{
			{"flag wins", []string{"seed", "--dsn", valid}, unused, unused, "--dsn"},
			{
				"environment wins with keyword DSN",
				[]string{"seed"},
				keywordDSN,
				unused,
				"DATABASE_URL",
			},
			{"config fallback", []string{"seed"}, "", valid, "Config.DSN"},
		}

		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				t.Setenv("DATABASE_URL", tc.env)

				var out bytes.Buffer

				config := p.config
				config.DSN = tc.config
				config.Out = &out
				err := migrate.Run[V1DB](t.Context(), p.target.Dialect, tc.args, config)
				require.NoError(t, err)

				require.Contains(t, out.String(), fmt.Sprintf("Database: %q at ", database))

				require.Contains(
					t,
					out.String(),
					fmt.Sprintf(":%s\" (from %s)\nNo seeders are registered.\n", port, tc.source),
				)

				require.NotContains(t, out.String(), valid)
				require.NotContains(t, out.String(), "private_application_value_342")
			})
		}

		t.Setenv("DATABASE_URL", "")

		p.config.DSN = valid
		must[V1DB](p, "diff", "start")
		dryRun := must[V1DB](p, "up", "--dry-run")
		require.NotContains(t, dryRun, "Database:") // stdout is SQL for a pipe

		for _, command := range [][]string{{"up"}, {"status"}, {"baseline"}, {"repair"}, {"seed"}} {
			out := must[V1DB](p, command...)
			require.Contains(t, out, fmt.Sprintf("Database: %q at ", database))
			require.Contains(t, out, fmt.Sprintf(":%s\" (from Config.DSN)\n", port))
			require.NotContains(t, out, valid)
			require.NotContains(t, out, "private_application_value_342")
		}
	})
}

func TestMigrateDoesNotLoadDotEnv(t *testing.T) {
	fixture.Serial(t)
	t.Setenv("DATABASE_URL", "")
	t.Chdir(t.TempDir())
	require.NoError(t, os.WriteFile(".env", []byte("DATABASE_URL=postgres://ignored\n"), 0o600))

	err := migrate.Run[V1DB](
		t.Context(),
		postgres.Dialect,
		[]string{"seed"},
		migrate.Config{Out: io.Discard},
	)
	require.ErrorContains(t, err, "seed needs a database")
}
