package tests

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/skudra-go/ant"
	"github.com/skudra-go/ant/internal/testdb"
)

// seedChunked inserts n users, each with one post.
func seedChunked(t *testing.T, app *AppDB, n int) {
	t.Helper()

	ctx := t.Context()
	users := make([]*User, n)

	for i := range users {
		users[i] = &User{
			Name:  fmt.Sprintf("u%03d", i),
			Email: fmt.Sprintf("u%03d@example.com", i),
			Age:   i,
		}
	}

	_, err := app.Users.InsertMany(ctx, users)
	require.NoError(t, err)

	posts := make([]*Post, n)
	for i, u := range users {
		posts[i] = &Post{UserID: u.ID, Title: "p" + u.Name}
	}

	_, err = app.Posts.InsertMany(ctx, posts)
	require.NoError(t, err)
}

// trackedCount returns how many entities of T the context tracks.
func trackedCount[T any](t *testing.T, ctx context.Context, db *AppDB) int {
	t.Helper()

	tracked, err := ant.Tracked[T](ctx, db)
	require.NoError(t, err)

	return len(tracked)
}

// chunkers are the two ways to read in groups.
var chunkers = map[string]func(
	q *ant.Query[User],
	ctx context.Context,
	size int,
	f func([]*User) error,
) error{
	"Chunk": func(q *ant.Query[User], ctx context.Context, size int, f func([]*User) error) error {
		return q.OrderBy(&Users.ID).Chunk(ctx, size, f)
	},
	"ChunkByKey": func(
		q *ant.Query[User],
		ctx context.Context,
		size int,
		f func([]*User) error,
	) error {
		return q.ChunkByKey(ctx, size, f)
	},
}

// A tracking context holds one group at a time: the rows a group began
// tracking go once its callback leaves nothing to save (#151).
func TestChunkLetsGoOfEachGroup(t *testing.T) {
	testdb.ForEach(t, func(t *testing.T, target testdb.Target) {
		app := openApp(t, target)
		seedChunked(t, app, 10)

		for name, chunk := range chunkers {
			t.Run(name+" reads", func(t *testing.T) {
				ctx := t.Context()
				s := ant.Session(app)
				groups := 0

				require.NoError(t, chunk(s.Users.Query(), ctx, 3, func(users []*User) error {
					groups++

					require.Equal(t, len(users), trackedCount[User](t, ctx, s))

					return nil
				}))

				require.Equal(t, 4, groups)
				require.Zero(t, trackedCount[User](t, ctx, s))
			})

			t.Run(name+" saves in the callback", func(t *testing.T) {
				ctx := t.Context()
				s := ant.Session(app)

				require.NoError(t, chunk(s.Users.Query(), ctx, 4, func(users []*User) error {
					require.Equal(t, len(users), trackedCount[User](t, ctx, s))

					for _, u := range users {
						u.Age += 100
					}

					return s.SaveChanges(ctx)
				}))

				require.Zero(t, trackedCount[User](t, ctx, s))

				n, err := app.Users.Where(ant.Gte(&Users.Age, 100)).Count(ctx)
				require.NoError(t, err)
				require.EqualValues(t, 10, n)

				_, err = app.Users.Query().Unfiltered().Update(ctx, ant.Set(&Users.Age, 0))
				require.NoError(t, err)
			})

			t.Run(name+" includes", func(t *testing.T) {
				ctx := t.Context()
				s := ant.Session(app)

				require.NoError(
					t,
					chunk(s.Users.Include(&Users.Posts), ctx, 3, func(users []*User) error {
						require.Len(t, users[0].Posts, 1)
						require.Equal(t, len(users), trackedCount[Post](t, ctx, s))

						return nil
					}),
				)

				require.Zero(t, trackedCount[User](t, ctx, s))
				require.Zero(t, trackedCount[Post](t, ctx, s))
			})
		}
	})
}

