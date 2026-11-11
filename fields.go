package ant

import (
	"github.com/skudra-go/ant/internal/meta"
	"github.com/skudra-go/ant/qb"
)

// Fields returns a probe for T. A probe holds no data. Its field addresses
// identify columns, so &Users.Age names the age column of User:
//
//	var Users = ant.Fields[User]()
//
// Declare one probe per model at package level. By convention it is named after
// the model in the plural, as its table is, so the names never clash. Any name
// works.
func Fields[T any]() *T {
	return meta.Probe[T]()
}

// column resolves a probe field to its column.
func column[V any](field *V) (qb.Column, *meta.Model, error) {
	f, model, err := fieldOf(field)
	if err != nil {
		return qb.Column{}, nil, err
	}

	return model.Col(f), model, nil
}

// fieldOf resolves a probe field to its metadata.
func fieldOf[V any](field *V) (*meta.Field, *meta.Model, error) {
	ref, ok := meta.Resolve(field)

	switch {
	case !ok:
		return nil, nil, ErrNotProbe
	case ref.Field == nil:
		return nil, nil, ErrNotColumn
	}

	return ref.Field, ref.Model, nil
}

// relation resolves a probe relation field.
func relation[V any](field *V) (*meta.Relation, error) {
	ref, ok := meta.Resolve(field)

	switch {
	case !ok:
		return nil, ErrNotProbe
	case ref.Relation == nil:
		return nil, ErrNotRelation
	}

	return ref.Relation, nil
}

// linkOf resolves a relation to R.
func linkOf[R any, V any](field *V) (*meta.Relation, meta.Link, error) {
	r, err := relation(field)
	if err != nil {
		return nil, meta.Link{}, err
	}

	link, err := linkFor(r)

	return r, link, err
}

// linkFor resolves a relation after configuring the models it connects.
func linkFor(r *meta.Relation) (meta.Link, error) {
	configureModel(r.Owner)
	configureModel(meta.OfType(r.Target))

	return r.Link()
}
