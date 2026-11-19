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
