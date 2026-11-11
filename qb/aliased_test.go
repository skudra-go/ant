package qb_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/skudra-go/ant/qb"
)

// Aliased requalifies the table's columns in every node that holds an
// expression, and leaves other tables' columns and nested SELECTs alone.
func TestAliasedReachesEveryNode(t *testing.T) {
	table := &qb.TableRef{Name: "posts"}
	other := &qb.TableRef{Name: "users"}
	col := qb.Column{Table: table, Name: "title"}
	want := qb.Column{Table: table, Alias: "ant_related", Name: "title"}
	outer := qb.Column{Table: other, Name: "name"}
	param := qb.Param{Value: 1}
	sub := &qb.Select{Where: []qb.Cond{qb.IsNull{Left: col}}}

	window := func(c qb.Expr) qb.Expr {
		return qb.Func{
			Name:   "COUNT",
			Args:   []qb.Expr{c},
			Filter: []qb.Cond{qb.IsNull{Left: c}},
			Over: &qb.Window{
				PartitionBy: []qb.Expr{c},
				OrderBy:     []qb.Order{{Value: c, Desc: true}},
			},
		}
	}

	shapes := map[string]func(c qb.Expr) qb.Cond{
		"compare": func(c qb.Expr) qb.Cond {
			return qb.Compare{Left: c, Op: qb.OpEq, Right: outer}
		},
		"function": func(c qb.Expr) qb.Cond {
			return qb.Compare{Left: qb.Func{Name: "LOWER", Args: []qb.Expr{c}}, Right: param}
		},
		"window and filter": func(c qb.Expr) qb.Cond {
			return qb.Compare{Left: window(c), Right: param}
		},
		"binary": func(c qb.Expr) qb.Cond {
			return qb.Compare{Left: qb.Binary{Left: param, Op: "+", Right: c}, Right: param}
		},
		"case": func(c qb.Expr) qb.Cond {
			return qb.Compare{
				Left: qb.Case{
					Whens: []qb.When{{If: qb.IsNull{Left: c}, Then: c}},
					Else:  c,
				},
				Right: param,
			}
		},
		"collate": func(c qb.Expr) qb.Cond {
			return qb.Compare{Left: qb.Collate{Value: c, Collation: "C"}, Right: param}
		},
		"cast": func(c qb.Expr) qb.Cond {
			return qb.Compare{Left: qb.Cast{Value: c, Type: "TEXT"}, Right: param}
		},
		"row in": func(c qb.Expr) qb.Cond {
			return qb.In{Left: qb.Row{Values: []qb.Expr{c, outer}}, Values: []qb.Expr{c}}
		},
		"between": func(c qb.Expr) qb.Cond {
			return qb.Between{Left: c, Low: c, High: c}
		},
		"fragment condition": func(c qb.Expr) qb.Cond {
			return qb.Fragment{SQL: "? > 0", Args: []qb.Expr{c}}
		},
		"fragment expression": func(c qb.Expr) qb.Cond {
			return qb.Compare{Left: qb.Fragment{SQL: "f(?)", Args: []qb.Expr{c}}, Right: param}
		},
		"connectives": func(c qb.Expr) qb.Cond {
			return qb.Not{
				Child: qb.And{
					Children: []qb.Cond{
						qb.Or{Children: []qb.Cond{qb.IsNull{Left: c}}},
					},
				},
			}
		},
		"subquery stays": func(qb.Expr) qb.Cond {
			return qb.And{
				Children: []qb.Cond{
					qb.Exists{Select: sub},
					qb.In{Left: outer, Sub: sub},
					qb.Compare{Left: qb.Subquery{Select: sub}, Right: param},
				},
			}
		},
	}

	for name, shape := range shapes {
		got := qb.Aliased(shape(col), table, "ant_related")
		require.Equal(t, shape(want), got, name)
	}

	lazy := qb.Aliased(
		qb.Lazy{
			Build: func(context.Context) (qb.Cond, error) {
				return qb.IsNull{Left: col}, nil
			},
		},
		table,
		"ant_related",
	)

	built, err := lazy.(qb.Lazy).Build(t.Context())
	require.NoError(t, err)
	require.Equal(t, qb.IsNull{Left: want}, built, "lazy")

	none := qb.Aliased(
		qb.Lazy{
			Build: func(context.Context) (qb.Cond, error) {
				return nil, nil
			},
		},
		table,
		"ant_related",
	)

	built, err = none.(qb.Lazy).Build(t.Context())
	require.NoError(t, err)
	require.Nil(t, built, "a lazy condition that matches all stays nil")
}
