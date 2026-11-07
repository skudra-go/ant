package schema

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
	"unicode/utf16"
	"unicode/utf8"
)

// Hint is a possible rename that a diff saw but cannot be sure of. A rename and a
// drop plus an add look identical, and only a person knows which was meant.
type Hint struct {
	Table Ref
	// Column is empty when the hint is about the table itself.
	Column string
	To     string
	// Schema is set when the table seems to have moved to another schema.
	Schema string
}

// Flag returns the value for a rename in Diff that takes up this hint. A name
// with a dot, a quote, or an equals sign in it is quoted.
func (h Hint) Flag() string {
	table := flagName(h.Table.Name)

	if h.Table.Schema != "" {
		table = flagName(h.Table.Schema) + "." + table
	}

	switch {
	case h.Column != "":
		return fmt.Sprintf("%s.%s=%s", table, flagName(h.Column), flagName(h.To))
	case h.moves():
		return table + "=" + flagName(h.Schema) + "." + flagName(h.To)
	}

	return table + "=" + flagName(h.To)
}

// moves reports whether the hint is of a table that moved to another schema.
func (h Hint) moves() bool { return h.Column == "" && h.Schema != "" && h.Schema != h.Table.Schema }

// flagName writes a name for a rename, in double quotes if it holds anything a
// rename reads as punctuation.
func flagName(name string) string {
	if !strings.ContainsAny(name, `."=`) {
		return name
	}

	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}

// Change returns the rename this hint suggests.
func (h Hint) Change() Change {
	switch {
	case h.moves():
		return MoveTable{Table: h.Table, Schema: h.Schema, To: h.To}
	case h.Column == "":
		return RenameTable{Table: h.Table, To: h.To}
	}

	return RenameColumn{Table: h.Table, From: h.Column, To: h.To}
}

// String says what was seen, for a person to judge.
func (h Hint) String() string {
	switch {
	case h.moves():
		return fmt.Sprintf("table %s was removed and %s was added", h.Table, Ref{h.Schema, h.To})
	case h.Column == "":
		return fmt.Sprintf("table %s was removed and %s was added", h.Table, h.To)
	}

	return fmt.Sprintf("column %s.%s was removed and %s was added", h.Table, h.Column, h.To)
}

// Plan is the ordered list of changes from one schema to another.
type Plan struct {
	Changes []Change
	Hints   []Hint

	// values holds, by enum, the values the plan can use, in the models'
	// order: all of a new enum's, and those an enum had before, since a value
	// it adds is not usable until committed.
	values map[string][]string
}

// Empty reports whether there is nothing to do.
func (p *Plan) Empty() bool { return len(p.Changes) == 0 }

// Warnings returns the warnings of every change.
func (p *Plan) Warnings() []string {
	var out []string

	for _, c := range p.Changes {
		if w := c.Warning(); w != "" {
			out = append(out, w)
		}
	}

	return out
}

// Online reports whether any change must run outside a transaction.
func (p *Plan) Online() bool {
	return slices.ContainsFunc(p.Changes, func(c Change) bool {
		o, ok := c.(Online)

		return ok && o.RunsOnline()
	})
}

// phases hold changes in the order they must run. Foreign keys go first and
// return last, so that nothing depends on what changes in between, and table and
// column renames come before everything, so that later steps can use the new
// names. Index and constraint renames wait for the drops, as a name may move
// from one that goes. A generated column reads other columns, so it stops being
// generated, or is dropped, before they change, and is added after.
type phases struct {
	schemas, types, renames, dropKeys, dropRest, dropTables, objectRenames []Change
	dropExpressions, dropGenerated, dropColumns, createTables, addColumns  []Change
	alterColumns, addGenerated, addRest, addKeys, comments, dropTypes      []Change

	// enums are the names of the old schema's enum types.
	enums []string

	// unique holds the columns of each primary key, unique constraint, and
	// unique index that the plan drops from a table it keeps. A foreign key that
	// points at the same columns may depend on one of them, and PostgreSQL will
	// not drop it until the foreign key is gone.
	unique map[Ref][]columns
}

func (p *phases) plan(old, to *Schema) []Change {
	return slices.Concat(
		p.schemas,
		p.types,
		p.renames,
		p.dropKeys,
		p.dropRest,
		p.dropTables,
		orderRenames(p.objectRenames, old, to),
		p.dropExpressions,
		p.dropGenerated,
		p.dropColumns,
		p.createTables,
		p.addColumns,
		p.alterColumns,
		p.addGenerated,
		p.addRest,
		p.addKeys,
		p.comments,
		p.dropTypes,
	)
}

