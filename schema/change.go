package schema

import (
	"fmt"
	"strings"
)

// Change is one step of a plan. Dialect.Statements turns it into SQL.
type Change interface {
	// String says what the change does, in a few words.
	String() string
	// Warning says what could go wrong, or is "".
	Warning() string
}

// Online is implemented by changes that cannot run in a transaction.
type Online interface {
	RunsOnline() bool
}

type quiet struct{}

func (quiet) Warning() string { return "" }

// CreateSchema creates a database schema.
type CreateSchema struct {
	quiet
	Name string
}

// String implements Change.
func (c CreateSchema) String() string { return "create schema " + c.Name }

// CreateTable creates a table with its columns, primary key, unique
// constraints, and checks. Its indexes and foreign keys are separate changes.
type CreateTable struct {
	quiet
	Table *Table
}

// String implements Change.
func (c CreateTable) String() string { return "create table " + c.Table.Ref().String() }

// DropTable removes a table.
type DropTable struct {
	Table    Ref
	IfExists bool // no error if it is missing
	Cascade  bool // remove what depends on it too
}

// String implements Change.
func (c DropTable) String() string { return "drop table " + c.Table.String() }

// Warning implements Change.
func (c DropTable) Warning() string {
	return fmt.Sprintf("this drops table %s and all of its data.", c.Table)
}

// RenameTable renames a table.
type RenameTable struct {
	quiet
	Table Ref
	To    string
}

// String implements Change.
func (c RenameTable) String() string { return fmt.Sprintf("rename table %s to %s", c.Table, c.To) }

// MoveTable moves a table to another schema, and gives it a new name there if To
// is set. Its indexes and constraints move with it.
type MoveTable struct {
	quiet
	Table  Ref
	Schema string
	To     string
}

// String implements Change.
func (c MoveTable) String() string {
	return fmt.Sprintf("move table %s to %s", c.Table, Ref{c.Schema, c.name()})
}

// name returns the table's name in its new schema.
func (c MoveTable) name() string {
	if c.To == "" {
		return c.Table.Name
	}

	return c.To
}

// AddColumn adds a column. Backfill is a literal that fills existing rows of a
// NOT NULL column.
type AddColumn struct {
	Table    Ref
	Column   *Column
	Backfill string
}

// String implements Change.
func (c AddColumn) String() string {
	return fmt.Sprintf("add column %s.%s", c.Table, c.Column.Name)
}

// Warning implements Change.
func (c AddColumn) Warning() string {
	switch {
	case c.Column.Nullable || c.Column.Default != "" || c.Column.Identity || c.Column.Generated != "":
		return ""
	case c.Backfill == "":
		return fmt.Sprintf(
			"%s.%s is NOT NULL with no default, so this fails if the table has rows. Add a default, or fill the column in steps.",
			c.Table,
			c.Column.Name,
		)
	}

	return fmt.Sprintf(
		"existing rows of %s get %s in %s. Change the DEFAULT below to fill them differently.",
		c.Table,
		c.Backfill,
		c.Column.Name,
	)
}

// DropColumn removes a column. Again is set when the plan adds the column back
// later, as it does for a generated column that it rebuilds.
type DropColumn struct {
	Table  Ref
	Column *Column
	Again  *Column
}

// String implements Change.
func (c DropColumn) String() string {
	return fmt.Sprintf("drop column %s.%s", c.Table, c.Column.Name)
}

// Warning implements Change.
func (c DropColumn) Warning() string {
	name := c.Table.String() + "." + c.Column.Name

	switch {
	case c.Again == nil:
		return fmt.Sprintf("this drops column %s and its data.", name)
	case c.Column.Generated == "":
		return fmt.Sprintf(
			"%s becomes generated, so it is dropped and added again further down: its stored values are replaced by computed ones, and its indexes and constraints are rebuilt.",
			name,
		)
	case c.Column.Generated != c.Again.Generated:
		return fmt.Sprintf(
			"%s gets a new expression, so it is dropped and added again further down, which rewrites the table and rebuilds its indexes and constraints.",
			name,
		)
	}

	return fmt.Sprintf(
		"%s is computed from a column this plan drops or changes the type of, so it is dropped and added again further down, which rewrites the table and rebuilds its indexes and constraints.",
		name,
	)
}

