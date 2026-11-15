package ant

import (
	"context"
	"fmt"
	"slices"
	"unsafe"

	"github.com/skudra-go/ant/dialect"
	"github.com/skudra-go/ant/internal/exec"
	"github.com/skudra-go/ant/internal/meta"
	"github.com/skudra-go/ant/qb"
)

// pivotKeys resolves a many-to-many relation for one owner and its targets.
func pivotKeys[T, R any](
	h Handle,
	owner *T,
	rel *[]R,
	targets []*R,
) (*state, *meta.Pivot, any, []any, error) {
	s, model, err := handleModel[T](h)
	if err != nil {
		return nil, nil, nil, nil, err
	}

	if owner == nil || slices.Contains(targets, nil) {
		return nil, nil, nil, nil, ErrNilEntity
	}

	r, link, err := linkOf[R](rel)

	switch {
	case err != nil:
		return nil, nil, nil, nil, err
	case r.Owner != model:
		return nil, nil, nil, nil, ErrWrongModel
	case r.Pivot == nil:
		return nil, nil, nil, nil, fmt.Errorf("%w: %s", ErrNotManyToMany, r.Name)
	}

	local, ok := savedKey(link.Local, unsafe.Pointer(owner))
	if !ok {
		return nil, nil, nil, nil, ErrNoKey
	}

	foreign := make([]any, 0, len(targets))

	for _, target := range targets {
		key, ok := savedKey(link.Foreign, unsafe.Pointer(target))
		if !ok {
			return nil, nil, nil, nil, ErrNoKey
		}

		foreign = append(foreign, key)
	}

	return s, r.Pivot, local, foreign, nil
}

// savedKey returns a key, or false for an entity not saved yet.
func savedKey(f *meta.Field, entity unsafe.Pointer) (any, bool) {
	if f.Generated && f.Value(entity).IsZero() {
		return nil, false
	}

	return f.Key(entity)
}

// Link adds rows to a many-to-many relation's join table, skipping pairs that are
// already there. It does not change owner's relation field.
func Link[T, R any](ctx context.Context, h Handle, owner *T, rel *[]R, targets ...*R) error {
	s, pivot, local, foreign, err := pivotKeys(h, owner, rel, targets)
	if err != nil || len(foreign) == 0 {
		return err
	}

	_, err = s.run(ctx, pivotInsert(s.dialect, pivot, local, foreign))

	return err
}

// Unlink removes rows from a many-to-many relation's join table. The targets
// themselves stay.
func Unlink[T, R any](ctx context.Context, h Handle, owner *T, rel *[]R, targets ...*R) error {
	s, pivot, local, foreign, err := pivotKeys(h, owner, rel, targets)
	if err != nil || len(foreign) == 0 {
		return err
	}

	_, err = s.run(ctx, pivotDelete(pivot, local, foreign))

	return err
}

// SyncLinks makes targets the only links of owner in a many-to-many relation.
func SyncLinks[T, R any](ctx context.Context, h Handle, owner *T, rel *[]R, targets ...*R) error {
	s, pivot, local, foreign, err := pivotKeys(h, owner, rel, targets)
	if err != nil {
		return err
	}

	return s.transact(ctx, true, func(to exec.Executor) error {
		clear := pivotDelete(pivot, local, nil)
		clear.Where = clear.Where[:1]

		if len(foreign) > 0 {
			clear.Where = append(
				clear.Where,
				qb.Not{Child: pivotDelete(pivot, local, foreign).Where[1]},
			)
		}

		if _, err := s.runOn(ctx, to, clear); err != nil {
			return err
		}

		if len(foreign) == 0 {
			return nil
		}

		_, err := s.runOn(ctx, to, pivotInsert(s.dialect, pivot, local, foreign))

		return err
	})
}

// Toggle links the targets that are not linked to owner, and unlinks those that
// are.
func Toggle[T, R any](ctx context.Context, h Handle, owner *T, rel *[]R, targets ...*R) error {
	s, pivot, local, foreign, err := pivotKeys(h, owner, rel, targets)
	if err != nil || len(foreign) == 0 {
		return err
	}

	return s.transact(ctx, true, func(to exec.Executor) error {
		removed, err := removeLinked(ctx, s, to, pivot, local, foreign)
		if err != nil {
			return err
		}

		gone := make(map[any]struct{}, len(removed))

		for _, key := range removed {
			if k, ok := meta.KeyValue(key); ok {
				gone[k] = struct{}{}
			}
		}

		added := slices.DeleteFunc(foreign, func(key any) bool {
			_, was := gone[key]

			return was
		})

		if len(added) == 0 {
			return nil
		}

		_, err = s.runOn(ctx, to, pivotInsert(s.dialect, pivot, local, added))

		return err
	})
}

// removeLinked deletes the join rows of local to foreign that exist, and returns
// the foreign keys they held. A row is removed in the same statement that finds
// it, so two callers cannot both see it. With no DELETE ... RETURNING, as on
// MySQL, the rows are read with a lock first, which a second caller waits on,
// and then deleted.
func removeLinked(
	ctx context.Context,
	s *state,
	to exec.Executor,
	pivot *meta.Pivot,
	local any,
	foreign []any,
) ([]any, error) {
	removal := pivotDelete(pivot, local, foreign)
	column := []qb.Selected{{Value: qb.Column{Name: pivot.Foreign}}}

	if dialect.Supports(s.dialect, dialect.ReturningDelete) {
		removal.Returning = column

		rows, err := s.queryOn(ctx, to, removal)
		if err != nil {
			return nil, err
		}

		return collect[any](rows)
	}

	read := &qb.Select{
		Columns: column,
		From:    qb.Source{Table: pivot.Table},
		Where:   removal.Where,
	}

	if !dialect.SyntaxOf(s.dialect).Lacks(dialect.RowLocks) {
		read.Lock = &qb.Lock{Mode: "UPDATE"}
	}

	rows, err := s.queryOn(ctx, to, read)
	if err != nil {
		return nil, err
	}

	found, err := collect[any](rows)
	if err != nil || len(found) == 0 {
		return found, err
	}

	if _, err := s.runOn(ctx, to, pivotDelete(pivot, local, found)); err != nil {
		return nil, err
	}

	return found, nil
}
