package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/skudra-go/ant/schema"
)

// separator joins names in a single text column. database/sql cannot scan
// arrays, and a name cannot hold this character.
const separator = "\x01"

// tempSchema is what Canonical uses for the temporary schema's name.
const tempSchema = "pg_temp"

// tempPrefix is how a type in the temporary schema prints.
var tempPrefix = regexp.MustCompile(`^pg_temp_\d+\.`)

type liveTable struct {
	oid  int64
	ref  schema.Ref
	into *schema.Table
}

// Inspect reads the tables in schemas, plus the default schema.
func (d impl) Inspect(
	ctx context.Context,
	q schema.Querier,
	schemas []string,
) (*schema.Schema, error) {
	var current string
	if err := queryRow(ctx, q, []any{&current}, "SELECT current_schema()"); err != nil {
		return nil, wrapInspect(err, "default schema")
	}

	names := []string{current}

	for _, name := range schemas {
		if name != "" && !slices.Contains(names, name) {
			names = append(names, name)
		}
	}

	return d.read(ctx, q, current, names, false)
}

func (d impl) read(
	ctx context.Context,
	q schema.Querier,
	current string,
	names []string,
	temp bool,
) (*schema.Schema, error) {
	out := &schema.Schema{Format: schema.Format, Dialect: d.Name()}

	tables, err := d.tables(ctx, q, current, names, temp)
	if err != nil {
		return nil, wrapInspect(err, "tables")
	}

	for _, t := range tables {
		out.Tables = append(out.Tables, t.into)

		if err := d.columns(ctx, q, t); err != nil {
			return nil, wrapInspect(err, "columns of "+t.ref.String())
		}

		if err := d.constraints(ctx, q, current, t); err != nil {
			return nil, wrapInspect(err, "constraints of "+t.ref.String())
		}

		if err := d.indexes(ctx, q, t); err != nil {
			return nil, wrapInspect(err, "indexes of "+t.ref.String())
		}
	}

	if !temp {
		if err := d.types(ctx, q, out, current); err != nil {
			return nil, wrapInspect(err, "types")
		}

		rows, err := q.QueryContext(
			ctx,
			"SELECT nspname FROM pg_namespace WHERE nspname = ANY($1)",
			names,
		)
		if err != nil {
			return nil, wrapInspect(err, "schemas")
		}

		err = each(rows, func() error {
			var name string
			if err := rows.Scan(&name); err != nil {
				return err
			}

			if name != current {
				out.Schemas = append(out.Schemas, name)
			}

			return nil
		})
		if err != nil {
			return nil, wrapInspect(err, "schemas")
		}
	}

	out.Sort()

	return out, nil
}

// types reads the enums and free-standing sequences in the default schema.
// Sequences a column owns belong to serial and identity columns.
func (d impl) types(
	ctx context.Context,
	q schema.Querier,
	out *schema.Schema,
	current string,
) error {
	rows, err := q.QueryContext(
		ctx,
		`
		SELECT t.typname, array_to_string(ARRAY(
			SELECT e.enumlabel::text FROM pg_enum e WHERE e.enumtypid = t.oid ORDER BY e.enumsortorder), chr(1))
		FROM pg_type t
		JOIN pg_namespace n ON n.oid = t.typnamespace
		WHERE t.typtype = 'e' AND n.nspname = $1
		ORDER BY t.typname`,
		current,
	)
	if err != nil {
		return err
	}

	err = each(rows, func() error {
		var name, values string
		if err := rows.Scan(&name, &values); err != nil {
			return err
		}

		out.Enums = append(
			out.Enums,
			&schema.Enum{Name: name, Values: strings.Split(values, separator)},
		)

		return nil
	})
	if err != nil {
		return err
	}

	rows, err = q.QueryContext(
		ctx,
		`
		SELECT c.relname, s.seqstart, s.seqincrement
		FROM pg_class c
		JOIN pg_sequence s ON s.seqrelid = c.oid
		JOIN pg_namespace n ON n.oid = c.relnamespace
		WHERE c.relkind = 'S' AND n.nspname = $1
		  AND NOT EXISTS (SELECT 1 FROM pg_depend d WHERE d.objid = c.oid AND d.deptype IN ('a', 'i'))
		ORDER BY c.relname`,
		current,
	)
	if err != nil {
		return err
	}

	return each(rows, func() error {
		sequence := new(schema.Sequence)
		if err := rows.Scan(&sequence.Name, &sequence.Start, &sequence.Increment); err != nil {
			return err
		}

		out.Sequences = append(out.Sequences, sequence)

		return nil
	})
}

