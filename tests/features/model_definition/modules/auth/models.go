// Package auth is the other module, with its own Account.
package auth

import "github.com/skudra-go/ant"

// Account is the auth module's account.
type Account struct {
	ID       int64
	Email    string
	Password string
}

// Plan shares the plans table with billing.Plan, on purpose.
type Plan struct {
	ID    int64
	Kind  string
	Seats *int
}

var plans = ant.Fields[Plan]()

func (Plan) Configure(b *ant.ModelBuilder[Plan]) {
	b.Discriminator(&plans.Kind, "auth")
}
