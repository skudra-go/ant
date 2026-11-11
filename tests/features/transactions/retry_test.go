package transactions

import (
	"database/sql"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/skudra-go/ant"
	"github.com/skudra-go/ant/tests/features/fixture"
)

// Attempts reruns a transaction that lost a serialization conflict or deadlock.
func TestRetry(t *testing.T) {
	fixture.RunTracked(t, func(t *testing.T, db *AppDB) {
		ctx := t.Context()

		t.Run("conflicting transactions all finish", func(t *testing.T) {
			counter := user("counter")
			require.NoError(t, db.Users.Insert(ctx, counter))

			const workers = 8

			var (
				wg   sync.WaitGroup
				runs atomic.Int32
			)

			errs := make(chan error, workers)

			for range workers {
				wg.Go(func() {
					errs <- ant.Session(db).Tx(
						ctx,
						func(tx *AppDB) error {
							runs.Add(1)

							u, err := tx.Users.NoTracking().Find(ctx, counter.ID)
							if err != nil {
								return err
							}

							_, err = tx.Users.Where(ant.Eq(&Users.ID, counter.ID)).
								Update(ctx, ant.Set(&Users.Age, u.Age+1))

							return err
						},
						ant.Isolation(sql.LevelSerializable),
						ant.Attempts(50),
					)
				})
			}

			wg.Wait()
			close(errs)

			for err := range errs {
				require.NoError(t, err)
			}

			got, err := db.Users.NoTracking().Find(ctx, counter.ID)
			require.NoError(t, err)
			require.Equal(t, workers, got.Age, "no update was lost")
			require.GreaterOrEqual(t, int(runs.Load()), workers)
		})

		t.Run("without Attempts a conflict is returned", func(t *testing.T) {
			require.True(t, ant.IsRetryable(serializationFailure(t, db)))
		})

		// A retried attempt starts from what was tracked before the transaction,
		// so a tracked read gets the row afresh rather than the instance the
		// failed attempt changed.
		t.Run("a retry reads rows afresh", func(t *testing.T) {
			row := user("retried")
			row.Age = 20
			require.NoError(t, db.Users.Insert(ctx, row))

			session := ant.Session(db)
			runs := 0
			err := session.Tx(
				ctx,
				func(tx *AppDB) error {
					runs++

					u, err := tx.Users.Find(ctx, row.ID)
					if err != nil {
						return err
					}

					u.Age += 10

					if err := tx.SaveChanges(ctx); err != nil {
						return err
					}

					if runs == 1 {
						return conflict{}
					}

					return nil
				},
				ant.Attempts(3),
			)
			require.NoError(t, err)
			require.Equal(t, 2, runs)

			got, err := db.Users.NoTracking().Find(ctx, row.ID)
			require.NoError(t, err)
			require.Equal(t, 30, got.Age, "the second attempt's edit, once")
			require.False(t, fixture.HasChanges(t, session))
		})

		t.Run("a retry inserts an added entity once", func(t *testing.T) {
			session := ant.Session(db)
			runs := 0
			err := session.Tx(
				ctx,
				func(tx *AppDB) error {
					runs++

					if err := ant.Add(ctx, tx, user("added-once")); err != nil {
						return err
					}

					if err := tx.SaveChanges(ctx); err != nil {
						return err
					}

					if runs == 1 {
						return conflict{}
					}

					return nil
				},
				ant.Attempts(3),
			)
			require.NoError(t, err)

			count, err := db.Users.Where(ant.Eq(&Users.Email, "added-once@example.com")).Count(ctx)
			require.NoError(t, err)
			require.EqualValues(t, 1, count)
			require.False(t, fixture.HasChanges(t, session))
		})

		// What was tracked before the transaction stays tracked, with its own
		// pending change, through a retry.
		t.Run("a retry keeps what was tracked before", func(t *testing.T) {
			row := user("tracked-before")
			require.NoError(t, db.Users.Insert(ctx, row))

			session := ant.Session(db)
			before, err := session.Users.Find(ctx, row.ID)
			require.NoError(t, err)

			before.Name = "renamed before"
			runs := 0
			err = session.Tx(
				ctx,
				func(tx *AppDB) error {
					runs++

					if runs == 1 {
						return conflict{}
					}

					return nil
				},
				ant.Attempts(3),
			)
			require.NoError(t, err)
			require.Equal(t, ant.Modified, fixture.Entry(t, session, before).State())

			again, err := session.Users.Find(ctx, row.ID)
			require.NoError(t, err)
			require.Same(t, before, again)
		})

		t.Run("an ordinary error is not retried", func(t *testing.T) {
			runs := 0
			err := db.Tx(
				ctx,
				func(tx *AppDB) error {
					runs++

					return errStop
				},
				ant.Attempts(5),
			)
			require.ErrorIs(t, err, errStop)
			require.Equal(t, 1, runs)
		})
	})
}

// conflict is an error IsRetryable takes for a serialization failure.
type conflict struct{}

func (conflict) Error() string    { return "serialization failure" }
func (conflict) SQLState() string { return "40001" }

// serializationFailure forces one conflict between two transactions.
func serializationFailure(t *testing.T, db *AppDB) error {
	t.Helper()
	ctx := t.Context()
	row := user("conflict")
	require.NoError(t, db.Users.Insert(ctx, row))
	one, err := ant.Begin(ctx, ant.Session(db), ant.Isolation(sql.LevelSerializable))
	require.NoError(t, err)

	defer one.Rollback()

	two, err := ant.Begin(ctx, ant.Session(db), ant.Isolation(sql.LevelSerializable))
	require.NoError(t, err)

	defer two.Rollback()

	_, err = one.Users.NoTracking().Find(ctx, row.ID)
	require.NoError(t, err)
	_, err = two.Users.NoTracking().Find(ctx, row.ID)
	require.NoError(t, err)
	_, err = one.Users.Where(ant.Eq(&Users.ID, row.ID)).Update(ctx, ant.Set(&Users.Age, 1))
	require.NoError(t, err)
	require.NoError(t, one.Commit())

	_, err = two.Users.Where(ant.Eq(&Users.ID, row.ID)).Update(ctx, ant.Set(&Users.Age, 2))

	return err
}
