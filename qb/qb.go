// Package qb is the dialect-neutral query AST. The query builder produces these
// nodes, and a dialect renders them to SQL. It is public so that a dialect outside
// this module can implement dialect.Dialect, and most programs never need it.
package qb

import (
	"context"
	"database/sql/driver"
	"log/slog"
	"slices"
)

// TableRef names a table. Nodes share a pointer, so a name set by model
// configuration reaches nodes built before it ran.
type TableRef struct {
	Schema string
	Name   string
}

// Expr is a value in a statement.
type Expr interface{ expr() }

// Column names a column. Alias, when set, replaces the table qualifier, and a
// nil Table with no Alias leaves the column unqualified.
type Column struct {
	Table *TableRef
	Alias string
	Name  string
}

// Param is a bound value.
type Param struct{ Value any }

// Secret holds a value that must stay out of logs. It reaches the driver as the
// value inside, and prints as [redacted].
type Secret struct{ V any }

// Value implements driver.Valuer.
func (s Secret) Value() (driver.Value, error) {
	return driver.DefaultParameterConverter.ConvertValue(s.V)
}

// String implements fmt.Stringer.
func (Secret) String() string { return "[redacted]" }

// LogValue implements slog.LogValuer.
func (Secret) LogValue() slog.Value { return slog.StringValue("[redacted]") }

// Star is the * in COUNT(*).
type Star struct{}

// Default is the DEFAULT keyword in an INSERT.
type Default struct{}

// Func is a function call. Over makes it a window function.
type Func struct {
	Name     string
	Args     []Expr
	Distinct bool
	Filter   []Cond
	Over     *Window
}

// Window is an OVER clause.
type Window struct {
	PartitionBy []Expr
	OrderBy     []Order
}

// Fragment is raw SQL. Each ? outside a quoted region takes the next argument.
// A fragment works as an expression and as a condition.
type Fragment struct {
	SQL  string
	Args []Expr
	// Needs names a capability the text depends on, so a database without it
	// gets a clear error. See dialect.Feature.
	Needs string
}

// Binary is a binary operator, such as addition.
type Binary struct {
	Left  Expr
	Op    string
	Right Expr
}

// Subquery is a parenthesized SELECT.
type Subquery struct{ Select *Select }

// When is one branch of a Case.
type When struct {
	If   Cond
	Then Expr
}

// Case is a CASE expression.
type Case struct {
	Whens []When
	Else  Expr
}

// Collate applies a collation.
type Collate struct {
	Value     Expr
	Collation string
}

// Cast converts a value to a database type.
type Cast struct {
	Value Expr
	Type  string
}

// Row is a parenthesized list of values, as in (a, b) IN ((1, 2)).
type Row struct{ Values []Expr }

func (Cast) expr()     {}
func (Row) expr()      {}
func (Column) expr()   {}
func (Param) expr()    {}
func (Star) expr()     {}
func (Default) expr()  {}
func (Func) expr()     {}
func (Fragment) expr() {}
func (Binary) expr()   {}
func (Subquery) expr() {}
func (Case) expr()     {}
func (Collate) expr()  {}
func (Exists) expr()   {}

// Op is a comparison operator.
type Op int

// Comparison operators.
const (
	OpEq Op = iota
	OpNe
	OpGt
	OpGte
	OpLt
	OpLte
	OpLike
	OpILike
)

// Cond is a node in a condition tree.
type Cond interface{ cond() }

// Compare compares two expressions.
type Compare struct {
	Left  Expr
	Op    Op
	Right Expr
}

// In tests membership in a list, or in a subquery if Sub is set.
type In struct {
	Left   Expr
	Values []Expr
	Sub    *Select
	Negate bool
}

// Between tests an inclusive range.
type Between struct {
	Left      Expr
	Low, High Expr
}

// IsNull tests for NULL.
type IsNull struct {
	Left   Expr
	Negate bool
}

// Exists tests whether a subquery has rows.
type Exists struct {
	Select *Select
	Negate bool
}

// Lazy is a condition built at render time, when the call's context is known.
// Global filters use it, so they reach subqueries built with no context. A nil
// condition from Build matches every row.
type Lazy struct {
	Build func(context.Context) (Cond, error)
}

// And is true when every child is true.
type And struct{ Children []Cond }

// Or is true when any child is true.
type Or struct{ Children []Cond }