func (d impl) tables(
	ctx context.Context,
	q schema.Querier,
	current string,
	names []string,
	temp bool,
) ([]*liveTable, error) {
	rows, err := q.QueryContext(
		ctx,
		`
		SELECT c.oid::bigint, n.nspname, c.relname, COALESCE(obj_description(c.oid, 'pg_class'), ''),
		       COALESCE(p.partstrat::text, ''),
		       COALESCE(array_to_string(ARRAY(
		           SELECT a.attname::text FROM unnest(p.partattrs::int2[]) WITH ORDINALITY k(num, ord)
		           JOIN pg_attribute a ON a.attrelid = c.oid AND a.attnum = k.num ORDER BY k.ord), chr(1)), '')
		FROM pg_class c
		JOIN pg_namespace n ON n.oid = c.relnamespace
		LEFT JOIN pg_partitioned_table p ON p.partrelid = c.oid
		WHERE c.relkind IN ('r', 'p') AND NOT c.relispartition
		  AND (n.nspname = ANY($1) OR ($2 AND n.oid = pg_my_temp_schema()))
		  AND NOT EXISTS (SELECT 1 FROM pg_depend d WHERE d.objid = c.oid AND d.deptype = 'e')
		ORDER BY n.nspname, c.relname`,
		names,
		temp,
	)
	if err != nil {
		return nil, err
	}

	var out []*liveTable

	err = each(rows, func() error {
		t := new(liveTable)

		var comment, strategy, keys string
		if err := rows.Scan(
			&t.oid,
			&t.ref.Schema,
			&t.ref.Name,
			&comment,
			&strategy,
			&keys,
		); err != nil {
			return err
		}

		if t.ref.Schema == current || strings.HasPrefix(t.ref.Schema, "pg_temp_") {
			t.ref.Schema = ""
		}

		t.into = &schema.Table{Schema: t.ref.Schema, Name: t.ref.Name, Comment: comment}

		if method := map[string]string{"r": "RANGE", "l": "LIST", "h": "HASH"}[strategy]; method != "" {
			t.into.Partition = &schema.Partition{
				Method:  method,
				Columns: strings.Split(keys, separator),
			}
		}

		out = append(out, t)

		return nil
	})

	return out, err
}

func (d impl) columns(ctx context.Context, q schema.Querier, t *liveTable) error {
	rows, err := q.QueryContext(
		ctx,
		`
		SELECT a.attname,
		       format_type(a.atttypid, a.atttypmod),
		       NOT a.attnotnull,
		       COALESCE(pg_get_expr(ad.adbin, ad.adrelid), ''),
		       a.attidentity <> '',
		       a.attgenerated <> '',
		       COALESCE(CASE WHEN a.attcollation <> ty.typcollation THEN co.collname END, ''),
		       COALESCE(col_description(a.attrelid, a.attnum), '')
		FROM pg_attribute a
		JOIN pg_type ty ON ty.oid = a.atttypid
		LEFT JOIN pg_attrdef ad ON ad.adrelid = a.attrelid AND ad.adnum = a.attnum
		LEFT JOIN pg_collation co ON co.oid = a.attcollation
		WHERE a.attrelid = $1::bigint::oid AND a.attnum > 0 AND NOT a.attisdropped
		ORDER BY a.attnum`,
		t.oid,
	)
	if err != nil {
		return err
	}

	return each(rows, func() error {
		c := new(schema.Column)

		var (
			expr      string
			generated bool
		)

		if err := rows.Scan(
			&c.Name,
			&c.Type,
			&c.Nullable,
			&expr,
			&c.Identity,
			&generated,
			&c.Collation,
			&c.Comment,
		); err != nil {
			return err
		}

		c.Type = tempPrefix.ReplaceAllString(c.Type, "")

		switch {
		case generated:
			c.Generated = expr
		case strings.HasPrefix(expr, "nextval("):
			// serial and identity columns both mean "the database assigns this".
			c.Identity = true
		default:
			c.Default = expr
		}

		t.into.Columns = append(t.into.Columns, c)

		return nil
	})
}