// usesValue reports whether an expression of s writes value as a literal, as a
// default, a check, a generated column, or an index may. It errs toward yes, as
// the same text in another enum, or in a string, counts too, which costs only
// the transaction a plan would otherwise run in.
func usesValue(s *Schema, value string) bool {
	literal := "'" + strings.ReplaceAll(value, "'", "''") + "'"
	in := func(expr string) bool { return strings.Contains(expr, literal) }

	for _, t := range s.Tables {
		if slices.ContainsFunc(t.Columns, func(c *Column) bool {
			return in(c.Default) || in(c.Generated)
		}) ||
			slices.ContainsFunc(t.Checks, func(c *Check) bool { return in(c.Expr) }) ||
			slices.ContainsFunc(t.Indexes, func(i *Index) bool {
				return in(i.Where) || slices.ContainsFunc(i.Parts, func(p IndexPart) bool {
					return in(p.Expr)
				})
			}) {
			return true
		}
	}

	return false
}

// enum reports whether typ is one of the old schema's enum types.
func (p *phases) enum(typ string) bool { return slices.Contains(p.enums, typ) }

// compareTypes works out what changes in enums and sequences. They come before
// tables, which use them, and go after, once nothing does.
func (p *phases) compareTypes(old, to *Schema) {
	for _, e := range to.Enums {
		existing := old.Enum(e.Name)
		if existing == nil {
			p.types = append(p.types, CreateEnum{Enum: e})

			continue
		}

		for i, value := range e.Values {
			if slices.Contains(existing.Values, value) {
				continue
			}

			// It goes before the next value the enum has already, or last.
			before := ""

			for _, next := range e.Values[i+1:] {
				if slices.Contains(existing.Values, next) {
					before = next

					break
				}
			}

			p.types = append(p.types, AddEnumValue{
				Enum:   e.Name,
				Value:  value,
				Before: before,
				Used:   usesValue(to, value),
			})
		}

		for _, value := range existing.Values {
			if !slices.Contains(e.Values, value) {
				p.types = append(p.types, RemoveEnumValue{Enum: e.Name, Value: value})
			}
		}

		kept := func(values, in []string) []string {
			return slices.DeleteFunc(slices.Clone(values), func(v string) bool {
				return !slices.Contains(in, v)
			})
		}

		if !slices.Equal(kept(existing.Values, e.Values), kept(e.Values, existing.Values)) {
			p.types = append(p.types, ReorderEnum{Enum: e.Name, Values: e.Values})
		}
	}

	for _, e := range old.Enums {
		if to.Enum(e.Name) == nil {
			p.dropTypes = append(p.dropTypes, DropEnum{Enum: e.Name})
		}
	}

	for _, q := range to.Sequences {
		switch existing := old.Sequence(q.Name); {
		case existing == nil:
			p.types = append(p.types, CreateSequence{Sequence: q})
		case existing.start() != q.start() || existing.increment() != q.increment():
			p.types = append(p.types, AlterSequence{From: existing, To: q})
		}
	}

	for _, q := range old.Sequences {
		if to.Sequence(q.Name) == nil {
			p.dropTypes = append(p.dropTypes, DropSequence{Sequence: q.Name})
		}
	}
}

// Diff returns the plan from from to to. Either can be nil, meaning empty.
//
// Each rename says that something in from has a new name in to, as
// table=new_name or table.column=new_name, and a table can be written
// schema.table. A rename the diff cannot apply is an error, because the caller
// meant it to prevent a drop.
func Diff(from, to *Schema, renames ...string) (*Plan, error) {
	old := from.Clone()

	if to == nil {
		to = &Schema{}
	}

	var p phases

	renamed, err := old.renames(renames, to)
	if err != nil {
		return nil, err
	}

	p.renames = renamed

	for _, name := range to.Schemas {
		if !slices.Contains(old.Schemas, name) {
			p.schemas = append(p.schemas, CreateSchema{Name: name})
		}
	}

	for _, e := range old.Enums {
		p.enums = append(p.enums, e.Name)
	}

	p.compareTypes(old, to)

	gone, retyped := rebuilt(old, to)

	for _, t := range to.Tables {
		o := old.Table(t.Ref())
		if o == nil {
			p.createTable(t)

			continue
		}

		p.alterTable(o, t, gone)
	}

	// Foreign keys wait until every table has said which unique keys it drops.
	for _, t := range to.Tables {
		p.foreignKeys(old.Table(t.Ref()), t, gone, retyped)
	}

	for _, o := range old.Tables {
		if to.Table(o.Ref()) == nil {
			for _, key := range o.ForeignKeys {
				p.dropKeys = append(p.dropKeys, DropForeignKey{Table: o.Ref(), Key: key})
			}

			p.dropTables = append(p.dropTables, DropTable{Table: o.Ref()})
		}
	}

	changes := p.plan(old, to)

	if len(gone) > 0 {
		atomic(changes)
	}

	values := map[string][]string{}

	for _, e := range to.Enums {
		values[e.Name] = e.Values

		if existing := old.Enum(e.Name); existing != nil {
			values[e.Name] = slices.DeleteFunc(slices.Clone(e.Values), func(v string) bool {
				return !slices.Contains(existing.Values, v)
			})
		}
	}

	return &Plan{Changes: changes, Hints: hints(old, to), values: values}, nil
}