// What a group leaves to save stays tracked, and so does what was tracked
// before the read, so letting go loses nothing (#151).
func TestChunkKeepsWhatIsStillNeeded(t *testing.T) {
	testdb.ForEach(t, func(t *testing.T, target testdb.Target) {
		app := openApp(t, target)
		seedChunked(t, app, 10)

		for name, chunk := range chunkers {
			t.Run(name+" unsaved edits", func(t *testing.T) {
				ctx := t.Context()
				s := ant.Session(app)

				require.NoError(t, chunk(s.Users.Query(), ctx, 3, func(users []*User) error {
					for _, u := range users {
						u.Bio = new("edited")
					}

					return nil
				}))

				require.Equal(t, 10, trackedCount[User](t, ctx, s))
				require.NoError(t, s.SaveChanges(ctx))

				n, err := app.Users.Where(ant.Eq(&Users.Bio, new("edited"))).Count(ctx)
				require.NoError(t, err)
				require.EqualValues(t, 10, n)

				_, err = app.Users.Query().Unfiltered().Update(ctx, ant.Set(&Users.Bio, nil))
				require.NoError(t, err)
			})

			t.Run(name+" an entity added in the callback", func(t *testing.T) {
				ctx := t.Context()
				s := ant.Session(app)
				added := false

				require.NoError(t, chunk(s.Users.Query(), ctx, 4, func(users []*User) error {
					if added {
						return nil
					}

					added = true

					return ant.Add(ctx, s, &Post{UserID: users[0].ID, Title: "added"})
				}))

				require.NoError(t, s.SaveChanges(ctx))

				n, err := app.Posts.Where(ant.Eq(&Posts.Title, "added")).Count(ctx)
				require.NoError(t, err)
				require.EqualValues(t, 1, n)

				_, err = app.Posts.Where(ant.Eq(&Posts.Title, "added")).Delete(ctx)
				require.NoError(t, err)
			})

			t.Run(name+" rows tracked before", func(t *testing.T) {
				ctx := t.Context()
				s := ant.Session(app)

				first, err := s.Users.OrderBy(&Users.ID).First(ctx)
				require.NoError(t, err)

				require.NoError(t, chunk(s.Users.Query(), ctx, 3, func([]*User) error { return nil }))

				tracked, err := ant.Tracked[User](ctx, s)
				require.NoError(t, err)
				require.Equal(t, []*User{first}, tracked)
			})

			// A parent tracked before the read, whose posts the group loads, holds
			// them in its relation, so they stay: let go, the next save would read
			// them as taken out of it.
			t.Run(name+" members of a parent tracked before", func(t *testing.T) {
				ctx := t.Context()
				s := ant.Session(app)

				first, err := s.Users.OrderBy(&Users.ID).First(ctx)
				require.NoError(t, err)

				require.NoError(
					t,
					chunk(s.Users.Include(&Users.Posts), ctx, 3, func([]*User) error { return nil }),
				)

				require.Len(t, first.Posts, 1)

				changes, err := s.HasChanges(ctx)
				require.NoError(t, err)
				require.False(t, changes)

				first.Posts = nil
				require.NoError(t, s.SaveChanges(ctx))

				n, err := app.Posts.Where(ant.Eq(&Posts.UserID, first.ID)).Count(ctx)
				require.NoError(t, err)
				require.Zero(t, n, "the post taken out of the relation was deleted")

				require.NoError(
					t,
					app.Posts.Insert(ctx, &Post{UserID: first.ID, Title: "p" + first.Name}),
				)
			})

			t.Run(name+" an error keeps the group", func(t *testing.T) {
				ctx := t.Context()
				s := ant.Session(app)
				stop := errors.New("stop")

				err := chunk(s.Users.Query(), ctx, 3, func([]*User) error { return stop })
				require.ErrorIs(t, err, stop)
				require.Equal(t, 3, trackedCount[User](t, ctx, s))
			})

			t.Run(name+" in a transaction", func(t *testing.T) {
				ctx := t.Context()

				require.NoError(t, app.Tx(ctx, func(tx *AppDB) error {
					s := ant.Session(tx)

					err := chunk(s.Users.Query(), ctx, 3, func(users []*User) error {
						for _, u := range users {
							u.Age++
						}

						return s.SaveChanges(ctx)
					})
					if err != nil {
						return err
					}

					require.Zero(t, trackedCount[User](t, ctx, s))

					return nil
				}))

				n, err := app.Users.Where(ant.Gte(&Users.Age, 1)).Count(ctx)
				require.NoError(t, err)
				require.EqualValues(t, 10, n)
			})
		}
	})
}
