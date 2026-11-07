package lifecycle

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/skudra-go/ant"
	"github.com/skudra-go/ant/tests/features/fixture"
)

// A model gets hooks by having methods with these names.
type Member struct {
	ID    int64
	Name  string
	Email string
	Log   []string `ant:"-"`
}

var errInvalid = errors.New("name is required")

func (m *Member) Validate(context.Context) error {
	if m.Name == "" {
		return errInvalid
	}

	return nil
}

func (m *Member) BeforeInsert(context.Context) error {
	m.Log = append(m.Log, "before insert")
	m.Email = strings.ToLower(m.Email)

	return nil
}

func (m *Member) AfterInsert(context.Context) error {
	m.Log = append(m.Log, "after insert")

	if m.Name == "fail after insert" {
		return errors.New("rejected")
	}

	return nil
}

func (m *Member) BeforeUpdate(context.Context) error {
	m.Log = append(m.Log, "before update")
	m.Email = strings.ToLower(m.Email)

	return nil
}

func (m *Member) AfterUpdate(context.Context) error {
	m.Log = append(m.Log, "after update")

	return nil
}

func (m *Member) BeforeDelete(context.Context) error {
	m.Log = append(m.Log, "before delete")

	if m.Name == "protected" {
		return errors.New("cannot delete")
	}

	return nil
}

func (m *Member) AfterDelete(context.Context) error {
	m.Log = append(m.Log, "after delete")

	return nil
}

func (m *Member) AfterLoad(context.Context) error {
	m.Log = append(m.Log, "loaded")

	return nil
}

type MembersDB struct {
	ant.Context
	Members ant.Table[Member]
}

var Mb = ant.Fields[Member]()

const membersDDL = `CREATE TABLE members (id BIGSERIAL PRIMARY KEY, name TEXT NOT NULL, email TEXT NOT NULL)`

func TestHooks(t *testing.T) {
	fixture.RunTrackedWith(t, membersDDL, func(t *testing.T, db *MembersDB) {
		ctx := t.Context()
		count := func() int64 {
			n, err := db.Members.Count(ctx)
			require.NoError(t, err)

			return n
		}

		t.Run("insert runs hooks, in order", func(t *testing.T) {
			m := &Member{Name: "Ann", Email: "ANN@X"}
			require.NoError(t, db.Members.Insert(ctx, m))
			require.Equal(t, []string{"before insert", "after insert"}, m.Log)
			require.Equal(t, "ann@x", m.Email, "a before hook can change what is written")
		})

		t.Run("Validate stops the write", func(t *testing.T) {
			before := count()

			require.ErrorIs(t, db.Members.Insert(ctx, &Member{}), errInvalid)
			require.Equal(t, before, count())
		})

		t.Run("an after hook's error undoes the write", func(t *testing.T) {
			before := count()
			m := &Member{Name: "fail after insert", Email: "x"}
			require.Error(t, db.Members.Insert(ctx, m))
			require.Equal(t, before, count())
			require.Zero(t, m.ID)
		})

		t.Run("update runs hooks, and a before hook's change is saved", func(t *testing.T) {
			session := ant.Session(db)
			m := &Member{Name: "Ben", Email: "ben@x"}
			require.NoError(t, session.Members.Insert(ctx, m))
			m.Log = nil
			m.Email = "BEN@Y"

			require.NoError(t, session.SaveChanges(ctx))
			require.Equal(t, []string{"before update", "after update"}, m.Log)

			got, err := db.Members.NoTracking().Find(ctx, m.ID)
			require.NoError(t, err)
			require.Equal(t, "ben@y", got.Email)
		})

		t.Run("no change runs no hook", func(t *testing.T) {
			session := ant.Session(db)
			m := &Member{Name: "Cal", Email: "c"}
			require.NoError(t, session.Members.Insert(ctx, m))
			m.Log = nil

			require.NoError(t, session.SaveChanges(ctx))
			require.Empty(t, m.Log)
		})

		t.Run("Validate runs on update", func(t *testing.T) {
			session := ant.Session(db)
			m := &Member{Name: "Dee", Email: "d"}
			require.NoError(t, session.Members.Insert(ctx, m))
			m.Name = ""

			require.ErrorIs(t, session.SaveChanges(ctx), errInvalid)
		})

		t.Run("delete runs hooks, and a before hook can refuse", func(t *testing.T) {
			session := ant.Session(db)
			m := &Member{Name: "protected", Email: "p"}
			require.NoError(t, session.Members.Insert(ctx, m))
			require.NoError(t, ant.Remove(ctx, session, m))
			require.ErrorContains(t, session.SaveChanges(ctx), "cannot delete")

			m.Name = "open"

			require.NoError(t, session.SaveChanges(ctx))
			require.Contains(t, m.Log, "after delete")
		})

		t.Run("AfterLoad runs on loaded rows", func(t *testing.T) {
			m, err := ant.Session(db).Members.First(ctx)
			require.NoError(t, err)
			require.Contains(t, m.Log, "loaded")
		})

		t.Run("WithoutHooks skips them", func(t *testing.T) {
			m := &Member{Email: "NOT LOWERED", Name: ""}
			require.NoError(t, db.Members.Insert(ant.WithoutHooks(ctx), m))
			require.Empty(t, m.Log)
		})

		t.Run("bulk writes run none", func(t *testing.T) {
			_, err := db.Members.Unfiltered().Update(ctx, ant.Set(&Mb.Name, ""))
			require.NoError(t, err)
		})
	})
}