// canRenameTable checks that t's new name is a table the models have, and that
// the snapshot does not have, or has only in a table the same batch renames away,
// and that no other rename of the batch takes. A rename to a name the models lack
// would be followed by a drop of the renamed table, losing what the rename was
// meant to keep.
func (s *Schema) canRenameTable(t *Table, ref Ref, to *Schema, claimed, leaving bool) error {
	if ref == t.Ref() || claimed || s.Table(ref) != nil && !leaving {
		return fmt.Errorf("the snapshot already has a table %s", ref)
	}

	if slices.Contains(s.Unmanaged, ref) {
		return fmt.Errorf("a table %s already exists, outside migrations", ref)
	}

	if to.Table(ref) != nil {
		return nil
	}

	var fresh []string

	for _, candidate := range to.Tables {
		if candidate.Schema == ref.Schema && s.Table(candidate.Ref()) == nil &&
			!slices.Contains(s.Unmanaged, candidate.Ref()) {
			fresh = append(fresh, candidate.Name)
		}
	}

	where := ""

	if ref.Schema != "" {
		where = " in schema " + ref.Schema
	}

	if len(fresh) == 0 {
		return fmt.Errorf(
			"models have no table %s%s; the models have no new table",
			ref.Name,
			where,
		)
	}

	return fmt.Errorf(
		"models have no table %s%s; new tables in the models: %s",
		ref.Name,
		where,
		strings.Join(fresh, ", "),
	)
}

// canRenameColumn checks that column c of t can take the new name: the models'
// table has it, and t does not, unless the column that has it is renamed away
// by the same batch, and no other rename of the batch takes it.
func canRenameColumn(t *Table, c *Column, name string, to *Schema, claimed, leaving bool) error {
	if name == c.Name || claimed || t.Column(name) != nil && !leaving {
		return fmt.Errorf("table %s already has a column %s", t.Ref(), name)
	}

	target := to.Table(t.Ref())
	if target == nil {
		return fmt.Errorf("models have no table %s", t.Ref())
	}

	if target.Column(name) != nil {
		return nil
	}

	var fresh []string

	for _, c := range target.Columns {
		if t.Column(c.Name) == nil {
			fresh = append(fresh, c.Name)
		}
	}

	if len(fresh) == 0 {
		return fmt.Errorf(
			"models have no column %s.%s; %s has no new column in the models",
			t.Ref(),
			name,
			t.Ref(),
		)
	}

	return fmt.Errorf(
		"models have no column %s.%s; new columns of %s in the models: %s",
		t.Ref(),
		name,
		t.Ref(),
		strings.Join(fresh, ", "),
	)
}

func (s *Schema) renameTable(t *Table, name string) Change {
	change := RenameTable{Table: t.Ref(), To: name}

	for _, other := range s.Tables {
		for _, key := range other.ForeignKeys {
			if key.RefSchema == t.Schema && key.RefTable == t.Name {
				key.RefTable = name
			}
		}
	}

	t.Name = name

	return change
}

func (s *Schema) renameColumn(t *Table, c *Column, name string) {
	swap := func(columns []string) {
		for i := range columns {
			if columns[i] == c.Name {
				columns[i] = name
			}
		}
	}

	if t.PrimaryKey != nil {
		swap(t.PrimaryKey.Columns)
	}

	for _, key := range t.Uniques {
		swap(key.Columns)
	}

	for _, index := range t.Indexes {
		swap(index.Include)

		for i := range index.Parts {
			if index.Parts[i].Column == c.Name {
				index.Parts[i].Column = name
			}
		}
	}

	for _, key := range t.ForeignKeys {
		swap(key.Columns)
	}

	for _, other := range s.Tables {
		for _, key := range other.ForeignKeys {
			if key.RefSchema == t.Schema && key.RefTable == t.Name {
				swap(key.RefColumns)
			}
		}
	}

	// PostgreSQL rewrites the expressions that name the column itself, so the
	// snapshot does too, and they are not rebuilt for it.
	for _, other := range t.Columns {
		other.Generated = renamedIn(other.Generated, c.Name, name)
	}

	for _, check := range t.Checks {
		check.Expr = renamedIn(check.Expr, c.Name, name)
	}

	for _, index := range t.Indexes {
		index.Where = renamedIn(index.Where, c.Name, name)

		for i := range index.Parts {
			index.Parts[i].Expr = renamedIn(index.Parts[i].Expr, c.Name, name)
		}
	}

	c.Name = name
}

