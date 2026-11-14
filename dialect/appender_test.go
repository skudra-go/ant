package dialect_test

import (
	"fmt"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/skudra-go/ant/dialect"
	"github.com/skudra-go/ant/dialect/postgres"
	"github.com/skudra-go/ant/dialect/sqlite"
	"github.com/skudra-go/ant/dialect/sqlserver"
	"github.com/skudra-go/ant/qb"
)

// Every dialect of ant's own writes identifiers and placeholders in place, and
// writes what Quote and Placeholder return.
func TestAppendersMatchQuoteAndPlaceholder(t *testing.T) {
	idents := []string{
		"a",
		"",
		"users",
		`a"b`,
		"a`b",
		"a]b",
		"a[b",
		`""`,
		"``",
		"]]",
		`"`,
		"]",
		"a.b",
		"ā b",
		"x]]y]",
	}

	for _, d := range everyDialect {
		t.Run(d.Name(), func(t *testing.T) {
			fast, ok := d.(dialect.Appender)
			if !ok {
				t.Fatal("no Appender")
			}

			for _, ident := range idents {
				if got := string(
					fast.AppendQuote([]byte("x "), ident),
				); got != "x "+d.Quote(
					ident,
				) {
					t.Errorf("AppendQuote(%q) = %q, Quote = %q", ident, got, d.Quote(ident))
				}
			}

			for _, n := range []int{1, 2, 9, 10, 99, 100, 65535} {
				if got := string(
					fast.AppendPlaceholder([]byte("x "), n),
				); got != "x "+d.Placeholder(
					n,
				) {
					t.Errorf(
						"AppendPlaceholder(%d) = %q, Placeholder = %q",
						n,
						got,
						d.Placeholder(n),
					)
				}
			}
		})
	}
}

func TestAppendHelpers(t *testing.T) {
	cases := []struct{ ident, open, close string }{
		{"a", `"`, `"`},
		{`a"b""c"`, `"`, `"`},
		{"a]b", "[", "]"},
		{"abc", "<<", ">>"},
		{"a>>b>", "<<", ">>"},
		{"a", "", ""},
	}

	for _, c := range cases {
		want := dialect.QuoteWith(c.ident, c.open, c.close)
		if got := string(dialect.AppendQuoteWith(nil, c.ident, c.open, c.close)); got != want {
			t.Errorf(
				"AppendQuoteWith(%q, %q, %q) = %q, want %q",
				c.ident,
				c.open,
				c.close,
				got,
				want,
			)
		}
	}

	for _, n := range []int{0, 1, 42, 1 << 20} {
		want := dialect.Numbered("$", n)
		if got := string(dialect.AppendNumbered([]byte("="), "$", n)); got != "="+want {
			t.Errorf("AppendNumbered(%d) = %q, want %q", n, got, "="+want)
		}
	}
}

// plainDialect is a dialect with no Appender, as one written outside ant may
// be: only what it is given to pass on is seen.
type plainDialect struct{ inner dialect.Dialect }

func (p plainDialect) Name() string              { return p.inner.Name() }
func (p plainDialect) DriverName() string        { return p.inner.DriverName() }
func (p plainDialect) Placeholder(n int) string  { return p.inner.Placeholder(n) }
func (p plainDialect) Quote(ident string) string { return p.inner.Quote(ident) }
func (p plainDialect) SupportsReturning() bool   { return p.inner.SupportsReturning() }
func (p plainDialect) Syntax() *dialect.Syntax   { return dialect.SyntaxOf(p.inner) }

func (p plainDialect) Render(stmt qb.Statement) (string, []any, error) {
	return dialect.Render(p, stmt)
}

// statements returns one statement of each kind, with identifiers that need
// escaping and values bound both ways a statement can hold them.
func statements() map[string]qb.Statement {
	users := &qb.TableRef{Schema: "app", Name: `us"er]s`}
	age := qb.Column{Table: users, Name: "a`ge"}
	name := qb.Column{Table: users, Name: "name"}
	held := []qb.Param{{Value: "ann"}, {Value: 7}, {Value: "bob"}, {Value: 8}}

	return map[string]qb.Statement{
		"select": &qb.Select{
			Columns: []qb.Selected{{Value: age}, {Value: name, Alias: "n"}},
			From:    qb.Source{Table: users},
			Where: []qb.Cond{
				qb.Compare{Left: age, Op: qb.OpGt, Right: qb.Param{Value: 18}},
				qb.In{Left: name, Values: []qb.Expr{qb.Param{Value: "a"}, &held[0]}},
				qb.Between{Left: age, Low: &held[1], High: qb.Param{Value: 90}},
			},
			Order: []qb.Order{{Value: name}},
			Limit: new(10),
		},
		"insert": &qb.Insert{
			Table:   users,
			Columns: []string{"name", "a`ge"},
			Rows: [][]qb.Expr{
				{&held[0], &held[1]},
				{&held[2], &held[3]},
				{qb.Param{Value: "cy"}, qb.Default{}},
			},
		},
		"update": &qb.Update{
			Table: users,
			Set:   []qb.Assignment{{Column: "name", Value: &held[0]}},
			Where: []qb.Cond{qb.Compare{Left: age, Op: qb.OpEq, Right: &held[1]}},
		},
		"delete": &qb.Delete{
			Table: users,
			Where: []qb.Cond{qb.Compare{Left: age, Op: qb.OpLt, Right: qb.Param{Value: 3}}},
		},
	}
}

