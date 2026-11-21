package schema_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/skudra-go/ant/schema"
)

// The summary of a type change names both sides, and a default change has to
// say the same: "default" alone leaves the reader to rerun with --sql.
func TestAlterColumnNamesBothDefaults(t *testing.T) {
	column := func(defaultExpr string) *schema.Column {
		return &schema.Column{Name: "active", Type: "boolean", Default: defaultExpr}
	}

	for _, tc := range []struct {
		name string
		from string
		to   string
		want string
	}{
		{"dropped", "true", "", "default true to none"},
		{"added", "", "false", "default none to false"},
		{"changed", "true", "false", "default true to false"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			change := schema.AlterColumn{
				Table: schema.Ref{Name: "customer"},
				From:  column(tc.from),
				To:    column(tc.to),
			}

			require.Equal(
				t,
				"change column customer.active: "+tc.want,
				change.String(),
			)
		})
	}
}

// Every other part of a column's summary names both sides too, or which way it
// went, so drift and diff never leave the reader to look it up (#195).
func TestAlterColumnNamesBothSides(t *testing.T) {
	base := schema.Column{Name: "total", Type: "numeric"}

	for _, tc := range []struct {
		name     string
		from, to func(*schema.Column)
		want     string
	}{
		{
			"identity added",
			func(*schema.Column) {},
			func(c *schema.Column) { c.Identity = true },
			"add identity",
		},
		{
			"identity dropped",
			func(c *schema.Column) { c.Identity = true },
			func(*schema.Column) {},
			"drop identity",
		},
		{
			"generated expression changed",
			func(c *schema.Column) { c.Generated = "price * 2" },
			func(c *schema.Column) { c.Generated = "price * 3" },
			"generated expression price * 2 to price * 3",
		},
		{
			"generated expression added",
			func(*schema.Column) {},
			func(c *schema.Column) { c.Generated = "price * 3" },
			"generated expression none to price * 3",
		},
		{
			"collation set",
			func(*schema.Column) {},
			func(c *schema.Column) { c.Collation = `"C"` },
			`collation default to "C"`,
		},
		{
			"collation removed",
			func(c *schema.Column) { c.Collation = `"C"` },
			func(*schema.Column) {},
			`collation "C" to default`,
		},
		{
			"several at once",
			func(c *schema.Column) { c.Nullable, c.Default = true, "0" },
			func(c *schema.Column) { c.Type = "bigint" },
			"type numeric to bigint, require a value, default 0 to none",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			from, to := base, base
			tc.from(&from)
			tc.to(&to)

			change := schema.AlterColumn{
				Table: schema.Ref{Name: "orders"},
				From:  &from,
				To:    &to,
			}

			require.Equal(t, "change column orders.total: "+tc.want, change.String())
		})
	}
}

// A sequence's summary names its start and increment before and after, counting
// an unset one as 1, as PostgreSQL does.
func TestAlterSequenceNamesBothSides(t *testing.T) {
	for _, tc := range []struct {
		name     string
		from, to schema.Sequence
		want     string
	}{
		{
			"start",
			schema.Sequence{Name: "org_numbers"},
			schema.Sequence{Name: "org_numbers", Start: 1000},
			"change sequence org_numbers: start 1 to 1000",
		},
		{
			"increment",
			schema.Sequence{Name: "org_numbers", Increment: 5},
			schema.Sequence{Name: "org_numbers"},
			"change sequence org_numbers: increment 5 to 1",
		},
		{
			"both",
			schema.Sequence{Name: "org_numbers", Start: 10, Increment: 2},
			schema.Sequence{Name: "org_numbers", Start: 1000, Increment: 10},
			"change sequence org_numbers: start 10 to 1000, increment 2 to 10",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			change := schema.AlterSequence{From: &tc.from, To: &tc.to}
			require.Equal(t, tc.want, change.String())
		})
	}
}
