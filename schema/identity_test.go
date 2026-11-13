package schema_test

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/skudra-go/ant/schema"
)

// Making an existing column an identity on a table that holds rows starts the
// identity after the largest value, so the next insert does not collide.
func TestIdentityOnFilledColumn(t *testing.T) {
	p := prUnique(t)

	sb := prNewSandbox(t, p)
	if sb == nil {
		t.Skip("needs PostgreSQL in ANT_TEST_DATABASES")
	}

	quote := func(name string) string { return `"` + strings.ReplaceAll(name, `"`, `""`) + `"` }

	for _, c := range []struct {
		name   string
		schema string
		table  string
		column string
		values []string
		want   int64
	}{
		{
			name:   "rows",
			table:  "items",
			column: "id",
			values: []string{"1", "2", "3"},
			want:   4,
		},
		{
			name:   "empty",
			table:  "items",
			column: "id",
			want:   1,
		},
		{
			name:   "only negative keys",
			table:  "items",
			column: "id",
			values: []string{"-5"},
			want:   1,
		},
		{
			name:   "quoted names",
			table:  `it'ems "X"`,
			column: `I'd`,
			values: []string{"7"},
			want:   8,
		},
		{
			name:   "another schema",
			schema: p + "other",
			table:  "items",
			column: "id",
			values: []string{"9"},
			want:   10,
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			var named []string

			if c.schema != "" {
				named = append(named, c.schema)
			}

			sb.wipe(t, named)

			table := func(identity bool) *schema.Schema {
				key := prReq(c.column, "bigint")

				if identity {
					key = prIdent(c.column)
				}

				return prWorld(
					prIn(c.schema, prPK(prTab(c.table, key, prOpt("v", "text")), c.column)),
				)
			}

			from, to := table(false), table(true)
			require.NoError(t, sb.create(from))

			ref := quote(c.table)

			if c.schema != "" {
				ref = quote(c.schema) + "." + ref
			}

			ctx := context.Background()

			for _, v := range c.values {
				_, err := sb.db.ExecContext(
					ctx,
					"INSERT INTO "+ref+" ("+quote(c.column)+") VALUES ("+v+")",
				)
				require.NoError(t, err)
			}

			plan, err := schema.Diff(from, to)
			require.NoError(t, err)

			script, err := schema.Script(sb.d, plan)
			require.NoError(t, err)
			require.NoError(t, sb.exec(plan, script))

			var got int64
			require.NoError(
				t,
				sb.db.QueryRowContext(ctx, "INSERT INTO "+ref+" (v) VALUES ('next') RETURNING "+quote(c.column)).
					Scan(&got),
			)

			require.Equal(t, c.want, got)
		})
	}
}
