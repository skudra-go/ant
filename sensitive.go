package ant

import (
	"github.com/skudra-go/ant/internal/meta"
	"github.com/skudra-go/ant/qb"
)

// readsSensitive reports whether node, an expression or a condition, reads a
// column that one of refs marks Sensitive. A value compared with, or combined
// with, such an expression is as secret as the column, so it is bound hidden
// from logs. A column is matched by its table and name, or by name alone when it
// is not qualified, which may hide a value that needs no hiding, never the
// reverse. Subqueries are not looked into: their own conditions hide their own
// values.
func readsSensitive(node any, refs []*meta.Model) bool {
	if !anySensitive(refs) {
		return false
	}

	return reads(node, func(c qb.Column) bool {
		for _, m := range refs {
			if c.Table != nil && m.Table != nil && c.Table.Name != m.Table.Name {
				continue
			}

			if f := m.Column(c.Name); f != nil && f.Sensitive {
				return true
			}
		}

		return false
	})
}

// anySensitive reports whether any of the models has a Sensitive field.
func anySensitive(refs []*meta.Model) bool {
	for _, m := range refs {
		if m == nil {
			continue
		}

		for _, f := range m.Fields {
			if f.Sensitive {
				return true
			}
		}
	}

	return false
}

// reads reports whether node holds a column that secret matches.
func reads(node any, secret func(qb.Column) bool) bool {
	some := func(nodes ...any) bool {
		for _, n := range nodes {
			if n != nil && reads(n, secret) {
				return true
			}
		}

		return false
	}

	switch n := node.(type) {
	case qb.Column:
		return secret(n)
	case qb.Func:
		for _, a := range n.Args {
			if some(a) {
				return true
			}
		}

		for _, c := range n.Filter {
			if some(c) {
				return true
			}
		}
	case qb.Fragment:
		for _, a := range n.Args {
			if some(a) {
				return true
			}
		}
	case qb.Binary:
		return some(n.Left, n.Right)
	case qb.Case:
		for _, w := range n.Whens {
			if some(w.If, w.Then) {
				return true
			}
		}

		return some(n.Else)
	case qb.Collate:
		return some(n.Value)
	case qb.Cast:
		return some(n.Value)
	case qb.Row:
		for _, v := range n.Values {
			if some(v) {
				return true
			}
		}
	case qb.Compare:
		return some(n.Left, n.Right)
	case qb.In:
		return some(n.Left)
	case qb.Between:
		return some(n.Left)
	case qb.IsNull:
		return some(n.Left)
	case qb.And:
		for _, c := range n.Children {
			if some(c) {
				return true
			}
		}
	case qb.Or:
		for _, c := range n.Children {
			if some(c) {
				return true
			}
		}
	case qb.Not:
		return some(n.Child)
	}

	return false
}

// hidden binds v as a parameter, hidden from logs if secret.
func hidden(v any, secret bool) qb.Param {
	if secret {
		return qb.Param{Value: qb.Secret{V: v}}
	}

	return qb.Param{Value: v}
}

// hideBare hides the bare values among nodes, those bound as parameters.
func hideBare(nodes []qb.Expr) {
	for i, n := range nodes {
		if p, ok := qb.ParamOf(n); ok {
			if _, done := p.Value.(qb.Secret); !done {
				nodes[i] = qb.Param{Value: qb.Secret{V: p.Value}}
			}
		}
	}
}
