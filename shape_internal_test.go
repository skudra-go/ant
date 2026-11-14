package ant

import (
	"context"
	"database/sql"
	"reflect"
	"strconv"
	"testing"
	"time"
	"unsafe"

	"github.com/skudra-go/ant/dialect"
	"github.com/skudra-go/ant/dialect/mysql"
	"github.com/skudra-go/ant/dialect/sqlite"
	"github.com/skudra-go/ant/dialect/sqlserver"
	"github.com/skudra-go/ant/internal/meta"
	"github.com/skudra-go/ant/qb"
)

type shapeRow struct {
	ID        int64
	Name      string
	Age       int
	At        time.Time
	Note      *string
	Secret    string
	DeletedAt DeletedAt
}

func (shapeRow) Configure(b *ModelBuilder[shapeRow]) {
	b.Column(&shapeRows.Secret).Sensitive()
}

type tenantRow struct {
	ID     int64
	Tenant string
}

func (tenantRow) Configure(b *ModelBuilder[tenantRow]) {
	b.Filter("tenant", func(ctx context.Context) Cond {
		tenant, _ := ctx.Value(tenantKey{}).(string)

		return Eq(&tenantRows.Tenant, tenant)
	})
}

type tenantKey struct{}

var (
	shapeRows  = Fields[shapeRow]()
	tenantRows = Fields[tenantRow]()
)

type shapeDB struct {
	Context
	Rows    Table[shapeRow]
	Tenants Table[tenantRow]
}

// numbered is a dialect with numbered placeholders and no Appender, as
// PostgreSQL's are, which this package cannot import.
type numbered struct{ dialect.Dialect }

func (numbered) Name() string             { return "numbered" }
func (numbered) Placeholder(n int) string { return "$" + strconv.Itoa(n) }
func (numbered) Quote(ident string) string {
	return dialect.QuoteWith(ident, `"`, `"`)
}
func (n numbered) Render(stmt qb.Statement) (string, []any, error) {
	return dialect.Render(n, stmt)
}

// shapeDialects are the dialects whose SQL the cache must reproduce: each binds
// its values in its own order, quotes in its own way, and some convert times.
var shapeDialects = []dialect.Dialect{
	numbered{sqlite.Dialect},
	mysql.Dialect,
	mysql.MariaDB,
	sqlite.Dialect,
	sqlserver.Dialect,
}

func shapeContext(t *testing.T, d dialect.Dialect) *shapeDB {
	t.Helper()

	// Nothing here runs a statement, so the database is never opened.
	pool, err := sql.Open("sqlite", "file:unused?mode=memory")
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { pool.Close() })

	db, err := Wrap[shapeDB](d, pool)
	if err != nil {
		t.Fatal(err)
	}

	return db
}

// rendered is what the usual way renders for q read with limit, and a key
// condition if key is set.
func rendered[T any](
	t *testing.T,
	q *Query[T],
	limit *int,
	key *meta.Field,
	keyValue any,
) (string, []any) {
	t.Helper()

	n := q

	if key != nil {
		n = q.next()
		n.where = append(n.where, keyEquals(q.model(), key, keyValue)...)
	}

	stmt := n.statement(n.selection())
	stmt.Limit = limit

	text, args, err := n.state.dialect.Render(
		qb.Contextual{Context: context.Background(), Statement: stmt},
	)
	if err != nil {
		t.Fatal(err)
	}

	return text, args
}

