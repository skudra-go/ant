package schema

import (
	"errors"
	"fmt"
	"slices"
	"strings"
)

// renaming is one rename asked for, found in the snapshot: a table, or a column
// of one. The table is held, not its name, so that a column rename keeps
// finding it once its table is renamed.
type renaming struct {
	text   string
	table  *Table
	column *Column // nil for a table
	to     Ref     // a table's new name, in its schema
	name   string  // a column's new name
}

// renames applies the renames to s, as the database would, and returns the
// changes. Table renames run first, so a column can be named under its table's
// old name or its new one. Within a table, or a schema, renames may swap names:
// one that lands on a name another is leaving waits for it, and a cycle goes
// through a temporary name.
func (s *Schema) renames(texts []string, to *Schema) ([]Change, error) {
	var (
		tables, columns []*renaming
		later           []string
	)

	for _, text := range texts {
		path, name, err := splitRename(text)
		if err != nil {
			return nil, err
		}

		found, err := s.find(text, path)

		switch {
		case err != nil:
			return nil, err
		case found == nil:
			later = append(later, text)
		case found.column == nil:
			found.to = s.target(found.table, name, to)
			tables = append(tables, found)
		default:
			found.name = columnName(name)
			columns = append(columns, found)
		}
	}

	out, err := s.renameTables(tables, to)
	if err != nil {
		return nil, err
	}

	for _, text := range later {
		path, name, _ := splitRename(text)

		found, err := s.find(text, path)
		if err != nil {
			return nil, err
		}

		if found == nil || found.column == nil {
			return nil, s.missing(text, path)
		}

		found.name = columnName(name)
		columns = append(columns, found)
	}

	more, err := s.renameColumns(columns, to)
	if err != nil {
		return nil, err
	}

	return append(out, more...), nil
}

// splitRename splits a rename into the path it names and the new name, at the
// first = outside double quotes.
func splitRename(text string) ([]segment, string, error) {
	bad := fmt.Errorf("ant: rename %q: want table=new_name or table.column=new_name", text)

	at := -1

	for i, quoted := 0, false; i < len(text); i++ {
		switch {
		case text[i] == '"':
			quoted = !quoted
		case text[i] == '=' && !quoted:
			at = i
		}

		if at >= 0 {
			break
		}
	}

	if at <= 0 || at == len(text)-1 {
		return nil, "", bad
	}

	path, err := segments(text[:at])
	if err != nil {
		return nil, "", bad
	}

	return path, text[at+1:], nil
}

// segment is one dot-separated part of a name, which a double-quoted part holds
// whole, dots and all.
type segment struct {
	text   string
	quoted bool
}

// segments splits a name on the dots outside double quotes. Inside quotes a
// doubled quote is one.
func segments(text string) ([]segment, error) {
	var (
		out     []segment
		current strings.Builder
		quoted  bool
		started bool
	)

	for i := 0; i < len(text); i++ {
		c := text[i]

		switch {
		case c == '"' && current.Len() == 0 && !started:
			end := i + 1

			for ; end < len(text); end++ {
				if text[end] == '"' {
					if end+1 < len(text) && text[end+1] == '"' {
						current.WriteByte('"')

						end++

						continue
					}

					break
				}

				current.WriteByte(text[end])
			}

			if end == len(text) {
				return nil, errors.New("unclosed quote")
			}

			i, quoted, started = end, true, true
		case c == '.':
			out = append(out, segment{current.String(), quoted})
			current.Reset()

			quoted, started = false, false
		case quoted:
			return nil, errors.New("text after a quoted name")
		default:
			current.WriteByte(c)

			started = true
		}
	}

	out = append(out, segment{current.String(), quoted})

	for _, part := range out {
		if part.text == "" {
			return nil, errors.New("an empty name")
		}
	}

	return out, nil
}

// groupings returns the ways to join consecutive parts into n names. Quoted
// parts stand alone, and unquoted ones may join with dots, as a name with a dot
// in it is written.
func groupings(parts []segment, n int) [][]string {
	if n == 1 {
		if len(parts) > 1 && slices.ContainsFunc(parts, func(s segment) bool { return s.quoted }) {
			return nil
		}

		texts := make([]string, len(parts))

		for i, part := range parts {
			texts[i] = part.text
		}

		return [][]string{{strings.Join(texts, ".")}}
	}

	var out [][]string

	for split := 1; split <= len(parts)-n+1; split++ {
		for _, head := range groupings(parts[:split], 1) {
			for _, rest := range groupings(parts[split:], n-1) {
				out = append(out, append(slices.Clip(head), rest...))
			}
		}
	}

	return out
}