// A statement renders the same through an Appender as through Quote and
// Placeholder.
func TestRenderIsTheSameWithNoAppender(t *testing.T) {
	for _, d := range everyDialect {
		if _, ok := any(plainDialect{d}).(dialect.Appender); ok {
			t.Fatal("the plain dialect has an Appender")
		}

		for name, stmt := range statements() {
			t.Run(d.Name()+"/"+name, func(t *testing.T) {
				text, args, err := d.Render(stmt)
				plainText, plainArgs, plainErr := plainDialect{d}.Render(stmt)

				if (err == nil) != (plainErr == nil) {
					t.Fatalf("errors differ: %v, %v", err, plainErr)
				}

				if text != plainText || !reflect.DeepEqual(args, plainArgs) {
					t.Errorf(
						"got\n%s %v\nwith no Appender\n%s %v",
						text,
						args,
						plainText,
						plainArgs,
					)
				}
			})
		}
	}
}

// A *qb.Param is a qb.Param wherever a statement holds one: bound the same,
// and gathered into one array where a list of values is.
func TestPointerParamsRenderAsParams(t *testing.T) {
	users := &qb.TableRef{Name: "users"}
	age := qb.Column{Table: users, Name: "age"}

	build := func(param func(v any) qb.Expr) []qb.Statement {
		return []qb.Statement{
			&qb.Select{
				Columns: []qb.Selected{{Value: age}},
				From:    qb.Source{Table: users},
				Where: []qb.Cond{
					qb.In{Left: age, Values: []qb.Expr{param(1), param(2), param(3)}},
					qb.Compare{Left: age, Op: qb.OpNe, Right: param(qb.Secret{V: 4})},
				},
			},
			&qb.Insert{
				Table:   users,
				Columns: []string{"age"},
				Rows:    [][]qb.Expr{{param(5)}, {param(nil)}},
			},
		}
	}

	values := build(func(v any) qb.Expr { return qb.Param{Value: v} })
	pointers := build(func(v any) qb.Expr { return &qb.Param{Value: v} })

	for _, d := range everyDialect {
		for i := range values {
			text, args, err := d.Render(values[i])
			if err != nil {
				t.Fatal(err)
			}

			pointerText, pointerArgs, err := d.Render(pointers[i])
			if err != nil {
				t.Fatal(err)
			}

			if text != pointerText || !reflect.DeepEqual(args, pointerArgs) {
				t.Errorf(
					"%s: got\n%s %v\nfor pointers, and for values\n%s %v",
					d.Name(),
					pointerText,
					pointerArgs,
					text,
					args,
				)
			}
		}
	}

	if p, ok := qb.ParamOf(&qb.Param{Value: 1}); !ok || p.Value != 1 {
		t.Error("ParamOf missed a *Param")
	}

	if p, ok := qb.ParamOf(qb.Param{Value: 1}); !ok || p.Value != 1 {
		t.Error("ParamOf missed a Param")
	}

	if _, ok := qb.ParamOf((*qb.Param)(nil)); ok {
		t.Error("ParamOf took a nil *Param")
	}

	if _, ok := qb.ParamOf(age); ok {
		t.Error("ParamOf took a column")
	}
}

// Statements rendered at once do not see each other's text: each buffer is one
// statement's while it is written.
func TestRenderAtOnce(t *testing.T) {
	users := &qb.TableRef{Name: "users"}

	var wg sync.WaitGroup

	for worker := range 16 {
		wg.Go(func() {
			column := fmt.Sprint("c", worker)
			stmt := &qb.Select{
				Columns: []qb.Selected{{Value: qb.Column{Table: users, Name: column}}},
				From:    qb.Source{Table: users},
				Where: []qb.Cond{
					qb.Compare{
						Left:  qb.Column{Name: column},
						Op:    qb.OpEq,
						Right: qb.Param{Value: worker},
					},
				},
			}

			want := fmt.Sprintf(`SELECT "users"."%[1]s" FROM "users" WHERE "%[1]s" = $1`, column)

			for range 2000 {
				text, args, err := postgres.Dialect.Render(stmt)
				if err != nil || text != want || len(args) != 1 || args[0] != worker {
					t.Errorf("worker %d got %q %v %v", worker, text, args, err)

					return
				}
			}
		})
	}

	wg.Wait()
}

