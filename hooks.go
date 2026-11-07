package ant

import (
	"context"
	"fmt"
	"reflect"
	"sync"
	"unsafe"

	"github.com/skudra-go/ant/internal/meta"
	"github.com/skudra-go/ant/internal/track"
)

// A model implements these on its pointer type to run code around writes through
// Insert, Upsert, and SaveChanges. They run inside the write's transaction, so an
// error cancels the whole write. Bulk Update and Delete, InsertMany, and Raw do
// not run them.
type (
	// Validator checks an entity before Insert or SaveChanges writes it.
	Validator interface {
		Validate(ctx context.Context) error
	}
	// BeforeInserter runs before an entity is inserted.
	BeforeInserter interface {
		BeforeInsert(ctx context.Context) error
	}
	// AfterInserter runs after an entity is inserted.
	AfterInserter interface {
		AfterInsert(ctx context.Context) error
	}
	// BeforeUpdater runs before a changed entity is updated. Changes it makes are
	// saved too.
	BeforeUpdater interface {
		BeforeUpdate(ctx context.Context) error
	}
	// AfterUpdater runs after an entity is updated.
	AfterUpdater interface {
		AfterUpdate(ctx context.Context) error
	}
	// BeforeDeleter runs before an entity is deleted.
	BeforeDeleter interface {
		BeforeDelete(ctx context.Context) error
	}
	// AfterDeleter runs after an entity is deleted.
	AfterDeleter interface {
		AfterDelete(ctx context.Context) error
	}
	// AfterLoader runs on each entity a query returns, before Include.
	AfterLoader interface {
		AfterLoad(ctx context.Context) error
	}
	// AfterIncluder runs on each entity a query returns, after Include, so its
	// relations are there.
	AfterIncluder interface {
		AfterInclude(ctx context.Context) error
	}
)

// skipHooks is the context key of WithoutHooks.
type skipHooks struct{}

// WithoutHooks skips model hooks for writes made with ctx.
func WithoutHooks(ctx context.Context) context.Context {
	return context.WithValue(ctx, skipHooks{}, true)
}

// hookKinds records which hooks a model has, so that models with none pay nothing.
type hookKinds struct {
	validate, beforeInsert, afterInsert, beforeUpdate, afterUpdate, beforeDelete, afterDelete bool
}

var hookCache sync.Map // reflect.Type to hookKinds

func hooksOf(m *meta.Model) hookKinds {
	if h, ok := hookCache.Load(m.Type); ok {
		return h.(hookKinds)
	}

	p := reflect.PointerTo(m.Type)
	is := func(t reflect.Type) bool { return p.Implements(t) }
	h := hookKinds{
		validate:     is(reflect.TypeFor[Validator]()),
		beforeInsert: is(reflect.TypeFor[BeforeInserter]()),
		afterInsert:  is(reflect.TypeFor[AfterInserter]()),
		beforeUpdate: is(reflect.TypeFor[BeforeUpdater]()),
		afterUpdate:  is(reflect.TypeFor[AfterUpdater]()),
		beforeDelete: is(reflect.TypeFor[BeforeDeleter]()),
		afterDelete:  is(reflect.TypeFor[AfterDeleter]()),
	}
	hookCache.Store(m.Type, h)

	return h
}

func (n *node) hook(ctx context.Context, run func(any) error) error {
	if ctx.Value(skipHooks{}) != nil {
		return nil
	}

	if err := run(reflect.NewAt(n.model.Type, n.entity).Interface()); err != nil {
		return fmt.Errorf("%s: %w", n.model.Type.Name(), err)
	}

	return nil
}

// checkRules runs the model's field rules.
func (n *node) checkRules(ctx context.Context) error {
	if ctx.Value(skipHooks{}) != nil {
		return nil
	}

	for _, rule := range n.model.Rules {
		if err := rule.Check(n.entity); err != nil {
			return &ValidationError{Model: n.model.Type.Name(), Field: rule.Field.Short, Err: err}
		}
	}

	return nil
}

func (n *node) beforeInsert(ctx context.Context) error {
	if err := n.checkRules(ctx); err != nil {
		return err
	}

	h := hooksOf(n.model)
	if h.validate {
		if err := n.hook(
			ctx,
			func(e any) error { return e.(Validator).Validate(ctx) },
		); err != nil {
			return err
		}
	}

	if h.beforeInsert {
		return n.hook(ctx, func(e any) error { return e.(BeforeInserter).BeforeInsert(ctx) })
	}

	return nil
}

func (n *node) afterInsert(ctx context.Context) error {
	if hooksOf(n.model).afterInsert {
		return n.hook(ctx, func(e any) error { return e.(AfterInserter).AfterInsert(ctx) })
	}

	return nil
}

