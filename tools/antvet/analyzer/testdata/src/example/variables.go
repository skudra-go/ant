package example

import (
	"context"

	"github.com/skudra-go/ant"

	"models"
)

// A query built up in a variable of the function, as a filter endpoint builds
// one, is judged as its chain would be, when every use of the variable runs the
// query or builds it up into a variable used the same way (#220).
func builtUp(ctx context.Context, users *ant.Table[User], posts *ant.Table[Post], name string) {
	q := posts.Where(ant.Gt(&P.UserID, 1))
	if name != "" {
		q = q.Where(ant.Eq(&U.Name, name)) // want `condition on User in a query of Post`
	}

	q.All(ctx)

	r := users.Where(ant.Gt(&P.UserID, 1)) // want `condition on Post in a query of User`
	s := r.OrderBy(&P.ID)                  // want `OrderBy on a field of Post in a query of User`
	(s).Where(ant.Gt(&U.Age, 1)).Count(ctx)

	var t = posts.Where()
	for range 2 {
		t = t.Where(ant.Gt(&U.Age, 1)) // want `condition on User in a query of Post`
	}

	ant.SumOf(ctx, t, &P.UserID)

	// A subquery's scope takes in the query held in the variable.
	o := users.Where(ant.Exists(posts.Where(ant.Eq(&models.Co.Code, "x")))) // want `condition on Country in a query of Post`
	o.All(ctx)

	ok := users.Where(ant.Exists(posts.Where(ant.Eq(&U.Name, "x"))))
	ok.All(ctx)

	a, b := posts.Where(ant.Gt(&U.Age, 1)), users.Where() // want `condition on User in a query of Post`
	a.All(ctx)
	b.All(ctx)

	// A join's models are in its type, which a variable keeps.
	j := posts.Where().JoinUsing[User](&P.UserID)
	j = j.Where(ant.Eq(&U.Name, "x"))
	j = j.Where(ant.Eq(&models.Co.Code, "x")) // want `condition on Country in a query of Post and User`
	j.Select(func(p *Post, u *User) string { return "" }).All(ctx)

	// A closure that runs it is a use like any other.
	c := posts.Where(ant.Gt(&U.Age, 1)) // want `condition on User in a query of Post`
	run := func() { c.First(ctx) }
	run()
}

// A variable whose query goes where the analyzer does not follow it may end up
// in another query: as a subquery, in a join, given to a function of one's own,
// returned, stored, or through its address. So are a parameter, which the caller
// gives, and a variable of the package.
func unfollowed(ctx context.Context, users *ant.Table[User], posts *ant.Table[Post], q *ant.Query[Post]) *ant.Query[Post] {
	sub := posts.Where(ant.Eq(&U.ID, 1))
	users.Where(ant.Exists(sub)).All(ctx)

	joined := posts.Where(ant.Eq(&U.Name, "x"))
	joined.Join().All(ctx)

	given := posts.Where(ant.Eq(&U.Name, "x"))
	keepQuery(given)

	addressed := posts.Where(ant.Eq(&U.Name, "x"))
	keepPointer(&addressed)

	var held struct{ q *ant.Query[Post] }

	stored := posts.Where(ant.Eq(&U.Name, "x"))
	held.q = stored

	partly := posts.Where(ant.Eq(&U.Name, "x"))
	partly.All(ctx)
	users.Where(ant.Exists(partly)).All(ctx)

	q = q.Where(ant.Eq(&U.Name, "x"))
	q.All(ctx)

	more := posts.Where().JoinUsing[User](&P.UserID)
	more = more.Where(ant.Eq(&D.Slug, "x"))
	more.JoinOn[Doc]().JoinOn[Pair]().Select(func(*Post, *User, *Doc, *Pair) string { return "" }).All(ctx)

	scoped := posts.Where(ant.Eq(&U.Name, "x"))
	scoped = visible(scoped)
	scoped.All(ctx)

	global = global.Where(ant.Eq(&U.Name, "x"))
	global.All(ctx)

	returned := posts.Where(ant.Eq(&U.Name, "x"))

	return returned
}

var global *ant.Query[Post]

func visible(q *ant.Query[Post]) *ant.Query[Post] { return q }

func keepQuery(*ant.Query[Post])    {}
func keepPointer(**ant.Query[Post]) {}
