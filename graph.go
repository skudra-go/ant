package ant

import (
	"context"
	"reflect"
	"unsafe"

	"github.com/skudra-go/ant/internal/meta"
)

// GraphNode is one entity TrackGraph reached.
type GraphNode struct {
	Model  string // the entity's type name
	Entity any    // a pointer to the entity
	Parent any    // a pointer to the entity it was reached from, or nil
	// HasKey reports whether the entity has a key already, which usually means the
	// row exists.
	HasKey bool
}

// TrackGraph walks a graph that came from outside, such as a request body, and
// tracks each entity in the state decide returns for it. Returning Detached skips
// the entity and everything below it. It is the way to save a tree whose earlier
// values are unknown:
//
//	ant.TrackGraph(ctx, db, order, func(n ant.GraphNode) ant.State {
//		if n.HasKey {
//			return ant.Modified
//		}
//		return ant.Added
//	})
//	err := db.SaveChanges(ctx)
//
// Foreign keys between the entities are filled in by SaveChanges. It claims the
// tracker while it walks, as Add does.
func TrackGraph[T any](
	ctx context.Context,
	h Handle,
	root *T,
	decide func(GraphNode) State,
) error {
	s, model, err := handleModel[T](h)
	if err != nil {
		return err
	}

	if root == nil {
		return ErrNilEntity
	}

	release, err := s.hold(ctx)
	if err != nil {
		return err
	}

	defer release()

	seen := map[unsafe.Pointer]bool{}
	parentType := map[unsafe.Pointer]reflect.Type{}

	var walk func(model *meta.Model, entity, parent unsafe.Pointer) error

	walk = func(model *meta.Model, entity, parent unsafe.Pointer) error {
		if seen[entity] {
			return nil
		}

		seen[entity] = true

		node := GraphNode{
			Model:  model.Type.Name(),
			Entity: reflect.NewAt(model.Type, entity).Interface(),
		}

		if parent != nil {
			node.Parent = reflect.NewAt(parentType[parent], parent).Interface()
		}

		parentType[entity] = model.Type
		node.HasKey = !isNew(model, entity)

		state := decide(node)
		if state == Detached {
			return nil
		}

		if len(model.Keys) == 0 {
			return ErrNoKey
		}

		s.tracker.Track(model, entity, state)

		for _, r := range model.Relations {
			if r.Empty(entity) || r.Pivot != nil && r.Pivot.ReadOnly {
				continue
			}

			link, err := linkFor(r)
			if err != nil {
				return err
			}

			for _, related := range r.Related(entity) {
				if err := walk(link.Target, related, entity); err != nil {
					return err
				}
			}
		}

		return nil
	}

	return walk(model, unsafe.Pointer(root), nil)
}