func (n *node) beforeUpdate(ctx context.Context) error {
	if err := n.checkRules(ctx); err != nil {
		return err
	}

	h := hooksOf(n.model)
	if h.validate {
		if err := n.hook(
			ctx,
			func(e any) error { return e.(Validator).Validate(ctx) },
		); err != nil {
			return err
		}
	}

	if h.beforeUpdate {
		return n.hook(ctx, func(e any) error { return e.(BeforeUpdater).BeforeUpdate(ctx) })
	}

	return nil
}

func (n *node) afterUpdate(ctx context.Context) error {
	if hooksOf(n.model).afterUpdate {
		return n.hook(ctx, func(e any) error { return e.(AfterUpdater).AfterUpdate(ctx) })
	}

	return nil
}

func (n *node) beforeDelete(ctx context.Context) error {
	if hooksOf(n.model).beforeDelete {
		return n.hook(ctx, func(e any) error { return e.(BeforeDeleter).BeforeDelete(ctx) })
	}

	return nil
}

func (n *node) afterDelete(ctx context.Context) error {
	if hooksOf(n.model).afterDelete {
		return n.hook(ctx, func(e any) error { return e.(AfterDeleter).AfterDelete(ctx) })
	}

	return nil
}

// Change describes one entity a SaveChanges is about to write.
type Change struct {
	Model   string
	State   State
	Entity  any      // a pointer to the entity
	Changed []string // field names, for an update
}

// SaveInterceptor is an Interceptor that also sees each SaveChanges before it
// writes, for auditing. An error cancels the save.
type SaveInterceptor interface {
	SavingChanges(ctx context.Context, changes []Change) error
}

// saving reports the unit's changes to interceptors.
func (u *unit) saving(ctx context.Context) error {
	var changes []Change

	built := false

	for _, in := range u.s.settings.interceptors {
		saver, ok := in.(SaveInterceptor)
		if !ok {
			continue
		}

		if !built {
			changes = u.changes()
			built = true
		}

		if len(changes) == 0 {
			continue
		}

		if err := saver.SavingChanges(ctx, changes); err != nil {
			return err
		}
	}

	return nil
}

func (u *unit) changes() []Change {
	var out []Change

	for _, n := range u.order {
		change := Change{
			Model:  n.model.Type.Name(),
			Entity: reflect.NewAt(n.model.Type, n.entity).Interface(),
		}

		switch {
		case n.insert:
			change.State = track.Added
		case n.remove:
			change.State = track.Deleted
		default:
			fields := n.pending(nil)
			if len(fields) == 0 {
				continue
			}

			change.State = track.Modified

			for _, f := range fields {
				change.Changed = append(change.Changed, f.Short)
			}
		}

		out = append(out, change)
	}

	return out
}

// observers holds a context's Observe registrations.
type observers struct {
	mu     sync.RWMutex
	byType map[reflect.Type][]observer
}

type observer struct {
	inserted, updated, deleted func(context.Context, unsafe.Pointer)
}

// Observer receives entities after their change is committed, so it never sees a
// write that rolled back. It runs once per SaveChanges or Insert entity, after the
// outermost transaction ends, and cannot affect the write.
type Observer[T any] struct {
	Inserted func(ctx context.Context, entity *T)
	Updated  func(ctx context.Context, entity *T)
	Deleted  func(ctx context.Context, entity *T)
}

// Observe registers an observer for model T, on this context and its sessions.
func Observe[T any](h Handle, o Observer[T]) error {
	s, model, err := handleModel[T](h)
	if err != nil {
		return err
	}

	wrap := func(f func(context.Context, *T)) func(context.Context, unsafe.Pointer) {
		if f == nil {
			return nil
		}

		return func(ctx context.Context, p unsafe.Pointer) { f(ctx, (*T)(p)) }
	}

	s.observed.mu.Lock()
	defer s.observed.mu.Unlock()

	if s.observed.byType == nil {
		s.observed.byType = map[reflect.Type][]observer{}
	}

	s.observed.byType[model.Type] = append(s.observed.byType[model.Type],
		observer{wrap(o.Inserted), wrap(o.Updated), wrap(o.Deleted)})

	return nil
}

// notify tells observers what the unit wrote, now or at the transaction's commit.
func (u *unit) notify(ctx context.Context) {
	u.s.observed.mu.RLock()
	defer u.s.observed.mu.RUnlock()

	if len(u.s.observed.byType) == 0 {
		return
	}

	for _, n := range u.order {
		for _, o := range u.s.observed.byType[n.model.Type] {
			var f func(context.Context, unsafe.Pointer)

			switch {
			case n.remove:
				f = o.deleted
			case n.insert:
				f = o.inserted
			case len(n.changed) > 0:
				f = o.updated
			}

			if f == nil {
				continue
			}

			entity := n.entity

			switch t := u.s.tx; {
			case t == nil:
				f(ctx, entity)
			case t.sees():
				t.pending = append(t.pending, func(ctx context.Context) { f(ctx, entity) })
			}
		}
	}
}