// renamedIn returns expr with each whole identifier that names column from
// naming to instead: unquoted, in any case, when from is that name in lower
// case, as unquoted names fold to it, or in double quotes, exactly. Text in single quotes is left alone, as
// are Unicode-escaped names. A name that needs quotes is written quoted. What
// this misses, the plan rebuilds, which is what it did before.
func renamedIn(expr, from, to string) string {
	if expr == "" || !mentions(expr, from) {
		return expr
	}

	quote := func(name string) string { return `"` + strings.ReplaceAll(name, `"`, `""`) + `"` }

	plain := to != "" && !strings.ContainsFunc(to, func(r rune) bool {
		return !(r == '_' || r >= 'a' && r <= 'z' || r >= '0' && r <= '9')
	}) && (to[0] < '0' || to[0] > '9')

	var b strings.Builder

	lower := strings.ToLower(expr)

	for i := 0; i < len(expr); {
		c := expr[i]

		switch {
		case c == '\'':
			end := i + 1

			for end < len(expr) && (expr[end] != '\'' || strings.HasPrefix(expr[end:], "''")) {
				if expr[end] == '\'' {
					end++
				}

				end++
			}

			end = min(end+1, len(expr))
			b.WriteString(expr[i:end])
			i = end
		case c == '"':
			end := i + 1

			for end < len(expr) && (expr[end] != '"' || strings.HasPrefix(expr[end:], `""`)) {
				if expr[end] == '"' {
					end++
				}

				end++
			}

			end = min(end+1, len(expr))
			name := strings.ReplaceAll(expr[i+1:max(end-1, i+1)], `""`, `"`)

			if name == from && (i < 2 || !strings.EqualFold(expr[i-2:i], "u&")) {
				b.WriteString(quote(to))
			} else {
				b.WriteString(expr[i:end])
			}

			i = end
		case identifierAt(lower, i) && !identifierAt(lower, i-1):
			end := i

			for end < len(expr) && identifierAt(lower, end) {
				end++
			}

			word := expr[i:end]

			// An unquoted name folds to lower case.
			switch {
			case lower[i:end] != from, strings.HasPrefix(expr[end:], "&\""):
				b.WriteString(word)
			case plain:
				b.WriteString(to)
			default:
				b.WriteString(quote(to))
			}

			i = end
		default:
			b.WriteByte(c)

			i++
		}
	}

	return b.String()
}

func (p *phases) createTable(t *Table) {
	ref := t.Ref()

	p.createTables = append(p.createTables, CreateTable{Table: t})

	if t.Comment != "" {
		p.comments = append(p.comments, SetTableComment{Table: ref, Comment: t.Comment})
	}

	for _, c := range t.Columns {
		if c.Comment != "" {
			p.comments = append(
				p.comments,
				SetColumnComment{Table: ref, Column: c.Name, Comment: c.Comment},
			)
		}
	}

	for _, index := range t.Indexes {
		p.addRest = append(p.addRest, CreateIndex{Table: ref, Index: index, New: true})
	}
}

// rebuilt returns the columns that the plan drops and adds again, by table, and
// those it gives another type. A column is rebuilt when it becomes generated or
// gets a new expression, which PostgreSQL before 17 cannot do in place, and when
// it stays generated but reads a column the plan drops or gives another type,
// which PostgreSQL will not do under it.
func rebuilt(old, to *Schema) (gone, retyped map[Ref]columns) {
	gone, retyped = map[Ref]columns{}, map[Ref]columns{}

	for _, t := range to.Tables {
		o := old.Table(t.Ref())
		if o == nil {
			continue
		}

		var changing columns

		for _, c := range o.Columns {
			next := t.Column(c.Name)

			switch {
			case next == nil:
				changing = append(changing, c.Name)
			case next.Type != c.Type:
				changing = append(changing, c.Name)
				retyped[t.Ref()] = append(retyped[t.Ref()], c.Name)
			}
		}

		for _, c := range t.Columns {
			existing := o.Column(c.Name)

			switch {
			case existing == nil:
			case (AlterColumn{From: existing, To: c}).Replaces(),
				existing.Generated != "" && c.Generated != "" && changing.mentioned(existing.Generated):
				gone[t.Ref()] = append(gone[t.Ref()], c.Name)
			}
		}
	}

	return gone, retyped
}

// columns is a set of column names.
type columns []string

// anyOf reports whether names holds one of the columns.
func (s columns) anyOf(names []string) bool {
	return slices.ContainsFunc(names, func(name string) bool { return slices.Contains(s, name) })
}