func (d impl) constraints(
	ctx context.Context,
	q schema.Querier,
	current string,
	t *liveTable,
) error {
	columns := func(key, table string) string {
		return `array_to_string(ARRAY(
			SELECT a.attname::text
			FROM unnest(` + key + `) WITH ORDINALITY k(num, ord)
			JOIN pg_attribute a ON a.attrelid = ` + table + ` AND a.attnum = k.num
			ORDER BY k.ord), chr(1))`
	}

	rows, err := q.QueryContext(
		ctx,
		`
		SELECT con.conname,
		       con.contype::text,
		       `+columns("con.conkey", "con.conrelid")+`,
		       COALESCE(fn.nspname, ''),
		       COALESCE(fc.relname, ''),
		       `+columns("con.confkey", "con.confrelid")+`,
		       con.confdeltype::text,
		       con.confupdtype::text,
		       COALESCE(pg_get_expr(con.conbin, con.conrelid), '')
		FROM pg_constraint con
		LEFT JOIN pg_class fc ON fc.oid = con.confrelid
		LEFT JOIN pg_namespace fn ON fn.oid = fc.relnamespace
		WHERE con.conrelid = $1::bigint::oid AND con.contype IN ('p', 'u', 'f', 'c')
		ORDER BY con.conname`,
		t.oid,
	)
	if err != nil {
		return err
	}

	return each(rows, func() error {
		var name, kind, local, refSchema, refTable, foreign, onDelete, onUpdate, expr string
		if err := rows.Scan(
			&name,
			&kind,
			&local,
			&refSchema,
			&refTable,
			&foreign,
			&onDelete,
			&onUpdate,
			&expr,
		); err != nil {
			return err
		}

		switch kind {
		case "p":
			t.into.PrimaryKey = &schema.Key{Name: name, Columns: strings.Split(local, separator)}
		case "u":
			t.into.Uniques = append(
				t.into.Uniques,
				&schema.Key{Name: name, Columns: strings.Split(local, separator)},
			)
		case "c":
			t.into.Checks = append(t.into.Checks, &schema.Check{Name: name, Expr: expr})
		case "f":
			if refSchema == current {
				refSchema = ""
			}

			t.into.ForeignKeys = append(t.into.ForeignKeys, &schema.ForeignKey{
				Name:       name,
				Columns:    strings.Split(local, separator),
				RefSchema:  refSchema,
				RefTable:   refTable,
				RefColumns: strings.Split(foreign, separator),
				OnDelete:   action(onDelete),
				OnUpdate:   action(onUpdate),
			})
		}

		return nil
	})
}

func action(code string) string {
	switch code {
	case "c":
		return "CASCADE"
	case "n":
		return "SET NULL"
	case "d":
		return "SET DEFAULT"
	case "r":
		return "RESTRICT"
	}

	return ""
}

// indexes reads the indexes that do not back a constraint. Those are reported as
// constraints.
func (d impl) indexes(ctx context.Context, q schema.Querier, t *liveTable) error {
	rows, err := q.QueryContext(
		ctx,
		`
		SELECT x.indexrelid::bigint, i.relname, x.indisunique, am.amname,
		       COALESCE(pg_get_expr(x.indpred, x.indrelid), ''), x.indnkeyatts
		FROM pg_index x
		JOIN pg_class i ON i.oid = x.indexrelid
		JOIN pg_am am ON am.oid = i.relam
		WHERE x.indrelid = $1::bigint::oid AND x.indisvalid AND NOT EXISTS (
			SELECT 1 FROM pg_constraint c WHERE c.conindid = x.indexrelid AND c.conrelid = x.indrelid)
		ORDER BY i.relname`,
		t.oid,
	)
	if err != nil {
		return err
	}

	type found struct {
		oid  int64
		keys int
		into *schema.Index
	}

	var indexes []found

	err = each(rows, func() error {
		f := found{into: new(schema.Index)}
		if err := rows.Scan(
			&f.oid,
			&f.into.Name,
			&f.into.Unique,
			&f.into.Method,
			&f.into.Where,
			&f.keys,
		); err != nil {
			return err
		}

		if f.into.Method == "btree" {
			f.into.Method = ""
		}

		indexes = append(indexes, f)

		return nil
	})
	if err != nil {
		return err
	}

	for _, f := range indexes {
		rows, err := q.QueryContext(
			ctx,
			`
			SELECT ord, pg_get_indexdef(x.indexrelid, ord, true), x.indkey[ord - 1] = 0,
			       -- included columns have no order
			       COALESCE(x.indoption[ord - 1] & 1 = 1, false),
			       COALESCE(x.indoption[ord - 1] & 2 = 2, false)
			FROM pg_index x, generate_series(1, x.indnatts) ord
			WHERE x.indexrelid = $1::bigint::oid
			ORDER BY ord`,
			f.oid,
		)
		if err != nil {
			return err
		}

		err = each(rows, func() error {
			var (
				ord                     int
				text                    string
				expression, desc, first bool
			)

			if err := rows.Scan(&ord, &text, &expression, &desc, &first); err != nil {
				return err
			}

			nulls := schema.NullsOrder(desc, first)

			switch {
			case ord > f.keys:
				f.into.Include = append(f.into.Include, unquoted(text))
			case expression:
				f.into.Parts = append(
					f.into.Parts,
					schema.IndexPart{Expr: text, Desc: desc, Nulls: nulls},
				)
			default:
				f.into.Parts = append(
					f.into.Parts,
					schema.IndexPart{Column: unquoted(text), Desc: desc, Nulls: nulls},
				)
			}

			return nil
		})
		if err != nil {
			return err
		}

		t.into.Indexes = append(t.into.Indexes, f.into)
	}

	return nil
}

