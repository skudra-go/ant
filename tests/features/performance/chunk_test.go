package performance

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/skudra-go/ant"
	"github.com/skudra-go/ant/tests/features/fixture"
)

// Chunk reads a large result in groups, holding one group in memory at a time.
func TestChunk(t *testing.T) {
	fixture.Run(t, func(t *testing.T, db *fixture.AppDB) {
		seed(t, db, 25)
		ctx := t.Context()
		ordered := db.Users.OrderBy(&Users.Name)

		t.Run("groups of the given size", func(t *testing.T) {
			var sizes []int

			require.NoError(t, ordered.Chunk(ctx, 10, func(users []*User) error {
				sizes = append(sizes, len(users))

				return nil
			}))

			require.Equal(t, []int{10, 10, 5}, sizes)
		})

		t.Run("an exact multiple makes no empty call", func(t *testing.T) {
			calls := 0

			require.NoError(t, ordered.Limit(1000).Chunk(ctx, 25, func(users []*User) error {
				calls++

				require.NotEmpty(t, users)

				return nil
			}))

			require.Equal(t, 1, calls)
		})

		t.Run("no rows makes no call", func(t *testing.T) {
			require.NoError(
				t,
				db.Users.Where(ant.Eq(&Users.Name, "none")).Chunk(ctx, 10, func([]*User) error {
					t.Fatal("unexpected call")

					return nil
				}),
			)
		})

		t.Run("an error stops it", func(t *testing.T) {
			stop := errors.New("stop")
			calls := 0
			err := ordered.Chunk(ctx, 10, func([]*User) error {
				calls++

				return stop
			})
			require.ErrorIs(t, err, stop)
			require.Equal(t, 1, calls)
		})

		t.Run("Include works, per group", func(t *testing.T) {
			require.NoError(
				t,
				ordered.Include(&Users.Posts).Chunk(ctx, 10, func(users []*User) error {
					require.NotNil(t, users[0].Posts)

					return nil
				}),
			)
		})

		t.Run("a bad size", func(t *testing.T) {
			require.Error(t, ordered.Chunk(ctx, 0, func([]*User) error { return nil }))
		})
	})
}

// ChunkByKey keeps its place by key, so the callback can delete what it reads,
// which would make Chunk skip rows.
func TestChunkByKey(t *testing.T) {
	fixture.Run(t, func(t *testing.T, db *fixture.AppDB) {
		seed(t, db, 25)
		ctx := t.Context()

		t.Run("every row exactly once", func(t *testing.T) {
			seen := map[string]int{}

			require.NoError(
				t,
				db.Users.Where(ant.Gte(&Users.Age, 0)).
					ChunkByKey(ctx, 7, func(users []*User) error {
						for _, u := range users {
							seen[u.Name]++
						}

						return nil
					}),
			)

			require.Len(t, seen, 25)
		})

		t.Run("the callback can delete its rows", func(t *testing.T) {
			require.NoError(
				t,
				db.Users.Where(ant.Gte(&Users.Age, 0)).
					ChunkByKey(ctx, 7, func(users []*User) error {
						for _, u := range users {
							require.NoError(t, db.Users.DeleteByID(ctx, u.ID))
						}

						return nil
					}),
			)

			n, err := db.Users.Count(ctx)
			require.NoError(t, err)
			require.Zero(t, n)
		})

		t.Run("a model with no key", func(t *testing.T) {
			type keyless struct{ Name string }

			var table ant.Table[keyless]
			require.ErrorIs(
				t,
				table.Query().ChunkByKey(ctx, 1, func([]*keyless) error { return nil }),
				ant.ErrNoKey,
			)
		})
	})
}