// Not negates its child.
type Not struct{ Child Cond }

func (Compare) cond()  {}
func (In) cond()       {}
func (Between) cond()  {}
func (IsNull) cond()   {}
func (Exists) cond()   {}
func (Fragment) cond() {}
func (Lazy) cond()     {}
func (And) cond()      {}
func (Or) cond()       {}
func (Not) cond()      {}

// MatchesAll reports whether c is true for every row regardless of the data,
// such as an And with no children or a NotIn with no values.
func MatchesAll(c Cond) bool {
	switch c := c.(type) {
	case And:
		return !slices.ContainsFunc(c.Children, func(child Cond) bool { return !MatchesAll(child) })
	case Or:
		return slices.ContainsFunc(c.Children, MatchesAll)
	case In:
		return c.Negate && c.Sub == nil && len(c.Values) == 0
	case Not:
		return matchesNone(c.Child)
	}

	return false
}

func matchesNone(c Cond) bool {
	switch c := c.(type) {
	case And:
		return slices.ContainsFunc(c.Children, matchesNone)
	case Or:
		return !slices.ContainsFunc(
			c.Children,
			func(child Cond) bool { return !matchesNone(child) },
		)
	case In:
		return !c.Negate && c.Sub == nil && len(c.Values) == 0
	case Not:
		return MatchesAll(c.Child)
	}

	return false
}

// Aliased returns c with columns of table qualified by alias instead, however
// deep they sit in its expressions. It lets a subquery use its outer table's
// conditions on a self-reference. A nested SELECT is left as it is, since its
// columns belong to its own FROM.
func Aliased(c Cond, table *TableRef, alias string) Cond {
	expr := func(e Expr) Expr { return aliasedExpr(e, table, alias) }

	switch c := c.(type) {
	case Compare:
		return Compare{expr(c.Left), c.Op, expr(c.Right)}
	case In:
		return In{expr(c.Left), aliasedExprs(c.Values, table, alias), c.Sub, c.Negate}
	case Between:
		return Between{expr(c.Left), expr(c.Low), expr(c.High)}
	case IsNull:
		return IsNull{expr(c.Left), c.Negate}
	case Fragment:
		c.Args = aliasedExprs(c.Args, table, alias)

		return c
	case Lazy:
		return Lazy{
			Build: func(ctx context.Context) (Cond, error) {
				built, err := c.Build(ctx)
				if err != nil || built == nil {
					return built, err
				}

				return Aliased(built, table, alias), nil
			},
		}
	case And:
		return And{aliasedConds(c.Children, table, alias)}
	case Or:
		return Or{aliasedConds(c.Children, table, alias)}
	case Not:
		return Not{Aliased(c.Child, table, alias)}
	}

	return c
}

func aliasedConds(in []Cond, table *TableRef, alias string) []Cond {
	if in == nil {
		return nil
	}

	out := make([]Cond, len(in))

	for i, c := range in {
		out[i] = Aliased(c, table, alias)
	}

	return out
}

func aliasedExprs(in []Expr, table *TableRef, alias string) []Expr {
	if in == nil {
		return nil
	}

	out := make([]Expr, len(in))

	for i, e := range in {
		out[i] = aliasedExpr(e, table, alias)
	}

	return out
}

func aliasedExpr(e Expr, table *TableRef, alias string) Expr {
	expr := func(e Expr) Expr { return aliasedExpr(e, table, alias) }

	switch e := e.(type) {
	case Column:
		if e.Table == table && e.Alias == "" {
			e.Alias = alias
		}

		return e
	case Func:
		e.Args = aliasedExprs(e.Args, table, alias)
		e.Filter = aliasedConds(e.Filter, table, alias)

		if e.Over != nil {
			over := Window{PartitionBy: aliasedExprs(e.Over.PartitionBy, table, alias)}

			for _, o := range e.Over.OrderBy {
				o.Value = expr(o.Value)
				over.OrderBy = append(over.OrderBy, o)
			}

			e.Over = &over
		}

		return e
	case Fragment:
		e.Args = aliasedExprs(e.Args, table, alias)

		return e
	case Binary:
		return Binary{expr(e.Left), e.Op, expr(e.Right)}
	case Case:
		whens := make([]When, len(e.Whens))

		for i, w := range e.Whens {
			whens[i] = When{Aliased(w.If, table, alias), expr(w.Then)}
		}

		return Case{whens, expr(e.Else)}
	case Collate:
		return Collate{expr(e.Value), e.Collation}
	case Cast:
		return Cast{expr(e.Value), e.Type}
	case Row:
		return Row{aliasedExprs(e.Values, table, alias)}
	}

	return e
}

