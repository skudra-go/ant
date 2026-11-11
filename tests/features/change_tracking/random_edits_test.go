package change_tracking

import (
	"fmt"
	"maps"
	"math/rand/v2"
	"os"
	"slices"
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/skudra-go/ant"
	"github.com/skudra-go/ant/tests/features/fixture"
)

// shadow is what the database should hold, kept without using ant.
type shadow struct {
	ages  map[int64]int
	posts map[int64]shadowPost
}

type shadowPost struct {
	user      int64
	title     string
	published bool
}

// change edits a saved post. A new post has no key yet, and is recorded with its
// final values after the save.
func (want *shadow) change(id int64, f func(*shadowPost)) {
	if p, ok := want.posts[id]; ok {
		f(&p)
		want.posts[id] = p
	}
}

// compare checks the database against the shadow, reading past the tracker.
func (want *shadow) compare(t *testing.T, db *AppDB, seed uint64, step int) {
	t.Helper()
	ctx := t.Context()
	got := shadow{ages: map[int64]int{}, posts: map[int64]shadowPost{}}
	users, err := db.Users.NoTracking().All(ctx)
	require.NoError(t, err)

	for _, u := range users {
		got.ages[u.ID] = u.Age
	}

	posts, err := db.Posts.NoTracking().All(ctx)
	require.NoError(t, err)

	for _, p := range posts {
		got.posts[p.ID] = shadowPost{p.UserID, p.Title, p.Published}
	}

	require.Equal(t, want.ages, got.ages, "seed %d step %d", seed, step)
	require.Equal(t, want.posts, got.posts, "seed %d step %d", seed, step)
}

// edit applies one random change to the loaded users and to the shadow. New
// posts have no key until saved, so they are recorded after the save.
func edit(r *rand.Rand, users []*User, want *shadow, counter *int) {
	*counter++
	user := users[r.IntN(len(users))]

	other := users[r.IntN(len(users))]

	switch n := len(user.Posts); {
	case r.IntN(10) == 0:
		user.Age = r.IntN(3) // zero values included
		want.ages[user.ID] = user.Age
	case n == 0 || r.IntN(4) == 0:
		user.Posts = append(
			user.Posts,
			Post{Title: fmt.Sprintf("new %d", *counter), Published: r.IntN(2) == 0},
		)
	case r.IntN(3) == 0:
		i := r.IntN(n)
		delete(want.posts, user.Posts[i].ID)
		user.Posts = slices.Delete(user.Posts, i, i+1)
	case r.IntN(3) == 0:
		i := r.IntN(n)
		user.Posts[i].Published = !user.Posts[i].Published
		want.change(user.Posts[i].ID, func(p *shadowPost) { p.published = user.Posts[i].Published })
	case r.IntN(3) == 0:
		i := r.IntN(n)
		user.Posts[i].Title = fmt.Sprintf("edit %d", *counter)
		want.change(user.Posts[i].ID, func(p *shadowPost) { p.title = user.Posts[i].Title })
	case r.IntN(3) == 0 && other != user:
		i := r.IntN(n)
		post := user.Posts[i]
		user.Posts = slices.Delete(user.Posts, i, i+1)
		post.UserID = other.ID
		other.Posts = append(other.Posts, post)
		want.change(post.ID, func(p *shadowPost) { p.user = other.ID })
	case r.IntN(2) == 0:
		slices.Reverse(user.Posts)
	default:
		user.Posts = slices.Clip(user.Posts) // the next append moves the slice
	}
}

// Random edits to loaded relations must leave the database exactly as a plain
// map of the same edits says, after every save.
func TestRandomEdits(t *testing.T) {
	seeds := uint64(20)

	if n, err := strconv.ParseUint(os.Getenv("ANT_RANDOM_SEEDS"), 10, 64); err == nil {
		seeds = n
	}

	for seed := range seeds {
		t.Run(fmt.Sprint(seed), func(t *testing.T) {
			fixture.RunTracked(t, func(t *testing.T, db *AppDB) {
				ctx := t.Context()
				r := rand.New(rand.NewPCG(seed, 7))
				want := shadow{ages: map[int64]int{}, posts: map[int64]shadowPost{}}

				for i := range 3 {
					u := &User{Name: fmt.Sprint(i), Email: fmt.Sprint(i), Age: 1}
					require.NoError(t, db.Users.Insert(ctx, u))
					want.ages[u.ID] = 1
				}

				counter := 0
				session := ant.Session(db)
				users, err := session.Users.Include(&Users.Posts).OrderBy(&Users.ID).All(ctx)
				require.NoError(t, err)

				for step := range 60 {
					for range 1 + r.IntN(4) {
						edit(r, users, &want, &counter)
					}

					require.NoError(t, session.SaveChanges(ctx), "seed %d step %d", seed, step)

					for _, u := range users {
						for _, p := range u.Posts {
							if _, known := want.posts[p.ID]; !known {
								require.NotZero(t, p.ID)
								want.posts[p.ID] = shadowPost{u.ID, p.Title, p.Published}
							}
						}
					}

					want.compare(t, db, seed, step)
					require.False(t, fixture.HasChanges(t, session), "seed %d step %d", seed, step)

					// Some of the time, continue from a fresh load.
					if r.IntN(8) == 0 {
						session = ant.Session(db)
						users, err = session.Users.Include(&Users.Posts).OrderBy(&Users.ID).All(ctx)
						require.NoError(t, err)
					}
				}

				require.Len(t, slices.Collect(maps.Keys(want.ages)), 3)
			})
		})
	}
}