// sameSet reports whether names are the columns, in any order, which is how a
// foreign key finds the unique key it points at.
func (s columns) sameSet(names []string) bool {
	return len(s) == len(names) &&
		!slices.ContainsFunc(names, func(name string) bool { return !slices.Contains(s, name) })
}

// key returns the columns of an index a foreign key can point at: a unique one,
// on plain columns, with no predicate.
func (i *Index) key() (columns, bool) {
	if !i.Unique || i.Where != "" {
		return nil, false
	}

	out := make(columns, len(i.Parts))

	for n, part := range i.Parts {
		if part.Expr != "" {
			return nil, false
		}

		out[n] = part.Column
	}

	return out, true
}

// dropsUnique records that the plan drops a unique key on these columns.
func (p *phases) dropsUnique(table Ref, key columns) {
	if p.unique == nil {
		p.unique = map[Ref][]columns{}
	}

	p.unique[table] = append(p.unique[table], key)
}

// mentioned reports whether a SQL expression names one of the columns. It errs
// toward yes, as a string literal with the name counts too, which costs no more
// than rebuilding something that did not need it.
func (s columns) mentioned(expr string) bool {
	return slices.ContainsFunc(s, func(name string) bool { return mentions(expr, name) })
}

// usedBy reports whether an index is on one of the columns, includes one, or
// names one in an expression or its predicate.
func (s columns) usedBy(i *Index) bool {
	for _, part := range i.Parts {
		if slices.Contains(s, part.Column) || s.mentioned(part.Expr) {
			return true
		}
	}

	return s.anyOf(i.Include) || s.mentioned(i.Where)
}

// mentions reports whether expr has name as a whole identifier. Unquoted names
// fold to lower case, so case does not matter.
func mentions(expr, name string) bool {
	if name == "" {
		return false
	}

	if spelledOut(expr, name) {
		return true
	}

	expr, name = strings.ToLower(expr), strings.ToLower(name)

	// A name with a double quote in it only appears quoted, with the quote doubled.
	if strings.Contains(expr, `"`+strings.ReplaceAll(name, `"`, `""`)+`"`) {
		return true
	}

	for from := 0; ; {
		at := strings.Index(expr[from:], name)
		if at < 0 {
			return false
		}

		start := from + at
		end := start + len(name)

		if !identifierAt(expr, start-1) && !identifierAt(expr, end) {
			return true
		}

		from = start + 1
	}
}

// identifierAt reports whether s[i] can be part of an unquoted identifier. s is in
// lower case, and bytes past ASCII are letters of some other alphabet.
func identifierAt(s string, i int) bool {
	if i < 0 || i >= len(s) {
		return false
	}

	c := s[i]

	return c == '_' || c == '$' || c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 0x80
}

// spelledOut reports whether expr has name as a Unicode-escaped identifier, such
// as U&"sl\0075g" for slug, in any case. The escape character is a backslash
// unless a UESCAPE clause, which may come after comments, picks another, so every
// ASCII byte in the identifier is tried as one: a wrong one can only find the name
// where it is not.
func spelledOut(expr, name string) bool {
	for from := 0; ; {
		at := strings.Index(expr[from:], `&"`)
		if at < 0 {
			return false
		}

		start := from + at + len(`&"`)
		from = start

		if start < 3 || expr[start-3] != 'u' && expr[start-3] != 'U' {
			continue
		}

		end := start

		for end < len(expr) && (expr[end] != '"' || strings.HasPrefix(expr[end:], `""`)) {
			if expr[end] == '"' {
				end++
			}

			end++
		}

		body := strings.ReplaceAll(expr[start:end], `""`, `"`)

		for i := 0; i < len(body); i++ {
			escape := body[i]

			if escape >= 0x80 || strings.IndexByte(body[:i], escape) >= 0 {
				continue
			}

			if spelled, ok := unescape(body, escape); ok && strings.EqualFold(spelled, name) {
				return true
			}
		}
	}
}

// unescape decodes the body of a Unicode-escaped identifier. The escape and four
// hex digits, or the escape, a plus sign and six, make a code point; two escapes
// make one; and a UTF-16 surrogate pair makes the code point it encodes.
func unescape(body string, escape byte) (string, bool) {
	var out strings.Builder

	var high rune

	for i := 0; i < len(body); {
		if body[i] != escape || strings.HasPrefix(body[i+1:], string(escape)) {
			if high != 0 {
				return "", false
			}

			out.WriteByte(body[i])

			if body[i] == escape {
				i++
			}

			i++

			continue
		}

		at := i + 1
		size := 4

		if strings.HasPrefix(body[at:], "+") {
			at++
			size = 6
		}

		if at+size > len(body) {
			return "", false
		}

		code, err := strconv.ParseUint(body[at:at+size], 16, 32)
		if err != nil {
			return "", false
		}

		i = at + size
		r := rune(code)

		switch {
		case high != 0:
			r = utf16.DecodeRune(high, r)
			high = 0

			if r == utf8.RuneError {
				return "", false
			}
		case r >= 0xd800 && r < 0xdc00:
			high = r

			continue
		case !utf8.ValidRune(r):
			return "", false
		}

		out.WriteRune(r)
	}

	return out.String(), high == 0
}