// RenameColumn renames a column.
type RenameColumn struct {
	quiet
	Table    Ref
	From, To string
}

// String implements Change.
func (c RenameColumn) String() string {
	return fmt.Sprintf("rename column %s.%s to %s", c.Table, c.From, c.To)
}

// AlterColumn changes a column from one definition to another. FromEnum says
// From.Type is a native enum, which casts to another type only through text.
type AlterColumn struct {
	Table    Ref
	From, To *Column
	FromEnum bool
}

// Replaces reports whether the column is dropped and added again, which is how
// it becomes generated or gets a new expression. PostgreSQL before 17 can do
// neither in place, and a migration cannot know which version it will run on.
// Indexes and constraints on the column go with it, so the plan drops and makes
// them around this change. A dialect must replace the column exactly when this
// is true.
func (c AlterColumn) Replaces() bool {
	return c.To.Generated != "" && c.From.Generated != c.To.Generated
}

// String implements Change.
func (c AlterColumn) String() string {
	var what []string

	if c.From.Type != c.To.Type {
		what = append(what, fmt.Sprintf("type %s to %s", c.From.Type, c.To.Type))
	}

	if c.From.Nullable != c.To.Nullable {
		what = append(
			what,
			map[bool]string{true: "allow NULL", false: "require a value"}[c.To.Nullable],
		)
	}

	if c.From.Default != c.To.Default {
		what = append(what, "default")
	}

	if c.From.Identity != c.To.Identity {
		what = append(what, "identity")
	}

	if c.From.Generated != c.To.Generated {
		what = append(what, "generated expression")
	}

	if c.From.Collation != c.To.Collation {
		what = append(what, "collation")
	}

	return fmt.Sprintf("change column %s.%s: %s", c.Table, c.To.Name, strings.Join(what, ", "))
}

// Warning implements Change.
func (c AlterColumn) Warning() string {
	var out []string

	if c.From.Type != c.To.Type {
		out = append(
			out,
			fmt.Sprintf(
				"changing %s.%s from %s to %s may fail or lose data, depending on existing values.",
				c.Table,
				c.To.Name,
				c.From.Type,
				c.To.Type,
			),
		)
	}

	if c.From.Nullable && !c.To.Nullable {
		out = append(
			out,
			fmt.Sprintf(
				"%s.%s becomes NOT NULL, which fails if any row holds NULL.",
				c.Table,
				c.To.Name,
			),
		)
	}

	switch {
	case c.Replaces() && c.From.Generated == "":
		out = append(
			out,
			fmt.Sprintf(
				"%s.%s becomes generated, so it is dropped and added again: its stored values are replaced by computed ones, and its indexes and constraints are rebuilt.",
				c.Table,
				c.To.Name,
			),
		)
	case c.Replaces():
		out = append(
			out,
			fmt.Sprintf(
				"%s.%s gets a new expression, so it is dropped and added again, which rewrites the table and rebuilds its indexes and constraints.",
				c.Table,
				c.To.Name,
			),
		)
	}

	return strings.Join(out, " ")
}

// AddPrimaryKey adds a primary key.
type AddPrimaryKey struct {
	quiet
	Table Ref
	Key   *Key
}

// String implements Change.
func (c AddPrimaryKey) String() string { return "add primary key on " + c.Table.String() }

// DropPrimaryKey removes a primary key.
type DropPrimaryKey struct {
	quiet
	Table Ref
	Key   *Key
}

// String implements Change.
func (c DropPrimaryKey) String() string { return "drop primary key on " + c.Table.String() }

// AddUnique adds a unique constraint.
type AddUnique struct {
	Table Ref
	Key   *Key
}

// String implements Change.
func (c AddUnique) String() string {
	return fmt.Sprintf("add unique %s on %s", c.Key.Name, c.Table)
}

// Warning implements Change.
func (c AddUnique) Warning() string {
	return fmt.Sprintf(
		"this fails if %s already holds duplicates of (%s).",
		c.Table,
		strings.Join(c.Key.Columns, ", "),
	)
}