// find looks for what a rename's path names: a table, as table or
// schema.table, or a column, as table.column or schema.table.column. A name may
// hold dots, so every way to read the path is tried, and the reading with the
// most names wins, then a column over a table, as table.column has always been
// read; two that are left are an error. It returns nil if nothing matches.
func (s *Schema) find(text string, path []segment) (*renaming, error) {
	var found []*renaming

	most := 0

	for n := min(len(path), 3); n >= 1; n-- {
		for _, names := range groupings(path, n) {
			var hits []*renaming

			switch n {
			case 1:
				if t := s.Table(Ref{Name: names[0]}); t != nil {
					hits = append(hits, &renaming{text: text, table: t})
				}
			case 2:
				if t := s.Table(Ref{Name: names[0]}); t != nil && t.Column(names[1]) != nil {
					hits = append(hits, &renaming{text: text, table: t, column: t.Column(names[1])})
				}

				if t := s.Table(Ref{names[0], names[1]}); t != nil {
					hits = append(hits, &renaming{text: text, table: t})
				}
			case 3:
				if t := s.Table(Ref{names[0], names[1]}); t != nil && t.Column(names[2]) != nil {
					hits = append(hits, &renaming{text: text, table: t, column: t.Column(names[2])})
				}
			}

			if len(hits) > 0 && most == 0 {
				most = n
			}

			if n == most {
				found = append(found, hits...)
			}
		}
	}

	if len(found) > 1 && slices.ContainsFunc(found, func(r *renaming) bool {
		return r.column != nil
	}) {
		found = slices.DeleteFunc(found, func(r *renaming) bool { return r.column == nil })
	}

	switch len(found) {
	case 0:
		return nil, nil
	case 1:
		return found[0], nil
	}

	return nil, fmt.Errorf(
		`ant: rename %q names more than one thing; quote a name that has a dot in it, as "a.b"`,
		text,
	)
}

// missing returns the error for a rename that names nothing, read the plain
// way: table, schema.table, or table.column and schema.table.column.
func (s *Schema) missing(text string, path []segment) error {
	parts := make([]string, len(path))

	for i, part := range path {
		parts[i] = part.text
	}

	switch {
	case len(parts) == 1 || len(parts) == 2 && s.Table(Ref{Name: parts[0]}) == nil:
		ref := Ref{Name: parts[len(parts)-1]}

		if len(parts) == 2 {
			ref.Schema = parts[0]
		}

		return fmt.Errorf("ant: rename %q: no table %s in the snapshot", text, ref)
	}

	ref := Ref{Name: parts[0]}

	if len(parts) >= 3 {
		ref = Ref{parts[0], parts[1]}
	}

	if s.Table(ref) == nil {
		return fmt.Errorf("ant: rename %q: no table %s in the snapshot", text, ref)
	}

	return fmt.Errorf(
		"ant: rename %q: no column %s in table %s",
		text,
		parts[len(parts)-1],
		ref,
	)
}

// columnName reads a column's new name, which may be quoted, as Hint.Flag
// quotes one that holds a dot.
func columnName(name string) string {
	if parts, err := segments(name); err == nil && len(parts) == 1 {
		return parts[0].text
	}

	return name
}

// target reads a table's new name: schema.name moves it to a schema the models
// have, and anything else is a name in its own schema, dots and all.
func (s *Schema) target(t *Table, name string, to *Schema) Ref {
	parts, err := segments(name)
	if err == nil && len(parts) == 2 && parts[0].text != t.Schema &&
		(slices.Contains(to.Schemas, parts[0].text) || slices.Contains(s.Schemas, parts[0].text)) {
		return Ref{parts[0].text, parts[1].text}
	}

	if err == nil && len(parts) == 1 {
		return Ref{t.Schema, parts[0].text}
	}

	return Ref{t.Schema, name}
}