// atomic keeps every index change in the plan's transaction. A plan that
// replaces a column needs one: run in parts, a failure could leave the column
// replaced with its indexes and constraints gone. An online build anywhere in it
// would take the transaction away from all of it.
func atomic(changes []Change) {
	for i, change := range changes {
		switch c := change.(type) {
		case CreateIndex:
			c.Atomic = true
			changes[i] = c
		case DropIndex:
			c.Atomic = true
			changes[i] = c
		}
	}
}

// alterTable compares two versions of a table, except for foreign keys. gone
// holds the columns the plan replaces, by table: dropping a column takes its
// indexes and constraints with it.
func (p *phases) alterTable(o, t *Table, gone map[Ref]columns) {
	ref := t.Ref()

	here := gone[ref]

	if o.Comment != t.Comment {
		p.comments = append(p.comments, SetTableComment{Table: ref, Comment: t.Comment})
	}

	for _, c := range t.Columns {
		existing := o.Column(c.Name)

		switch {
		case existing == nil && c.Generated != "":
			p.addGenerated = append(p.addGenerated, AddColumn{Table: ref, Column: c})
		case existing == nil:
			p.addColumns = append(p.addColumns, AddColumn{Table: ref, Column: c})
		case slices.Contains(here, c.Name):
			p.dropGenerated = append(
				p.dropGenerated,
				DropColumn{Table: ref, Column: existing, Again: c},
			)

			p.addGenerated = append(p.addGenerated, AddColumn{Table: ref, Column: c})
		case existing.Generated != "" && c.Generated == "":
			// It keeps its values, and stops reading the others before they change.
			plain := *existing
			plain.Generated = ""

			p.dropExpressions = append(
				p.dropExpressions,
				AlterColumn{Table: ref, From: existing, To: &plain},
			)

			if !plain.sameAs(c) {
				p.alterColumns = append(p.alterColumns, AlterColumn{
					Table:    ref,
					From:     &plain,
					To:       c,
					FromEnum: p.enum(existing.Type),
				})
			}
		case !existing.sameAs(c):
			p.alterColumns = append(p.alterColumns, AlterColumn{
				Table:    ref,
				From:     existing,
				To:       c,
				FromEnum: p.enum(existing.Type),
			})
		}

		// The comment the column has once the changes above have run.
		comment := ""

		if existing != nil && !slices.Contains(here, c.Name) {
			comment = existing.Comment
		}

		if comment != c.Comment {
			p.comments = append(
				p.comments,
				SetColumnComment{Table: ref, Column: c.Name, Comment: c.Comment},
			)
		}
	}

	for _, c := range o.Columns {
		switch {
		case t.Column(c.Name) != nil:
		case c.Generated != "":
			p.dropGenerated = append(p.dropGenerated, DropColumn{Table: ref, Column: c})
		default:
			p.dropColumns = append(p.dropColumns, DropColumn{Table: ref, Column: c})
		}
	}

	switch {
	case o.PrimaryKey != nil && t.PrimaryKey == nil:
		p.dropRest = append(p.dropRest, DropPrimaryKey{Table: ref, Key: o.PrimaryKey})
		p.dropsUnique(ref, o.PrimaryKey.Columns)
	case o.PrimaryKey == nil && t.PrimaryKey != nil:
		p.addRest = append(p.addRest, AddPrimaryKey{Table: ref, Key: t.PrimaryKey})
	case o.PrimaryKey != nil &&
		(!o.PrimaryKey.sameAs(t.PrimaryKey) || here.anyOf(o.PrimaryKey.Columns)):
		p.dropRest = append(p.dropRest, DropPrimaryKey{Table: ref, Key: o.PrimaryKey})
		p.dropsUnique(ref, o.PrimaryKey.Columns)
		p.addRest = append(p.addRest, AddPrimaryKey{Table: ref, Key: t.PrimaryKey})
	case o.PrimaryKey != nil && o.PrimaryKey.Name != t.PrimaryKey.Name:
		p.objectRenames = append(
			p.objectRenames,
			RenameConstraint{Table: ref, From: o.PrimaryKey.Name, To: t.PrimaryKey.Name},
		)
	}

	objects(
		o.Uniques,
		t.Uniques,
		func(k *Key) string { return k.Name },
		(*Key).sameAs,
		func(k *Key) bool { return here.anyOf(k.Columns) },
		func(k *Key) {
			p.dropRest = append(p.dropRest, DropConstraint{Table: ref, Name: k.Name})
			p.dropsUnique(ref, k.Columns)
		},
		func(k *Key) { p.addRest = append(p.addRest, AddUnique{Table: ref, Key: k}) },
		func(from, to string) {
			p.objectRenames = append(
				p.objectRenames,
				RenameConstraint{Table: ref, From: from, To: to},
			)
		},
	)

	objects(
		o.Checks,
		t.Checks,
		func(c *Check) string { return c.Name },
		(*Check).sameAs,
		func(c *Check) bool { return here.mentioned(c.Expr) },
		func(c *Check) {
			p.dropRest = append(p.dropRest, DropConstraint{Table: ref, Name: c.Name})
		},
		func(c *Check) { p.addRest = append(p.addRest, AddCheck{Table: ref, Check: c}) },
		func(from, to string) {
			p.objectRenames = append(
				p.objectRenames,
				RenameConstraint{Table: ref, From: from, To: to},
			)
		},
	)

	objects(
		o.Indexes,
		t.Indexes,
		func(i *Index) string { return i.Name },
		(*Index).sameAs,
		here.usedBy,
		func(i *Index) {
			if key, ok := i.key(); ok {
				p.dropsUnique(ref, key)
			}

			p.dropRest = append(p.dropRest, DropIndex{Table: ref, Index: i})
		},
		func(i *Index) { p.addRest = append(p.addRest, CreateIndex{Table: ref, Index: i}) },
		func(from, to string) {
			p.objectRenames = append(p.objectRenames, RenameIndex{Table: ref, From: from, To: to})
		},
	)
}

