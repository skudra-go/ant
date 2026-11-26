package postgres

import (
	"context"
	"slices"

	"github.com/skudra-go/ant/schema"
)

var _ schema.ExternalTypes = impl{}

// externalType reads the type a column's type names, or the element of an
// array of one: its name, schema, kind, and the extension that makes it. A name
// the server does not know reads no row.
const externalType = `SELECT e.typname, n.nspname, e.typtype::text, coalesce(x.extname, '')
FROM pg_type t
JOIN pg_type e ON e.oid = CASE
	WHEN t.typtype = 'b' AND t.typcategory = 'A' AND t.typelem <> 0 THEN t.typelem
	ELSE t.oid
END
JOIN pg_namespace n ON n.oid = e.typnamespace
LEFT JOIN pg_depend d ON d.classid = 'pg_type'::regclass AND d.objid = e.oid AND d.deptype = 'e'
LEFT JOIN pg_extension x ON x.oid = d.refobjid
WHERE t.oid = to_regtype($1)`

// typeKinds names pg_type.typtype.
var typeKinds = map[string]string{
	"b": "a base type",
	"c": "a composite type",
	"d": "a domain",
	"e": "an enum",
	"m": "a multirange type",
	"p": "a pseudo-type",
	"r": "a range type",
}

// ExternalTypes implements schema.ExternalTypes. It asks the server what each
// column's type is, as the connection's search path finds it, which is how a
// migration's CREATE TABLE would. A type in pg_catalog or information_schema is
// PostgreSQL's own, and an enum of s is one s creates.
func (d impl) ExternalTypes(
	ctx context.Context,
	q schema.Querier,
	s *schema.Schema,
) ([]schema.ExternalType, error) {
	var (
		names []string
		users = map[string][]string{}
	)

	for _, t := range s.Tables {
		for _, c := range t.Columns {
			if s.Enum(c.Type) != nil {
				continue
			}

			if _, seen := users[c.Type]; !seen {
				names = append(names, c.Type)
			}

			users[c.Type] = append(users[c.Type], t.Ref().String()+"."+c.Name)
		}
	}

	var (
		out  []schema.ExternalType
		keys []string // each type's schema and name, as out holds them
	)

	for _, name := range names {
		var typeName, namespace, kind, extension string

		found := false

		rows, err := q.QueryContext(ctx, externalType, name)
		if err != nil {
			return nil, wrapInspect(err, "type "+name)
		}

		err = each(rows, func() error {
			found = true

			return rows.Scan(&typeName, &namespace, &kind, &extension)
		})
		if err != nil {
			return nil, wrapInspect(err, "type "+name)
		}

		switch {
		case !found, namespace == "pg_catalog", namespace == "information_schema":
			continue
		case kind == "e" && s.Enum(typeName) != nil:
			continue // an array of an enum of s
		}

		// Two spellings of one type, such as year and year[], or year and
		// public.year, are one.
		i := slices.Index(keys, namespace+"."+typeName)

		if i < 0 {
			out = append(out, schema.ExternalType{
				Name:      typeName,
				Kind:      typeKinds[kind],
				Extension: extension,
			})

			keys = append(keys, namespace+"."+typeName)
			i = len(out) - 1
		}

		out[i].Columns = append(out[i].Columns, users[name]...)
	}

	return out, nil
}