type unknownStatement struct{ qb.Statement }

// A statement that fails gives its buffer back clean: the next one starts
// from nothing.
func TestRenderAfterAnError(t *testing.T) {
	users := &qb.TableRef{Name: "users"}
	good := &qb.Delete{Table: users, Where: []qb.Cond{qb.IsNull{Left: qb.Column{Name: "a"}}}}

	// Fails halfway, after writing text and binding a value.
	bad := &qb.Select{
		Columns: []qb.Selected{{Value: qb.Param{Value: 1}}, {Value: nil}},
		From:    qb.Source{Table: users},
	}

	for range 50 {
		if text, args, err := postgres.Dialect.Render(
			bad,
		); err == nil || text != "" ||
			args != nil {
			t.Fatalf("a bad statement gave %q %v %v", text, args, err)
		}

		if _, _, err := postgres.Dialect.Render(unknownStatement{}); err == nil {
			t.Fatal("an unknown statement rendered")
		}

		text, args, err := postgres.Dialect.Render(good)
		if err != nil || text != `DELETE FROM "users" WHERE "a" IS NULL` || len(args) != 0 {
			t.Fatalf("after an error: %q %v %v", text, args, err)
		}
	}
}

// Values of the types that need no conversion reach the driver as they are, in
// every dialect, including those that bind times in UTC.
func TestPlainValuesBindAsTheyAre(t *testing.T) {
	text, number, flag := "s", int64(5), true
	values := []any{
		nil,
		"s",
		[]byte("b"),
		true,
		int(1),
		int8(2),
		int16(3),
		int32(4),
		int64(5),
		uint(6),
		uint8(7),
		uint16(8),
		uint32(9),
		uint64(10),
		float32(1.5),
		float64(2.5),
		&text,
		&number,
		&flag,
		(*string)(nil),
		(*int)(nil),
		(*int32)(nil),
		(*float64)(nil),
	}

	row := make([]qb.Expr, len(values))
	columns := make([]string, len(values))

	for i, v := range values {
		row[i], columns[i] = qb.Param{Value: v}, fmt.Sprint("c", i)
	}

	stmt := &qb.Insert{Table: &qb.TableRef{Name: "t"}, Columns: columns, Rows: [][]qb.Expr{row}}

	for _, d := range everyDialect {
		_, args, err := d.Render(stmt)
		if err != nil {
			t.Fatal(err)
		}

		if !reflect.DeepEqual(args, values) {
			t.Errorf("%s bound %v, want %v", d.Name(), args, values)
		}

		for i, a := range args {
			if p, ok := values[i].(*string); ok && a != any(p) {
				t.Errorf("%s bound another pointer for value %d", d.Name(), i)
			}
		}
	}
}

// A time that is in UTC already is bound as it is, and one that is not is
// still converted, where the database wants UTC.
func TestUTCTimesAreNotCopied(t *testing.T) {
	at := time.Date(2026, 1, 2, 3, 4, 5, 6000, time.UTC)
	local := at.In(time.FixedZone("east", 3*3600))
	stmt := &qb.Select{
		Columns: []qb.Selected{{Value: qb.Param{Value: at}}, {Value: qb.Param{Value: local}}},
	}

	for _, d := range []dialect.Dialect{sqlite.Dialect, sqlserver.Dialect} {
		_, args, err := d.Render(stmt)
		if err != nil {
			t.Fatal(err)
		}

		for i, a := range args {
			got, ok := a.(time.Time)
			if !ok || got != at || got.Location() != time.UTC {
				t.Errorf("%s bound value %d as %v", d.Name(), i, a)
			}
		}
	}

	// Elsewhere a time keeps its zone.
	_, args, err := postgres.Dialect.Render(stmt)
	if err != nil {
		t.Fatal(err)
	}

	if got := args[1].(time.Time); got != local {
		t.Errorf("postgres bound %v, want %v", got, local)
	}
}

// Rendering allocates for the statement's text and its values, and not for
// each identifier and placeholder in it.
func TestRenderAllocations(t *testing.T) {
	stmt := statements()["select"]

	for _, d := range []dialect.Dialect{postgres.Dialect, sqlserver.Dialect} {
		fast := testing.AllocsPerRun(200, func() {
			if _, _, err := d.Render(stmt); err != nil {
				t.Fatal(err)
			}
		})

		slow := testing.AllocsPerRun(200, func() {
			if _, _, err := (plainDialect{d}).Render(stmt); err != nil {
				t.Fatal(err)
			}
		})

		t.Logf("%s: %.0f allocations, %.0f with no Appender", d.Name(), fast, slow)

		// The select has nine identifiers and five placeholders.
		if fast > slow-12 {
			t.Errorf(
				"%s: %.0f allocations, %.0f with no Appender: want at least 12 fewer",
				d.Name(),
				fast,
				slow,
			)
		}
	}
}