// unquoted reads a name as pg_get_indexdef writes it: in double quotes, with a
// quote in it doubled, if it needs them.
func unquoted(name string) string {
	if len(name) < 2 || name[0] != '"' || name[len(name)-1] != '"' {
		return name
	}

	return strings.ReplaceAll(name[1:len(name)-1], `""`, `"`)
}

// Canonical returns target as PostgreSQL itself stores it. The server rewrites
// what it is given: int becomes integer, and a check on x IN (1, 2) comes back as
// x = ANY (ARRAY[1, 2]). Matching that by hand would never be complete, so each
// schema's tables are created as temporary tables in a transaction that is rolled
// back, then read back. Nothing permanent is touched, no extra permission is
// needed, and foreign keys stay out of it because they are not rewritten and a
// temporary table cannot refer to a permanent one.
func (d impl) Canonical(
	ctx context.Context,
	conn *sql.Conn,
	target *schema.Schema,
) (*schema.Schema, error) {
	out := target.Clone()
	groups := map[string][]*schema.Table{}

	for _, t := range out.Tables {
		groups[t.Schema] = append(groups[t.Schema], t)
	}

	for _, tables := range groups {
		if err := d.canonicalGroup(ctx, conn, target, tables); err != nil {
			return nil, err
		}
	}

	return out, nil
}

func (d impl) canonicalGroup(
	ctx context.Context,
	conn *sql.Conn,
	target *schema.Schema,
	tables []*schema.Table,
) error {
	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	// Types a column uses may not exist yet, which Inspect reports by itself, so
	// temporary ones stand in. The temporary schema is searched first for tables
	// and types, but not for sequences through nextval, so those are created by name.
	for _, e := range target.Enums {
		values := make([]string, len(e.Values))

		for i, value := range e.Values {
			values[i] = literal(value)
		}

		statement := "CREATE TYPE pg_temp." + d.Quote(
			e.Name,
		) + " AS ENUM (" + strings.Join(
			values,
			", ",
		) + ")"

		if _, err := tx.ExecContext(ctx, statement); err != nil {
			return wrapInspect(err, "model enum "+e.Name)
		}
	}

	for _, q := range target.Sequences {
		if _, err := tx.ExecContext(
			ctx,
			"CREATE TEMP SEQUENCE IF NOT EXISTS "+d.Quote(q.Name),
		); err != nil {
			return wrapInspect(err, "model sequence "+q.Name)
		}
	}

	for _, t := range tables {
		temp := *t
		temp.Schema = tempSchema

		temp.Partition = nil

		if _, err := tx.ExecContext(ctx, d.createTable(&temp, false)); err != nil {
			return wrapInspect(err, "model table "+t.Ref().String())
		}

		for _, index := range t.Indexes {
			if _, err := tx.ExecContext(ctx, d.createIndex(temp.Ref(), index, false)); err != nil {
				return wrapInspect(err, "model index "+index.Name)
			}
		}
	}

	read, err := d.read(ctx, tx, "", []string{}, true)
	if err != nil {
		return err
	}

	for _, t := range tables {
		stored := read.Table(schema.Ref{Name: t.Name})
		if stored == nil {
			continue
		}

		for _, c := range stored.Columns {
			if declared := t.Column(c.Name); declared != nil {
				c.Comment = declared.Comment // comments are not created on temporary tables
			}
		}

		t.Columns = stored.Columns
		t.PrimaryKey = stored.PrimaryKey
		t.Uniques = stored.Uniques
		t.Checks = stored.Checks
		t.Indexes = stored.Indexes
	}

	return nil
}

func wrapInspect(err error, what string) error {
	if err == nil {
		return nil
	}

	return fmt.Errorf("postgres: inspect %s: %w", what, err)
}

func queryRow(ctx context.Context, q schema.Querier, into []any, query string) error {
	rows, err := q.QueryContext(ctx, query)
	if err != nil {
		return err
	}

	return each(rows, func() error { return rows.Scan(into...) })
}

func each(rows *sql.Rows, row func() error) error {
	defer rows.Close()

	for rows.Next() {
		if err := row(); err != nil {
			return err
		}
	}

	return rows.Err()
}