// renameTables checks and applies table renames, and returns their changes.
func (s *Schema) renameTables(list []*renaming, to *Schema) ([]Change, error) {
	targets := map[Ref]bool{}

	for _, r := range list {
		leaving := func(ref Ref) bool {
			return slices.ContainsFunc(list, func(o *renaming) bool {
				return o != r && o.table.Ref() == ref
			})
		}

		if err := s.canRenameTable(r.table, r.to, to, targets[r.to], leaving(r.to)); err != nil {
			return nil, fmt.Errorf("ant: rename %q: %w", r.text, err)
		}

		targets[r.to] = true
	}

	var out []Change

	taken := func(ref Ref) bool { return s.Table(ref) != nil }

	for pending := slices.Clone(list); len(pending) > 0; {
		i := slices.IndexFunc(pending, func(r *renaming) bool { return !taken(r.to) })
		if i < 0 {
			// Every target is held by another table of the batch: one steps aside.
			r := pending[0]
			out = append(out, s.renameTable(r.table, s.spare(r.table.Schema, to, taken)))

			continue
		}

		r := pending[i]
		pending = slices.Delete(pending, i, i+1)

		out = append(out, s.moveTable(r.table, r.to))
	}

	return out, nil
}

// spare returns a temporary name in a schema that nothing holds or will hold.
func (s *Schema) spare(schemaName string, to *Schema, taken func(Ref) bool) string {
	for n := 1; ; n++ {
		name := fmt.Sprintf("ant_rename_%d", n)
		ref := Ref{schemaName, name}

		if !taken(ref) && to.Table(ref) == nil && !slices.Contains(s.Unmanaged, ref) {
			return name
		}
	}
}

// moveTable renames t, into another schema if ref is in one.
func (s *Schema) moveTable(t *Table, ref Ref) Change {
	if ref.Schema == t.Schema {
		return s.renameTable(t, ref.Name)
	}

	change := MoveTable{Table: t.Ref(), Schema: ref.Schema, To: ref.Name}

	for _, other := range s.Tables {
		for _, key := range other.ForeignKeys {
			if key.RefSchema == t.Schema && key.RefTable == t.Name {
				key.RefSchema, key.RefTable = ref.Schema, ref.Name
			}
		}
	}

	// The schema stays out of s.Schemas, so that the plan creates it first if
	// it is new.
	t.Schema, t.Name = ref.Schema, ref.Name

	return change
}

// renameColumns checks and applies column renames, and returns their changes.
func (s *Schema) renameColumns(list []*renaming, to *Schema) ([]Change, error) {
	type slot struct {
		table *Table
		name  string
	}

	targets := map[slot]bool{}

	for _, r := range list {
		leaving := slices.ContainsFunc(list, func(o *renaming) bool {
			return o != r && o.table == r.table && o.column.Name == r.name
		})

		err := canRenameColumn(
			r.table,
			r.column,
			r.name,
			to,
			targets[slot{r.table, r.name}],
			leaving,
		)
		if err != nil {
			return nil, fmt.Errorf("ant: rename %q: %w", r.text, err)
		}

		targets[slot{r.table, r.name}] = true
	}

	var out []Change

	for pending := slices.Clone(list); len(pending) > 0; {
		i := slices.IndexFunc(pending, func(r *renaming) bool {
			return r.table.Column(r.name) == nil
		})

		if i < 0 {
			r := pending[0]
			spare := spareColumn(r.table, to)
			from := r.column.Name
			s.renameColumn(r.table, r.column, spare)
			out = append(out, RenameColumn{Table: r.table.Ref(), From: from, To: spare})

			continue
		}

		r := pending[i]
		pending = slices.Delete(pending, i, i+1)
		from := r.column.Name
		s.renameColumn(r.table, r.column, r.name)
		out = append(out, RenameColumn{Table: r.table.Ref(), From: from, To: r.name})
	}

	return out, nil
}

// spareColumn returns a temporary column name that t does not have and will not.
func spareColumn(t *Table, to *Schema) string {
	target := to.Table(t.Ref())

	for n := 1; ; n++ {
		name := fmt.Sprintf("ant_rename_%d", n)
		if t.Column(name) == nil && (target == nil || target.Column(name) == nil) {
			return name
		}
	}
}