// The cache gives the SQL and values the usual way renders, on every dialect,
// the first time a shape is seen and every time after, with other values.
func TestShapeCacheRendersAsTheUsualWay(t *testing.T) {
	east := time.FixedZone("east", 3*3600)
	note := "n"

	type read struct {
		limit    *int
		key      bool
		keyValue any
	}

	cases := []struct {
		name  string
		query func(db *shapeDB, i int) *Query[shapeRow]
		reads []read
	}{
		{
			"no conditions",
			func(db *shapeDB, _ int) *Query[shapeRow] {
				return db.Rows.Query()
			},
			[]read{{}, {limit: new(1)}, {limit: new(2)}},
		},
		{
			"comparisons",
			func(db *shapeDB, i int) *Query[shapeRow] {
				return db.Rows.Where(
					Eq(&shapeRows.Name, "a"+strconv.Itoa(i)),
					Gt(&shapeRows.Age, i),
					Lte(&shapeRows.Age, 1000+i),
					Ne(&shapeRows.Name, "z"),
					Like(&shapeRows.Name, "a%"),
					ILike(&shapeRows.Name, "B%"),
				)
			},
			[]read{{}, {limit: new(1)}},
		},
		{
			"nulls and ranges",
			func(db *shapeDB, i int) *Query[shapeRow] {
				return db.Rows.Where(
					IsNull(&shapeRows.Note),
					IsNotNull(&shapeRows.Name),
					Eq(&shapeRows.Note, nil),
					Ne(&shapeRows.Note, &note),
					Between(&shapeRows.Age, i, i+10),
				)
			},
			[]read{{}, {limit: new(1)}},
		},
		{
			"times in a zone",
			func(db *shapeDB, i int) *Query[shapeRow] {
				return db.Rows.Where(Gt(&shapeRows.At, time.Date(2026, 1, 2, 3, 4, i, 0, east)))
			},
			[]read{{}, {limit: new(1)}},
		},
		{
			"a sensitive value",
			func(db *shapeDB, i int) *Query[shapeRow] {
				return db.Rows.Where(Eq(&shapeRows.Secret, "s"+strconv.Itoa(i)))
			},
			[]read{{}, {limit: new(1)}},
		},
		{
			"ordered and paged",
			func(db *shapeDB, i int) *Query[shapeRow] {
				return db.Rows.Where(Gt(&shapeRows.Age, i)).
					OrderByDesc(&shapeRows.Age).
					ThenBy(&shapeRows.Name).
					Order(Col(&shapeRows.Age).Asc().NullsFirst()).
					Limit(10 + i).
					Offset(20 + i)
			},
			[]read{{limit: new(10)}, {limit: new(1)}, {limit: new(2)}},
		},
		{
			"offset with no limit",
			func(db *shapeDB, i int) *Query[shapeRow] {
				return db.Rows.OrderBy(&shapeRows.Name).Offset(5 + i)
			},
			[]read{{}, {limit: new(1)}},
		},
		{
			"no soft delete filter",
			func(db *shapeDB, i int) *Query[shapeRow] {
				return db.Rows.WithDeleted().Where(Eq(&shapeRows.Age, i))
			},
			[]read{{}, {limit: new(1)}},
		},
		{
			"only deleted",
			func(db *shapeDB, i int) *Query[shapeRow] {
				return db.Rows.OnlyDeleted().Where(Eq(&shapeRows.Age, i))
			},
			[]read{{}},
		},
		{
			"by key",
			func(db *shapeDB, i int) *Query[shapeRow] {
				return db.Rows.Where(Gt(&shapeRows.Age, i))
			},
			[]read{
				{limit: new(1), key: true, keyValue: int64(7)},
				{limit: new(1), key: true, keyValue: 99},
			},
		},
	}

	for _, d := range shapeDialects {
		db := shapeContext(t, d)
		key := meta.Of[shapeRow]().Key()

		for _, c := range cases {
			for ri, r := range c.reads {
				// Twice with other values: the first learns the shape, the
				// second uses it.
				for i := range 3 {
					q := c.query(db, i+ri*10)

					var (
						k     *meta.Field
						value any
					)

					if r.key {
						k, value = key, r.keyValue
					}

					text, args, ok := q.cachedSQL(k, value, r.limit)
					if !ok {
						t.Fatalf("%s, %s: the cache did not take the query", d.Name(), c.name)
					}

					wantText, wantArgs := rendered(t, q, r.limit, k, value)

					if text != wantText || !reflect.DeepEqual(args, wantArgs) {
						t.Fatalf(
							"%s, %s, read %d, pass %d:\ngot  %s %#v\nwant %s %#v",
							d.Name(),
							c.name,
							ri,
							i,
							text,
							args,
							wantText,
							wantArgs,
						)
					}
				}
			}
		}
	}
}

