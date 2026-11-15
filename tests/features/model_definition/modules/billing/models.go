// Package billing is one module of an application whose models share type names
// with another module's.
package billing

import "github.com/skudra-go/ant"

// Account is the billing module's account.
type Account struct {
	ID      int64
	IBAN    string
	Balance int64
}

// Plan shares the plans table with auth.Plan, on purpose.
type Plan struct {
	ID   int64
	Kind string
	Fee  *int64
}

var plans = ant.Fields[Plan]()

func (Plan) Configure(b *ant.ModelBuilder[Plan]) {
	b.Discriminator(&plans.Kind, "billing")
}