// foreignKeys compares the foreign keys of two versions of a table, or adds them
// to a new one, which o is nil for. A key is dropped first and made again last
// when it is on a replaced or retyped column, or points at one, or points at the
// columns of a unique key that the plan drops, since it may depend on that.
func (p *phases) foreignKeys(o, t *Table, gone, retyped map[Ref]columns) {
	ref := t.Ref()

	if o == nil {
		for _, key := range t.ForeignKeys {
			p.addKeys = append(p.addKeys, AddForeignKey{Table: ref, Key: key, New: true})
		}

		return
	}

	breaks := func(k *ForeignKey) bool {
		target := Ref{k.RefSchema, k.RefTable}

		// A type change on either side may leave the two types unable to
		// compare, and PostgreSQL then refuses it under the key.
		return gone[ref].anyOf(k.Columns) || gone[target].anyOf(k.RefColumns) ||
			retyped[ref].anyOf(k.Columns) || retyped[target].anyOf(k.RefColumns) ||
			slices.ContainsFunc(
				p.unique[target],
				func(key columns) bool { return key.sameSet(k.RefColumns) },
			)
	}

	objects(
		o.ForeignKeys,
		t.ForeignKeys,
		func(k *ForeignKey) string { return k.Name },
		(*ForeignKey).sameAs,
		breaks,
		func(k *ForeignKey) { p.dropKeys = append(p.dropKeys, DropForeignKey{Table: ref, Key: k}) },
		func(k *ForeignKey) { p.addKeys = append(p.addKeys, AddForeignKey{Table: ref, Key: k}) },
		func(from, to string) {
			p.objectRenames = append(
				p.objectRenames,
				RenameConstraint{Table: ref, From: from, To: to},
			)
		},
	)
}

// orderRenames orders index and constraint renames so that each lands on a name
// that is free: one waits for another that is leaving the name it takes, and
// when renames go round in a cycle, as when two indexes swap names, one goes
// through a spare name first. Index names, and the names of primary keys and
// unique constraints, which are indexes too, are one namespace per schema, and
// other constraint names one per table.
func orderRenames(list []Change, old, to *Schema) []Change {
	type rename struct {
		table    Ref
		from, to string
		index    bool // a RenameIndex, not a RenameConstraint
		space    string
	}

	relation := func(table Ref, name string) bool {
		t := old.Table(table)
		if t == nil {
			return false
		}

		return t.PrimaryKey != nil && t.PrimaryKey.Name == name ||
			slices.ContainsFunc(t.Uniques, func(k *Key) bool { return k.Name == name })
	}

	pending := make([]*rename, 0, len(list))

	for _, c := range list {
		switch c := c.(type) {
		case RenameIndex:
			pending = append(
				pending,
				&rename{c.Table, c.From, c.To, true, "index " + c.Table.Schema},
			)
		case RenameConstraint:
			space := "constraint " + c.Table.String()

			if relation(c.Table, c.From) {
				space = "index " + c.Table.Schema
			}

			pending = append(pending, &rename{c.Table, c.From, c.To, false, space})
		}
	}

	change := func(r *rename, to string) Change {
		if r.index {
			return RenameIndex{Table: r.table, From: r.from, To: to}
		}

		return RenameConstraint{Table: r.table, From: r.from, To: to}
	}

	leaving := func(r *rename) bool {
		return slices.ContainsFunc(pending, func(o *rename) bool {
			return o != r && o.space == r.space && o.from == r.to
		})
	}

	var out []Change

	for spare := 1; len(pending) > 0; {
		i := slices.IndexFunc(pending, func(r *rename) bool { return !leaving(r) })
		if i >= 0 {
			out = append(out, change(pending[i], pending[i].to))
			pending = slices.Delete(pending, i, i+1)

			continue
		}

		r := pending[0]

		name := fmt.Sprintf("ant_rename_%d", spare)
		for ; objectNamed(old, r.table.Schema, name) || objectNamed(to, r.table.Schema, name); spare++ {
			name = fmt.Sprintf("ant_rename_%d", spare)
		}

		spare++

		out = append(out, change(r, name))
		r.from = name
	}

	return out
}