// Assignment sets a column in an UPDATE.
type Assignment struct {
	Column string
	Value  Expr
}

// Order is one ORDER BY term.
type Order struct {
	Value Expr
	Desc  bool
	Nulls NullsOrder
}

// NullsOrder places NULLs in an ordering.
type NullsOrder int

// Placements of NULLs.
const (
	NullsDefault NullsOrder = iota
	NullsFirst
	NullsLast
)

// Selected is one item of a SELECT list.
type Selected struct {
	Value Expr
	Alias string
}

// Source is a table, subquery, name, or raw SQL in a FROM or JOIN.
type Source struct {
	Table  *TableRef
	Sub    *Select
	Name   string // a CTE or other unqualified name
	Raw    *Fragment
	Values *Values
	Alias  string
}

// Values is a VALUES list used as a table. Columns name its columns.
type Values struct {
	Columns []string
	Rows    [][]Expr
}

// JoinKind is the type of a join.
type JoinKind int

// Join kinds.
const (
	InnerJoin JoinKind = iota
	LeftJoin
	RightJoin
	FullJoin
	CrossJoin
)

// Join is one JOIN clause. Without On or Using, it is unconditional.
type Join struct {
	Kind    JoinKind
	Source  Source
	On      []Cond
	Using   []string
	Lateral bool
}

// Lock is a row-locking clause.
type Lock struct {
	Mode       string // "UPDATE" or "SHARE"
	NoWait     bool
	SkipLocked bool
}

// CTE is one WITH entry. Key names the columns that tell its rows apart, for a
// database whose recursive WITH must stop a cycle itself, as SQL Server's does:
// it has no UNION that drops the rows already reached.
type CTE struct {
	Name   string
	Select *Select
	Key    []string
}

// SetOp is a UNION, INTERSECT, or EXCEPT arm.
type SetOp struct {
	Kind   string
	All    bool
	Select *Select
}

// Select is a SELECT statement.
type Select struct {
	With      []CTE
	Recursive bool
	Comment   string
	Hint      string // an optimizer hint, written /*+ like this */
	Distinct  bool
	Columns   []Selected
	From      Source
	Joins     []Join
	Where     []Cond
	GroupBy   []Expr
	Having    []Cond
	Sets      []SetOp
	Order     []Order
	Limit     *int
	Offset    *int
	Lock      *Lock
}

// Conflict is an ON CONFLICT clause. With no Set it does nothing.
type Conflict struct {
	Columns []string
	Set     []Assignment
	Where   []Cond
}

// Insert is an INSERT statement. Rows holds VALUES, From an INSERT ... SELECT,
// and with neither the row is all defaults.
type Insert struct {
	Table     *TableRef
	Columns   []string
	Rows      [][]Expr
	From      *Select
	Conflict  *Conflict
	Returning []Selected
}

// Update is an UPDATE statement.
type Update struct {
	Table     *TableRef
	Set       []Assignment
	Where     []Cond
	Returning []Selected
}

// Delete is a DELETE statement.
type Delete struct {
	Table     *TableRef
	Where     []Cond
	Returning []Selected
}

// MergeAction is one WHEN clause of a Merge. With no Set and no Columns it is
// DELETE for a matched row and DO NOTHING for an unmatched one.
type MergeAction struct {
	Matched bool
	If      []Cond
	Set     []Assignment
	Columns []string
	Values  []Expr
	Delete  bool
}

// Merge is a MERGE statement.
type Merge struct {
	Table   *TableRef
	Using   Source
	On      []Cond
	Actions []MergeAction
}

// Statement is a complete statement a dialect can render.
type Statement interface{ statement() }

func (*Merge) statement()  {}
func (*Select) statement() {}
func (*Insert) statement() {}
func (*Update) statement() {}
func (*Delete) statement() {}

// Contextual carries the call's context to the renderer, for Lazy conditions.
type Contextual struct {
	Context   context.Context
	Statement Statement
}

func (Contextual) statement() {}