// AddCheck adds a check constraint.
type AddCheck struct {
	Table Ref
	Check *Check
}

// String implements Change.
func (c AddCheck) String() string {
	return fmt.Sprintf("add check %s on %s", c.Check.Name, c.Table)
}

// Warning implements Change.
func (c AddCheck) Warning() string {
	return fmt.Sprintf("this fails if any row of %s breaks the check.", c.Table)
}

// DropConstraint removes a unique or check constraint.
type DropConstraint struct {
	quiet
	Table Ref
	Name  string
}

// String implements Change.
func (c DropConstraint) String() string {
	return fmt.Sprintf("drop constraint %s on %s", c.Name, c.Table)
}

// RenameConstraint renames a constraint.
type RenameConstraint struct {
	quiet
	Table    Ref
	From, To string
}

// String implements Change.
func (c RenameConstraint) String() string {
	return fmt.Sprintf("rename constraint %s on %s to %s", c.From, c.Table, c.To)
}

// CreateIndex adds an index. New means its table is created by the same plan.
// Atomic means the plan must run in one transaction, as a plan that replaces a
// column must, so the index is built in it even if it asks to be concurrent.
type CreateIndex struct {
	Table  Ref
	Index  *Index
	New    bool
	Atomic bool
}

// String implements Change.
func (c CreateIndex) String() string {
	return fmt.Sprintf("create index %s on %s", c.Index.Name, c.Table)
}

// Warning implements Change.
func (c CreateIndex) Warning() string {
	var out []string

	if c.Index.Unique && !c.New {
		out = append(
			out,
			fmt.Sprintf(
				"this fails if %s already holds duplicates for %s.",
				c.Table,
				c.Index.Name,
			),
		)
	}

	if c.Index.Concurrent && !c.New && c.Atomic {
		out = append(
			out,
			fmt.Sprintf(
				"%s is built without CONCURRENTLY, which blocks writes to %s while it builds, because this migration replaces a column and must run in one transaction. To build it online, move it to a migration of its own.",
				c.Index.Name,
				c.Table,
			),
		)
	}

	return strings.Join(out, " ")
}

// RunsOnline reports whether the index builds without blocking writes, which
// PostgreSQL does not allow in a transaction. An index on a new table has no
// writes to block.
func (c CreateIndex) RunsOnline() bool { return c.Index.Concurrent && !c.New && !c.Atomic }

// DropIndex removes an index. Atomic means the plan must run in one
// transaction, as for CreateIndex.
type DropIndex struct {
	quiet
	Table  Ref
	Index  *Index
	Atomic bool
}

// String implements Change.
func (c DropIndex) String() string {
	return fmt.Sprintf("drop index %s on %s", c.Index.Name, c.Table)
}

// RunsOnline reports whether the index goes without blocking writes.
func (c DropIndex) RunsOnline() bool { return c.Index.Concurrent && !c.Atomic }

// RenameIndex renames an index.
type RenameIndex struct {
	quiet
	Table    Ref
	From, To string
}

// String implements Change.
func (c RenameIndex) String() string {
	return fmt.Sprintf("rename index %s on %s to %s", c.From, c.Table, c.To)
}

// AddForeignKey adds a foreign key. New means its table is created by the same
// plan.
type AddForeignKey struct {
	Table Ref
	Key   *ForeignKey
	New   bool
}

// String implements Change.
func (c AddForeignKey) String() string {
	return fmt.Sprintf("add foreign key %s on %s", c.Key.Name, c.Table)
}

// Warning implements Change.
func (c AddForeignKey) Warning() string {
	if c.New {
		return ""
	}

	return fmt.Sprintf("this fails if a row of %s refers to a row that does not exist.", c.Table)
}

// DropForeignKey removes a foreign key.
type DropForeignKey struct {
	quiet
	Table Ref
	Key   *ForeignKey
}

// String implements Change.
func (c DropForeignKey) String() string {
	return fmt.Sprintf("drop foreign key %s on %s", c.Key.Name, c.Table)
}

