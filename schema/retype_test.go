package schema_test

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/skudra-go/ant/schema"
)

// A type change that would cut values to fit fails instead, as PostgreSQL's own
// cast does: an explicit USING cast truncates strings and bit strings without a
// word. Changes that need USING still get it.
func TestRetypeKeepsValues(t *testing.T) {
	sb := prNewSandbox(t, prUnique(t))
	if sb == nil {
		t.Skip("needs PostgreSQL in ANT_TEST_DATABASES")
	}

	for _, c := range []struct {
		name     string
		from, to string
		value    string
		want     string // the stored value after the change, or "" for an error
	}{
		{
			name:  "shorter varchar",
			from:  "varchar(10)",
			to:    "varchar(3)",
			value: "'abcdefghij'",
		},
		{
			name:  "text to varchar",
			from:  "text",
			to:    "varchar(3)",
			value: "'abcdefghij'",
		},
		{
			name:  "shorter char",
			from:  "char(5)",
			to:    "char(2)",
			value: "'abcde'",
		},
		{
			name:  "shorter bit string",
			from:  "bit(4)",
			to:    "bit(2)",
			value: "B'1010'",
		},
		{
			name:  "integer to varchar",
			from:  "bigint",
			to:    "varchar(3)",
			value: "12345",
		},
		{
			name:  "array of shorter strings",
			from:  "text[]",
			to:    "varchar(3)[]",
			value: "ARRAY['abcdef']",
		},
		{
			name:  "longer varchar",
			from:  "varchar(3)",
			to:    "varchar(10)",
			value: "'abc'",
			want:  "abc",
		},
		{
			name:  "integer to text",
			from:  "bigint",
			to:    "text",
			value: "12345",
			want:  "12345",
		},
		{
			name:  "text to integer needs USING",
			from:  "text",
			to:    "bigint",
			value: "'42'",
			want:  "42",
		},
		{
			name:  "text to array needs USING",
			from:  "text",
			to:    "text[]",
			value: "'{a,b}'",
			want:  "{a,b}",
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			sb.wipe(t, nil)

			table := func(typ string) *schema.Schema {
				return prWorld(prTab("items", prOpt("v", typ)))
			}

			from, to := table(c.from), table(c.to)
			require.NoError(t, sb.create(from))

			ctx := context.Background()
			_, err := sb.db.ExecContext(ctx, "INSERT INTO items (v) VALUES ("+c.value+")")
			require.NoError(t, err)

			plan, err := schema.Diff(from, to)
			require.NoError(t, err)

			script, err := schema.Script(sb.d, plan)
			require.NoError(t, err)

			err = sb.exec(plan, script)

			if c.want == "" {
				require.Error(t, err, "the change kept going and cut the value")

				return
			}

			require.NoError(t, err)

			var got string
			require.NoError(t, sb.db.QueryRowContext(ctx, "SELECT v::text FROM items").Scan(&got))
			require.Equal(t, c.want, got)
		})
	}
}

// The script leaves USING out where PostgreSQL has a cast of its own, and keeps
// it where it has none.
func TestRetypeUsing(t *testing.T) {
	for _, c := range []struct {
		from, to string
		using    bool
	}{
		{"varchar(10)", "varchar(3)", false},
		{"numeric(10,3)", "numeric(10,0)", false},
		{"timestamp(6) with time zone", "timestamp(0) with time zone", false},
		{"bigint", "character varying(3)", false},
		{"text[]", "varchar(3)[]", false},
		{"text", "bigint", true},
		{"text", "text[]", true},
		{"integer", "boolean", true},
	} {
		plan, err := schema.Diff(
			prWorld(prTab("items", prOpt("v", c.from))),
			prWorld(prTab("items", prOpt("v", c.to))),
		)
		require.NoError(t, err)

		script, err := schema.Script(ddl(t), plan)
		require.NoError(t, err)

		require.Equal(
			t,
			c.using,
			strings.Contains(script, "USING"),
			"%s to %s:\n%s",
			c.from,
			c.to,
			script,
		)
	}
}
