// Package liveschema puts a schema from models or a snapshot in the terms a
// dialect's Inspect uses, so the two can be compared.
package liveschema

import (
	"context"
	"slices"

	"github.com/skudra-go/ant/schema"
)

// defaulter is a dialect that can name the connection's default schema.
type defaulter interface {
	DefaultSchema(ctx context.Context, q schema.Querier) (string, error)
}

// Relative returns s with every explicit name of the connection's default
// schema written as "", since Inspect reports that schema with an empty name. A
// model that names it, as with Schema("public"), then matches its live table
// instead of showing as a new schema and a new table. s is not changed. A
// dialect that cannot name its default schema gets s back as it is.
func Relative(
	ctx context.Context,
	d schema.Dialect,
	q schema.Querier,
	s *schema.Schema,
) (*schema.Schema, error) {
	named, ok := d.(defaulter)
	if !ok {
		return s, nil
	}

	current, err := named.DefaultSchema(ctx, q)
	if err != nil {
		return nil, err
	}

	out := s.Clone()
	out.Schemas = slices.DeleteFunc(out.Schemas, func(name string) bool { return name == current })

	for _, t := range out.Tables {
		if t.Schema == current {
			t.Schema = ""
		}

		for _, key := range t.ForeignKeys {
			if key.RefSchema == current {
				key.RefSchema = ""
			}
		}
	}

	for i := range out.Unmanaged {
		if out.Unmanaged[i].Schema == current {
			out.Unmanaged[i].Schema = ""
		}
	}

	out.Sort()

	return out, nil
}