// An observer hears about a write after it is committed.
func TestObservers(t *testing.T) {
	fixture.RunTrackedWith(t, membersDDL, func(t *testing.T, db *MembersDB) {
		ctx := t.Context()

		var inserted, updated, deleted []string

		require.NoError(t, ant.Observe(db, ant.Observer[Member]{
			Inserted: func(_ context.Context, m *Member) { inserted = append(inserted, m.Name) },
			Updated:  func(_ context.Context, m *Member) { updated = append(updated, m.Name) },
			Deleted:  func(_ context.Context, m *Member) { deleted = append(deleted, m.Name) },
		}))

		t.Run("each kind of write", func(t *testing.T) {
			m := &Member{Name: "a", Email: "a"}
			require.NoError(t, db.Members.Insert(ctx, m))
			require.Equal(t, []string{"a"}, inserted)

			m.Email = "b"

			require.NoError(t, db.SaveChanges(ctx))
			require.Equal(t, []string{"a"}, updated)

			require.NoError(t, ant.Remove(ctx, db, m))
			require.NoError(t, db.SaveChanges(ctx))
			require.Equal(t, []string{"a"}, deleted)
		})

		t.Run("a failed write is not reported", func(t *testing.T) {
			inserted = nil

			require.Error(t, db.Members.Insert(ctx, &Member{Name: "fail after insert", Email: "x"}))
			require.Empty(t, inserted)
		})

		t.Run("it waits for the outer transaction", func(t *testing.T) {
			inserted = nil
			err := db.Tx(ctx, func(tx *MembersDB) error {
				require.NoError(t, tx.Members.Insert(ctx, &Member{Name: "in tx", Email: "x"}))
				require.Empty(t, inserted, "not committed yet")

				return nil
			})
			require.NoError(t, err)
			require.Equal(t, []string{"in tx"}, inserted)
		})

		t.Run("a rollback is never reported", func(t *testing.T) {
			inserted = nil
			stop := errors.New("stop")
			err := db.Tx(ctx, func(tx *MembersDB) error {
				require.NoError(t, tx.Members.Insert(ctx, &Member{Name: "rolled", Email: "x"}))

				return stop
			})
			require.ErrorIs(t, err, stop)
			require.Empty(t, inserted)
		})

		t.Run("a rolled back savepoint is not reported either", func(t *testing.T) {
			inserted = nil
			stop := errors.New("stop")
			err := db.Tx(ctx, func(tx *MembersDB) error {
				require.NoError(t, tx.Members.Insert(ctx, &Member{Name: "before", Email: "x"}))

				require.ErrorIs(
					t,
					tx.Tx(ctx, func(inner *MembersDB) error {
						require.NoError(
							t,
							inner.Members.Insert(ctx, &Member{Name: "inner", Email: "x"}),
						)

						return stop
					}),
					stop,
				)

				return tx.Members.Insert(ctx, &Member{Name: "after", Email: "x"})
			})
			require.NoError(t, err)
			require.Equal(t, []string{"before", "after"}, inserted)
		})

		t.Run("sessions share observers", func(t *testing.T) {
			inserted = nil

			require.NoError(t, ant.Session(db).Members.Insert(ctx, &Member{Name: "s", Email: "x"}))
			require.Equal(t, []string{"s"}, inserted)
		})
	})
}

// With tracking off, an inner rollback still leaves earlier work reported.
func TestObserversWithoutTracking(t *testing.T) {
	fixture.RunWith(t, membersDDL, func(t *testing.T, db *MembersDB) {
		ctx := t.Context()

		var inserted []string

		require.NoError(t, ant.Observe(db, ant.Observer[Member]{
			Inserted: func(_ context.Context, m *Member) { inserted = append(inserted, m.Name) },
		}))

		stop := errors.New("stop")
		err := db.Tx(ctx, func(tx *MembersDB) error {
			require.NoError(t, tx.Members.Insert(ctx, &Member{Name: "kept", Email: "x"}))

			require.ErrorIs(
				t,
				tx.Tx(ctx, func(inner *MembersDB) error {
					require.NoError(
						t,
						inner.Members.Insert(ctx, &Member{Name: "undone", Email: "x"}),
					)

					return stop
				}),
				stop,
			)

			return nil
		})
		require.NoError(t, err)
		require.Equal(t, []string{"kept"}, inserted)
	})
}
