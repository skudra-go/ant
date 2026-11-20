package ant

import (
	"reflect"
	"slices"
	"sync"

	"github.com/skudra-go/ant/internal/meta"
)

// Naming replaces the conventions that turn Go names into database names. A nil
// function keeps the convention. It receives the type or field name.
type Naming struct {
	Table  func(goName string) string
	Column func(goName string) string
}

// conventions holds what WithNaming and WithConvention set. Model metadata is
// global and built once, so these are too.
var conventions struct {
	sync.RWMutex
	naming Naming
	bulk   []*conventionRule
}

// conventionRule is one rule from WithConvention.
type conventionRule struct {
	apply func(*ModelConvention)
}

// WithNaming sets a naming strategy, such as PascalCase table names for an
// existing database. Names set explicitly are kept. Metadata is global, and each
// model is named once, on first use, so a process has one strategy, set in the
// first Open.
func WithNaming(naming Naming) Option {
	return func(*settings) {
		conventions.Lock()
		defer conventions.Unlock()

		conventions.naming = naming
	}
}

// WithConvention adds a rule applied to every model as it is configured, before
// its Configure method, which can override it. Metadata is global, so a process has
// one set of rules, added in the first Open. The same option given again, as to
// Open and to the migrate commands, adds no second rule.
//
//	ant.WithConvention(func(m *ant.ModelConvention) {
//		for _, f := range m.Fields() {
//			if f.Type.Kind() == reflect.String {
//				f.SetLength(255)
//			}
//		}
//	})
func WithConvention(rule func(*ModelConvention)) Option {
	r := &conventionRule{rule}

	return func(*settings) {
		conventions.Lock()
		defer conventions.Unlock()

		if !slices.Contains(conventions.bulk, r) {
			conventions.bulk = append(conventions.bulk, r)
		}
	}
}

// ModelConvention is one model, offered to a rule from WithConvention.
type ModelConvention struct {
	model *meta.Model
}

// Name returns the model's type name.
func (m *ModelConvention) Name() string { return m.model.Type.Name() }

// Table returns the model's table name.
func (m *ModelConvention) Table() string { return m.model.Table.Name }

// SetTable changes the model's table name.
func (m *ModelConvention) SetTable(name string) {
	m.model.Table.Name, m.model.Named = name, true
}

// Fields returns the model's columns.
func (m *ModelConvention) Fields() []*FieldConvention {
	out := make([]*FieldConvention, len(m.model.Fields))

	for i, f := range m.model.Fields {
		out[i] = &FieldConvention{
			Name:   f.Short,
			Column: f.Column,
			Type:   f.Type,
			field:  f,
		}
	}

	return out
}

// FieldConvention is one column, offered to a rule from WithConvention.
type FieldConvention struct {
	Name   string
	Column string
	Type   reflect.Type
	field  *meta.Field
}

// SetType sets the database type, as ColumnBuilder.Type does.
func (f *FieldConvention) SetType(sql string) { f.field.Storage.Type = sql }

// SetLength limits a string column, as ColumnBuilder.Length does.
func (f *FieldConvention) SetLength(n int) { f.field.Storage.Length = n }

// SetSensitive keeps the column out of logs, as ColumnBuilder.Sensitive does.
func (f *FieldConvention) SetSensitive() { f.field.Sensitive = true }

func applyConventions(model *meta.Model) {
	conventions.RLock()
	defer conventions.RUnlock()

	if n := conventions.naming; n.Table != nil || n.Column != nil {
		model.Rename(n.Table, n.Column)
	}

	for _, rule := range conventions.bulk {
		rule.apply(&ModelConvention{model})
	}
}
