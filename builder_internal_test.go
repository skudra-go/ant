package ant

import (
	"database/sql"
	"testing"

	_ "modernc.org/sqlite" // driver "sqlite"

	"github.com/skudra-go/ant/dialect/sqlite"
	"github.com/skudra-go/ant/qb"
)

type sharedParent struct {
	ID   int64
	Name string
}

type sharedChild struct {
	ID       int64
	ParentID int64
	Parent   *sharedParent
}

type sharedDB struct {
	Context
	Parents  Table[sharedParent]
	Children Table[sharedChild]
}

// A query shares with the one it was made from the lists that are only added
// to, with no room left in them, and has a copy of its own of anything it
// changes in place.
func TestBuilderSharesOnlyWhatItDoesNotChange(t *testing.T) {
	// Nothing here runs a statement, so the database is never opened.
	pool, err := sql.Open("sqlite", "file:unused?mode=memory")
	if err != nil {
		t.Fatal(err)
	}

	defer pool.Close()

	db, err := Wrap[sharedDB](sqlite.Dialect, pool)
	if err != nil {
		t.Fatal(err)
	}

	children := Fields[sharedChild]()
	parents := Fields[sharedParent]()

	base := db.Children.Where(Gt(&children.ID, 1), Gt(&children.ID, 2), Gt(&children.ID, 3))
	next := base.Limit(1)

	if &next.where[0] != &base.where[0] || &next.sources[0] != &base.sources[0] {
		t.Error("a step copied a list it did not change")
	}

	if cap(next.where) != len(next.where) || cap(next.sources) != len(next.sources) ||
		cap(next.refs) != len(next.refs) {
		t.Error("a shared list has room that two queries could both write into")
	}

	// The list every query of the model selects is the model's own.
	if &base.selection()[0] != &base.model().Selection()[0] ||
		&base.columns()[0] != &base.model().Stored()[0] {
		t.Error("a query with no columns chosen built its own selection")
	}

	one := next.Where(Eq(&children.ParentID, 10))
	two := next.Where(Eq(&children.ParentID, 20))

	if len(base.where) != 3 || len(next.where) != 3 || len(one.where) != 4 || len(two.where) != 4 {
		t.Fatalf(
			"conditions: %d, %d, %d, %d",
			len(base.where),
			len(next.where),
			len(one.where),
			len(two.where),
		)
	}

	for q, want := range map[*Query[sharedChild]]int64{one: 10, two: 20} {
		if got := q.where[3].(qb.Compare).Right.(qb.Param).Value; got != want {
			t.Errorf("a query made from the same base holds %v, want %d", got, want)
		}
	}

	// A right join makes the tables before it nullable, in a copy.
	right := base.RightJoinOn[sharedParent](EqCol(&children.ParentID, &parents.ID))

	if base.sources[0].nullable || next.sources[0].nullable {
		t.Error("a right join made its base's table nullable")
	}

	if !right.sources[0].nullable || len(base.sources) != 1 || len(right.sources) != 2 {
		t.Errorf("right join sources: %+v", right.sources)
	}

	// A join is added to a copy of the joins, whose conditions are set in place.
	inner := base.Join(&children.Parent)
	using := base.JoinUsing[sharedParent, int64](&children.ID)

	if len(base.joins) != 0 || len(inner.joins) != 1 || len(using.joins) != 1 {
		t.Fatalf("joins: %d, %d, %d", len(base.joins), len(inner.joins), len(using.joins))
	}

	if left := inner.joins[0].On[0].(qb.Compare).Left.(qb.Column).Name; left != "parent_id" {
		t.Errorf("another query's join changed this one to be on %s", left)
	}

	if left := using.joins[0].On[0].(qb.Compare).Left.(qb.Column).Name; left != "id" {
		t.Errorf("JoinUsing joined on %s", left)
	}

	// Ordering is replaced in place by paging, so each query has its own.
	ordered := base.OrderBy(&children.ID)
	then := ordered.ThenBy(&children.ParentID)

	if len(ordered.order) != 1 || len(then.order) != 2 || &ordered.order[0] == &then.order[0] {
		t.Error("two queries share an ordering")
	}

	// Omitted columns are taken out in place by Include, so they are copied.
	omitted := base.Omit(&children.ParentID)
	again := omitted.Limit(1)

	if &omitted.omitted[0] == &again.omitted[0] {
		t.Error("two queries share their omitted columns")
	}

	if got := len(omitted.columns()); got != 1 {
		t.Errorf("%d columns with one of two omitted", got)
	}

	if len(base.model().Stored()) != 2 {
		t.Error("omitting a column took it out of the model")
	}
}