// Queries whose SQL the cache cannot vouch for go the usual way.
func TestShapeCacheLeavesOtherQueries(t *testing.T) {
	db := shapeContext(t, sqlite.Dialect)

	seven := make([]Cond, 7)
	for i := range seven {
		seven[i] = Gt(&shapeRows.Age, i)
	}

	cases := map[string]*Query[shapeRow]{
		"an OR":               db.Rows.Where(Or(Eq(&shapeRows.Age, 1), Eq(&shapeRows.Age, 2))),
		"IN":                  db.Rows.Where(In(&shapeRows.Age, 1, 2)),
		"NOT":                 db.Rows.Where(Not(Eq(&shapeRows.Age, 1))),
		"raw SQL":             db.Rows.Where(SQLCond("age > ?", 1)),
		"two columns":         db.Rows.Where(EqCol(&shapeRows.Age, &shapeRows.Age)),
		"chosen columns":      db.Rows.Column(&shapeRows.Name),
		"an omitted column":   db.Rows.Omit(&shapeRows.Name),
		"Debug":               db.Rows.Query().Debug(),
		"a comment":           db.Rows.Query().Tag("t"),
		"a hint":              db.Rows.Query().Hint("h"),
		"DISTINCT":            db.Rows.Distinct(),
		"a lock":              db.Rows.Query().ForUpdate(),
		"an ordering by SQL":  db.Rows.Query().OrderByRandom(),
		"too many conditions": db.Rows.Where(seven...),
		"a derived source":    db.Rows.Query().From(db.Rows.Where(Eq(&shapeRows.Age, 1))),
		"a union":             db.Rows.Where(Eq(&shapeRows.Age, 1)).Union(db.Rows.Query()),
	}

	for name, q := range cases {
		if _, _, ok := q.cachedSQL(nil, nil, nil); ok {
			t.Errorf("the cache took a query with %s", name)
		}
	}

	if _, _, ok := db.Tenants.Query().cachedSQL(nil, nil, nil); ok {
		t.Error("the cache took a query with a filter that reads its context")
	}

	if _, _, ok := db.Tenants.Query().IgnoreFilters().cachedSQL(nil, nil, nil); !ok {
		t.Error("the cache did not take a query with its filters switched off")
	}
}

// An INSERT of one row binds its values to the kept SQL as the usual way does,
// for each set of columns the database fills.
func TestShapeCacheInsertsAsTheUsualWay(t *testing.T) {
	east := time.FixedZone("east", 3*3600)

	for _, d := range shapeDialects {
		db := shapeContext(t, d)
		model := meta.Of[shapeRow]()
		writable := model.Writable()
		u := newUnit(db.state)

		for i := range 3 {
			for _, generated := range []bool{true, false} {
				// With no RETURNING, a generated key is read another way.
				if generated && !d.SupportsReturning() {
					continue
				}

				row := &shapeRow{
					Name:   "n" + strconv.Itoa(i),
					Age:    i,
					At:     time.Date(2026, 1, 2, 3, 4, i, 0, east),
					Secret: "s",
				}

				var (
					filled    filledFields
					returning []*meta.Field
				)

				if generated {
					filled.set(0, len(writable))
					returning = []*meta.Field{writable[0]}
				} else {
					row.ID = int64(100 + i)
				}

				n := &node{model: model, entity: unsafeOf(row)}

				text, args, err := u.insertSQL(n, writable, &filled, returning)
				if err != nil {
					t.Fatal(err)
				}

				values := make([]any, 0, len(writable))

				for j, f := range writable {
					if !filled.has(j) {
						values = append(values, fieldParam(f, f.Value(n.entity).Interface()).Value)
					}
				}

				wantText, wantArgs, err := d.Render(
					oneRowInsert(model, writable, &filled, returning, values),
				)
				if err != nil {
					t.Fatal(err)
				}

				if text != wantText || !reflect.DeepEqual(args, wantArgs) {
					t.Fatalf(
						"%s, pass %d:\ngot  %s %#v\nwant %s %#v",
						d.Name(),
						i,
						text,
						args,
						wantText,
						wantArgs,
					)
				}
			}
		}
	}
}

// Each shape is learned once for a context, and the cache stops growing at
// its bound.
func TestShapeCacheIsBounded(t *testing.T) {
	var c plans[int]

	for i := range maxPlans + 10 {
		p := &plan{text: strconv.Itoa(i)}
		if got := c.put(i, p); got != p {
			t.Fatalf("put %d gave another plan", i)
		}
	}

	if got := len(*c.m.Load()); got != maxPlans {
		t.Errorf("%d plans kept, want %d", got, maxPlans)
	}

	if c.get(maxPlans+5) != nil {
		t.Error("a plan past the bound was kept")
	}

	first := c.get(0)
	if c.put(0, &plan{text: "other"}) != first {
		t.Error("a second plan for a shape replaced the first")
	}
}

func unsafeOf[T any](v *T) unsafe.Pointer { return unsafe.Pointer(v) }
