package ant

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"unsafe"

	"github.com/skudra-go/ant/internal/meta"
	"github.com/skudra-go/ant/internal/track"
)

// State is the lifecycle state of an entity.
type State = track.State

// Entity states.
const (
	// Detached entities are not tracked.
	Detached = track.Detached
	// Added entities are inserted by the next SaveChanges.
	Added = track.Added
	// Unchanged entities match what was loaded.
	Unchanged = track.Unchanged
	// Modified entities are updated by the next SaveChanges.
	Modified = track.Modified
	// Deleted entities are removed by the next SaveChanges.
	Deleted = track.Deleted
)

// The helpers below read or mark the tracker, as an operation does, so each one
// claims it: another operation on the context at the same time makes it return
// ErrConcurrentUse. Called from a hook with the hook's context, a helper shares
// the operation's claim.

// Add schedules a new entity for insertion by the next SaveChanges.
func Add[T any](ctx context.Context, h Handle, entity *T) error {
	return mark(ctx, h, entity, track.Added)
}

// Attach tracks an entity as it is, assuming it matches the database.
func Attach[T any](ctx context.Context, h Handle, entity *T) error {
	return mark(ctx, h, entity, track.Unchanged)
}

// MarkModified makes the next SaveChanges write every field of an entity. Use it
// for a disconnected entity whose earlier values are unknown.
func MarkModified[T any](ctx context.Context, h Handle, entity *T) error {
	return mark(ctx, h, entity, track.Modified)
}

// Remove schedules an entity for deletion by the next SaveChanges.
func Remove[T any](ctx context.Context, h Handle, entity *T) error {
	return mark(ctx, h, entity, track.Deleted)
}

func mark[T any](ctx context.Context, h Handle, entity *T, state track.State) error {
	s, model, err := handleModel[T](h)
	if err != nil {
		return err
	}

	if entity == nil {
		return ErrNilEntity
	}

	if len(model.Keys) == 0 {
		return fmt.Errorf("%w: %s", ErrNoKey, model.Type.Name())
	}

	release, err := s.hold(ctx)
	if err != nil {
		return err
	}

	defer release()

	s.tracker.Track(model, unsafe.Pointer(entity), state)

	return nil
}

// hold claims s's tracker for a helper, as claim does for an operation, and
// returns the release.
func (s *state) hold(ctx context.Context) (func(), error) {
	_, release, err := s.claim(ctx, true)

	return release, err
}

// open returns c's state, or ErrNotOpen.
func (c *Context) open() (*state, error) {
	if c.state == nil {
		return nil, ErrNotOpen
	}

	return c.state, nil
}

func handleModel[T any](h Handle) (*state, *meta.Model, error) {
	if h == nil || h.context().state == nil {
		return nil, nil, ErrNotOpen
	}

	model := configure[T]()

	return h.context().state, model, model.Err()
}

// Detach stops tracking an entity. Unsaved changes to it are forgotten.
func Detach[T any](ctx context.Context, h Handle, entity *T) error {
	s, _, err := handleModel[T](h)
	if err != nil {
		return err
	}

	if entity == nil {
		return ErrNilEntity
	}

	release, err := s.hold(ctx)
	if err != nil {
		return err
	}

	defer release()

	s.tracker.Remove(unsafe.Pointer(entity))

	return nil
}

// Tracked returns the tracked entities of type T, oldest first.
func Tracked[T any](ctx context.Context, h Handle) ([]*T, error) {
	s, model, err := handleModel[T](h)
	if err != nil {
		return nil, err
	}

	release, err := s.hold(ctx)
	if err != nil {
		return nil, err
	}

	defer release()

	var out []*T

	for _, e := range s.tracker.Entries() {
		if e.Model == model {
			out = append(out, (*T)(e.Entity))
		}
	}

	return out, nil
}

// HasChanges reports whether SaveChanges has anything to write: an entity
// added, removed, or changed, and an entity added to, taken out of, or moved
// between the relations it loaded. It plans the save as SaveChanges would, and
// writes nothing, nor changes any entity.
func (c *Context) HasChanges(ctx context.Context) (bool, error) {
	s, err := c.open()
	if err != nil {
		return false, err
	}

	release, err := s.hold(ctx)
	if err != nil {
		return false, err
	}

	defer release()

	u := newUnit(s)
	u.dry = true

	if err := u.collect(); err != nil {
		return false, err
	}

	return u.writesAnything(), nil
}

// ClearTracked stops tracking everything, forgetting unsaved changes.
func (c *Context) ClearTracked(ctx context.Context) error {
	s, err := c.open()
	if err != nil {
		return err
	}

	release, err := s.hold(ctx)
	if err != nil {
		return err
	}

	defer release()

	s.tracker.Clear()

	return nil
}

// DebugTracked describes every tracked entity, for logs and debugging.
func (c *Context) DebugTracked(ctx context.Context) (string, error) {
	s, err := c.open()
	if err != nil {
		return "", err
	}

	release, err := s.hold(ctx)
	if err != nil {
		return "", err
	}

	defer release()

	var b strings.Builder

	for _, e := range s.tracker.Entries() {
		key, _ := e.Model.KeyOf(e.Entity)
		fmt.Fprintf(&b, "%s {%v} %s", e.Model.Type.Name(), key, entryState(e))

		for _, f := range e.Changed() {
			fmt.Fprintf(&b, " %s", f.Short)
		}

		b.WriteByte('\n')
	}

	return b.String(), nil
}

func entryState(e *track.Entry) track.State {
	if e.State == track.Unchanged && len(e.Changed()) > 0 {
		return track.Modified
	}

	return e.State
}

// EntityEntry is one entity's tracking state, as it was when Entry read it: a
// change to the entity, or a save, since does not show in it.
type EntityEntry[T any] struct {
	state    State
	changed  []*meta.Field
	original unsafe.Pointer // a private copy of the loaded values, or nil
}

// Entry returns the tracking state of an entity. An untracked entity is
// Detached.
func Entry[T any](ctx context.Context, h Handle, entity *T) (*EntityEntry[T], error) {
	s, _, err := handleModel[T](h)
	if err != nil {
		return nil, err
	}

	if entity == nil {
		return nil, ErrNilEntity
	}

	release, err := s.hold(ctx)
	if err != nil {
		return nil, err
	}

	defer release()

	out := &EntityEntry[T]{state: Detached}

	if e := s.tracker.Get(unsafe.Pointer(entity)); e != nil {
		out.state, out.changed, out.original = entryState(e), e.Changed(), e.Scratch()
	}

	return out, nil
}

// State returns the entity's state, comparing it with what was loaded.
func (e *EntityEntry[T]) State() State { return e.state }

// Changed returns the names of the fields that differ from what was loaded.
func (e *EntityEntry[T]) Changed() []string {
	var out []string

	for _, f := range e.changed {
		out = append(out, f.Short)
	}

	return out
}

// IsDirty reports whether a field differs from what was loaded.
func IsDirty[T, V any](e *EntityEntry[T], field *V) bool {
	f, _, err := fieldOf(field)
	if err != nil {
		return false
	}

	return slices.Contains(e.changed, f)
}

// Original returns a field's value as it was loaded. It returns ErrNotTracked
// for an entity with no loaded values.
func Original[T, V any](e *EntityEntry[T], field *V) (V, error) {
	var zero V

	f, _, err := fieldOf(field)
	if err != nil {
		return zero, err
	}

	if e.original == nil {
		return zero, ErrNotTracked
	}

	return f.Value(e.original).Interface().(V), nil
}