// CreateEnum creates a native enum type.
type CreateEnum struct {
	quiet
	Enum *Enum
}

// String implements Change.
func (c CreateEnum) String() string { return "create enum " + c.Enum.Name }

// AddEnumValue adds a value to an enum, before value Before if it is set, and
// last otherwise, so that the enum sorts as the models list it. Used says the
// same plan uses the value, which PostgreSQL allows only once the value is
// committed, so the plan cannot run in one transaction.
type AddEnumValue struct {
	Enum   string
	Value  string
	Before string
	Used   bool
}

// String implements Change.
func (c AddEnumValue) String() string {
	return fmt.Sprintf("add value %s to enum %s", c.Value, c.Enum)
}

// Warning implements Change.
func (c AddEnumValue) Warning() string {
	if !c.Used {
		return ""
	}

	return fmt.Sprintf(
		"this migration uses the new value %s of enum %s, which PostgreSQL allows only once the value is committed, so the file runs without a transaction. To keep one, add the value in a migration of its own.",
		c.Value,
		c.Enum,
	)
}

// RunsOnline reports whether the value must be committed before the rest of the
// plan runs.
func (c AddEnumValue) RunsOnline() bool { return c.Used }

// ReorderEnum reports an enum whose values sort in another order than the models
// list, which needs a person: PostgreSQL cannot reorder them.
type ReorderEnum struct {
	Enum   string
	Values []string // as the models list them
}

// String implements Change.
func (c ReorderEnum) String() string { return "reorder the values of enum " + c.Enum }

// Warning implements Change.
func (c ReorderEnum) Warning() string {
	return fmt.Sprintf(
		"PostgreSQL cannot reorder the values of enum %s to %s. Rebuild the type by hand: create a new one, move the columns over, and drop the old one.",
		c.Enum,
		strings.Join(c.Values, ", "),
	)
}

// AlterSequence changes how a sequence counts. Its current value is kept.
type AlterSequence struct {
	From, To *Sequence
}

// String implements Change.
func (c AlterSequence) String() string { return "change sequence " + c.To.Name }

// Warning implements Change.
func (c AlterSequence) Warning() string {
	if c.From.start() == c.To.start() {
		return ""
	}

	return fmt.Sprintf(
		"a new start for sequence %s does not move its current value. Run ALTER SEQUENCE ... RESTART by hand if you mean that.",
		c.To.Name,
	)
}

// DropEnum removes an enum.
type DropEnum struct {
	quiet
	Enum string
}

// String implements Change.
func (c DropEnum) String() string { return "drop enum " + c.Enum }

// RemoveEnumValue reports a value gone from an enum, which needs a person: rows
// may hold it, and PostgreSQL cannot drop one.
type RemoveEnumValue struct {
	Enum  string
	Value string
}

// String implements Change.
func (c RemoveEnumValue) String() string {
	return fmt.Sprintf("remove value %s from enum %s", c.Value, c.Enum)
}

// Warning implements Change.
func (c RemoveEnumValue) Warning() string {
	return fmt.Sprintf(
		"PostgreSQL cannot remove value %s from enum %s. Rebuild the type by hand: create a new one, move the columns over, and drop the old one.",
		c.Value,
		c.Enum,
	)
}

// CreateSequence creates a sequence.
type CreateSequence struct {
	quiet
	Sequence *Sequence
}

// String implements Change.
func (c CreateSequence) String() string { return "create sequence " + c.Sequence.Name }

// DropSequence removes a sequence.
type DropSequence struct {
	quiet
	Sequence string
}

// String implements Change.
func (c DropSequence) String() string { return "drop sequence " + c.Sequence }

// SetTableComment changes a table's comment.
type SetTableComment struct {
	quiet
	Table   Ref
	Comment string
}

// String implements Change.
func (c SetTableComment) String() string { return "set comment on table " + c.Table.String() }

// SetColumnComment changes a column's comment.
type SetColumnComment struct {
	quiet
	Table   Ref
	Column  string
	Comment string
}

// String implements Change.
func (c SetColumnComment) String() string {
	return fmt.Sprintf("set comment on column %s.%s", c.Table, c.Column)
}