// objectNamed reports whether s has a table, an index, or a constraint called
// name in the schema.
func objectNamed(s *Schema, schemaName, name string) bool {
	for _, t := range s.Tables {
		if t.Schema != schemaName {
			continue
		}

		switch {
		case t.Name == name,
			t.PrimaryKey != nil && t.PrimaryKey.Name == name,
			slices.ContainsFunc(t.Uniques, func(k *Key) bool { return k.Name == name }),
			slices.ContainsFunc(t.Checks, func(c *Check) bool { return c.Name == name }),
			slices.ContainsFunc(t.Indexes, func(i *Index) bool { return i.Name == name }),
			slices.ContainsFunc(t.ForeignKeys, func(k *ForeignKey) bool { return k.Name == name }):
			return true
		}
	}

	return false
}

// objects compares two lists of named objects. An object with the same definition
// under a new name is renamed, so that an index on a large table is not rebuilt
// because its name changed. An object for which uses is true is dropped and made
// again, even if nothing about it changed, because something it needs is dropped:
// a replaced column, or the unique key a foreign key points at.
func objects[T any](
	old, next []T,
	name func(T) string,
	same func(a, b T) bool,
	uses func(T) bool,
	drop, add func(T),
	rename func(from, to string),
) {
	find := func(list []T, wanted string) (T, bool) {
		for _, item := range list {
			if name(item) == wanted {
				return item, true
			}
		}

		var zero T

		return zero, false
	}

	taken := map[string]bool{}

	var added []T

	for _, item := range next {
		if existing, ok := find(old, name(item)); ok {
			taken[name(existing)] = true

			if !same(existing, item) || uses(existing) || uses(item) {
				drop(existing)
				add(item)
			}

			continue
		}

		added = append(added, item)
	}

	for _, item := range added {
		matched := false

		for _, existing := range old {
			if !taken[name(existing)] && same(existing, item) && !uses(existing) {
				taken[name(existing)] = true
				rename(name(existing), name(item))

				matched = true

				break
			}
		}

		if !matched {
			add(item)
		}
	}

	for _, existing := range old {
		if !taken[name(existing)] {
			drop(existing)
		}
	}
}

// hints finds what looks like a rename that nobody declared.
func hints(old, to *Schema) []Hint {
	var (
		out              []Hint
		dropped, created []*Table
	)

	for _, t := range old.Tables {
		if to.Table(t.Ref()) == nil {
			dropped = append(dropped, t)
		}
	}

	for _, t := range to.Tables {
		o := old.Table(t.Ref())
		if o == nil {
			created = append(created, t)

			continue
		}

		for _, gone := range o.Columns {
			if t.Column(gone.Name) != nil {
				continue
			}

			for _, added := range t.Columns {
				if o.Column(added.Name) == nil && added.Type == gone.Type {
					out = append(out, Hint{Table: t.Ref(), Column: gone.Name, To: added.Name})
				}
			}
		}
	}

	for _, gone := range dropped {
		for _, added := range created {
			switch {
			case !sameColumns(gone, added):
			case added.Schema == gone.Schema:
				out = append(out, Hint{Table: gone.Ref(), To: added.Name})
			case added.Schema != "":
				// A rename names the schema a table moves to, which the
				// connection's default cannot be.
				out = append(out, Hint{Table: gone.Ref(), To: added.Name, Schema: added.Schema})
			}
		}
	}

	return out
}

func sameColumns(a, b *Table) bool {
	return len(a.Columns) == len(b.Columns) &&
		!slices.ContainsFunc(a.Columns, func(c *Column) bool {
			other := b.Column(c.Name)

			return other == nil || other.Type != c.Type
		})
}
