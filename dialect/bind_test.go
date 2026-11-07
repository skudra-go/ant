package dialect_test

import (
	"database/sql"
	"database/sql/driver"
	"reflect"
	"testing"

	"github.com/skudra-go/ant/dialect"
	"github.com/skudra-go/ant/dialect/postgres"
	"github.com/skudra-go/ant/dialect/sqlite"
	"github.com/skudra-go/ant/qb"
)

func TestBind(t *testing.T) {
	age, name := sql.Named("age", 18), sql.Named("name", "x")

	cases := []struct {
		name, in, want string
		args, bound    []any
	}{
		{"one", "age > @age", "age > $1", []any{age}, []any{18}},
		{
			"repeat binds once",
			"a = @age OR b = @age AND c = @name",
			"a = $1 OR b = $1 AND c = $2",
			[]any{age, name},
			[]any{18, "x"},
		},
		{"string", "a = '@age' AND b = @age", "a = '@age' AND b = $1", []any{age}, []any{18}},
		{
			"escaped quote",
			"a = 'it''s @age' AND b = @age",
			"a = 'it''s @age' AND b = $1",
			[]any{age},
			[]any{18},
		},
		{"identifier", `"@age" = @age`, `"@age" = $1`, []any{age}, []any{18}},
		{"line comment", "a -- @nope\n= @age", "a -- @nope\n= $1", []any{age}, []any{18}},
		{"block comment", "/* @nope */ a = @age", "/* @nope */ a = $1", []any{age}, []any{18}},
		{"dollar quote", "$x$ @nope $x$ = @age", "$x$ @nope $x$ = $1", []any{age}, []any{18}},
		{
			"operators",
			"a @> b AND c @@ d AND e = @age",
			"a @> b AND c @@ d AND e = $1",
			[]any{age},
			[]any{18},
		},
		{"positional placeholder", "a = $1", "a = $1", []any{1}, []any{1}},
		{"no arguments", "SELECT 1", "SELECT 1", nil, nil},
		{"question marks", "a = ? AND b = ?", "a = $1 AND b = $2", []any{1, 2}, []any{1, 2}},
		{"marks in text stay", "a = '?' AND b = ?", "a = '?' AND b = $1", []any{1}, []any{1}},
		{"jsonb operator with no arguments", "a ? 'k'", "a ? 'k'", nil, nil},
		{"jsonb operator with $1", "a ? 'k' AND b = $1", "a ? 'k' AND b = $1", []any{1}, []any{1}},
		{"jsonb operator with a value", "a ? $1", "a ? $1", []any{"k"}, []any{"k"}},
		{"more marks than arguments", "a ? 'k' AND b = ?", "a ? 'k' AND b = ?", []any{1}, []any{1}},
		{
			"list in marks",
			"a IN (?) AND b = ?",
			"a IN ($1, $2) AND b = $3",
			[]any{[]int{1, 2}, 3},
			[]any{1, 2, 3},
		},
		{
			"list in names",
			"a IN ( @ids ) OR b in (@ids)",
			"a IN ( $1, $2 ) OR b in ($1, $2)",
			[]any{sql.Named("ids", []int{1, 2})},
			[]any{1, 2},
		},
		{
			"NOT IN expands",
			"a NOT IN (@ids)",
			"a NOT IN ($1)",
			[]any{sql.Named("ids", []int{1})},
			[]any{1},
		},
		{"empty list", "a IN (@ids)", "a IN (NULL)", []any{sql.Named("ids", []int{})}, nil},
		{
			"array outside IN",
			"a = ANY(@ids)",
			"a = ANY($1)",
			[]any{sql.Named("ids", []int{1, 2})},
			[]any{[]int{1, 2}},
		},
		{
			"function ending in IN is not IN",
			"margin(@ids)",
			"margin($1)",
			[]any{sql.Named("ids", []int{1})},
			[]any{[]int{1}},
		},
		{
			"part of a list",
			"a IN (@ids, 9)",
			"a IN ($1, 9)",
			[]any{sql.Named("ids", []int{1})},
			[]any{[]int{1}},
		},
		{"bytes are one value", "a IN (?)", "a IN ($1)", []any{[]byte("x")}, []any{[]byte("x")}},
		{
			"positional slices stay arrays",
			"a = ANY($1)",
			"a = ANY($1)",
			[]any{[]int{1}},
			[]any{[]int{1}},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, bound, err := dialect.Bind(postgres.Dialect, c.in, c.args)
			if err != nil {
				t.Fatal(err)
			}

			if got != c.want || !reflect.DeepEqual(bound, c.bound) {
				t.Errorf("got %q %v, want %q %v", got, bound, c.want, c.bound)
			}
		})
	}
}

func TestBindErrors(t *testing.T) {
	age := sql.Named("age", 18)
	for name, c := range map[string]struct {
		in   string
		args []any
	}{
		"missing argument": {"a = @age AND b = @other", []any{age}},
		"unused argument":  {"a = 1", []any{age}},
		"mixed with $1":    {"a = @age AND b = $1", []any{age, 1}},
	} {
		t.Run(name, func(t *testing.T) {
			if _, _, err := dialect.Bind(postgres.Dialect, c.in, c.args); err == nil {
				t.Error("want an error")
			}
		})
	}
}

// temp converts itself only through a pointer, as a Value method with a pointer
// receiver does, which database/sql would not find on the value it is given.
type temp struct{ milli int64 }

func (t *temp) Value() (driver.Value, error) { return t.milli, nil }

func TestPointerValuersAreBoundByPointer(t *testing.T) {
	check := func(t *testing.T, args []any) {
		t.Helper()

		for _, a := range args {
			v, ok := a.(driver.Valuer)
			if !ok {
				t.Fatalf("%T is not a Valuer", a)
			}

			if got, err := v.Value(); err != nil || got != int64(7) {
				t.Errorf("Value() = %v, %v", got, err)
			}
		}
	}

	_, args, err := dialect.Bind(postgres.Dialect, "SELECT $1", []any{temp{7}})
	if err != nil {
		t.Fatal(err)
	}

	check(t, args)

	_, args, err = postgres.Dialect.Render(&qb.Select{
		Columns: []qb.Selected{{Value: qb.Param{Value: temp{7}}}},
	})
	if err != nil {
		t.Fatal(err)
	}

	check(t, args)

	_, args, err = sqlite.Dialect.Render(&qb.Select{
		Columns: []qb.Selected{{Value: qb.Param{Value: qb.Secret{V: temp{7}}}}},
	})
	if err != nil {
		t.Fatal(err)
	}

	check(t, args)
}
