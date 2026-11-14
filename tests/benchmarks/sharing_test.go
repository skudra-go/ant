package benchmarks

import (
	"context"
	"database/sql"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/skudra-go/ant"
	"github.com/skudra-go/ant/dialect/postgres"
)

// The tests below hold the behaviour that the work on allocations must not
// change: queries, rows and statements that share memory behind the scenes are
// each still their own to whoever holds them.

type Author struct {
	ant.Base
	Name  string
	Books []Book
}

type Book struct {
	ant.Base
	AuthorID int64
	Title    string
	Author   *Author
}

type Library struct {
	ant.Context
	Authors ant.Table[Author]
	Books   ant.Table[Book]
}

var (
	Authors = ant.Fields[Author]()
	Books   = ant.Fields[Book]()
)

func library(t testing.TB) (*Library, *fake) {
	t.Helper()

	f := &fake{}
	pool := sql.OpenDB(f)

	t.Cleanup(func() { pool.Close() })

	db, err := ant.Wrap[Library](postgres.Dialect, pool, ant.WithTracking(false))
	require.NoError(t, err)

	return db, f
}

func sqlOf[T any](t *testing.T, q *ant.Query[T]) string {
	t.Helper()

	text, _, err := q.ToSQL()
	require.NoError(t, err)

	return text
}

const (
	bookColumns = `"books"."id", "books"."created_at", "books"."updated_at", "books"."author_id", "books"."title"`
	allBooks    = `SELECT ` + bookColumns + ` FROM "books"`
)

// A query is the base of any number of others, in any order, and none of them
// changes it or each other, although each step shares the lists it only adds
// to with the query it came from.
func TestQueriesMadeFromOneBaseStayApart(t *testing.T) {
	db, _ := library(t)

	// Three steps leave the list of conditions with room to spare, which a
	// careless append in two queries made from it would both write into.
	base := db.Books.Where(ant.Gt(&Books.ID, 1)).
		Where(ant.Gt(&Books.ID, 2)).
		Where(ant.Gt(&Books.ID, 3))

	baseSQL := allBooks + ` WHERE "books"."id" > $1 AND "books"."id" > $2 AND "books"."id" > $3`

	require.Equal(t, baseSQL, sqlOf(t, base))

	titled := base.Where(ant.Eq(&Books.Title, "a"))
	authored := base.Where(ant.Eq(&Books.AuthorID, 7))
	ordered := base.OrderBy(&Books.Title)
	thenBy := ordered.ThenByDesc(&Books.ID)
	reordered := ordered.OrderByDesc(&Books.AuthorID)
	limited := base.Limit(5).Offset(2)
	chosen := base.Column(&Books.Title)
	both := chosen.Column(&Books.ID)
	omitted := base.Omit(&Books.Title)
	omittedTwo := omitted.Omit(&Books.AuthorID)
	joined := base.IncludeJoined(&Books.Author)
	tagged := base.Tag("t").Distinct()

	// The base and every query made from it, checked after all were built.
	for name, want := range map[*ant.Query[Book]]string{
		base:     baseSQL,
		titled:   baseSQL + ` AND "books"."title" = $4`,
		authored: baseSQL + ` AND "books"."author_id" = $4`,
		ordered:  baseSQL + ` ORDER BY "books"."title"`,
		thenBy:   baseSQL + ` ORDER BY "books"."title", "books"."id" DESC`,
		reordered: baseSQL +
			` ORDER BY "books"."author_id" DESC`,
		limited: baseSQL + ` LIMIT $4 OFFSET $5`,
		tagged:  `/* t */ SELECT DISTINCT ` + bookColumns + ` FROM "books"` + baseSQL[len(allBooks):],
	} {
		require.Equal(t, want, sqlOf(t, name))
	}

	require.Contains(t, sqlOf(t, chosen), `SELECT "books"."title" FROM`)
	require.Contains(t, sqlOf(t, both), `SELECT "books"."title", "books"."id" FROM`)
	require.NotContains(t, sqlOf(t, omitted), `"title"`)
	require.Contains(t, sqlOf(t, omitted), `"books"."author_id"`)
	require.NotContains(t, sqlOf(t, omittedTwo), `"author_id"`)

	// The joined query adds the author's columns to its own list, not to the
	// one every query of the model shares.
	require.Contains(t, sqlOf(t, joined), `"ant_joined0"."name"`)
	require.Equal(t, allBooks, sqlOf(t, db.Books.Query()))
	require.Equal(t, baseSQL, sqlOf(t, base))
}

// A join that makes the tables before it nullable does so in its own query,
// not in the one it was made from.
func TestJoinsLeaveTheirBaseAlone(t *testing.T) {
	db, f := library(t)
	ctx := context.Background()

	base := db.Books.Where(ant.Gt(&Books.ID, 1))
	before := sqlOf(t, base)

	type pair struct {
		Book   *Book
		Author *Author
	}

	both := func(b *Book, a *Author) pair { return pair{b, a} }

	// The fake has no rows, so each of these only shows the SQL it sent.
	ran := func(run func() error) string {
		calls := f.recorded(func() { require.NoError(t, run()) })
		require.Len(t, calls, 1)

		return calls[0].SQL
	}

	right := base.RightJoinOn[Author](ant.EqCol(&Books.AuthorID, &Authors.ID))
	using := base.JoinUsing[Author, int64](&Books.ID)
	inner := base.Join(&Books.Author)

	require.Contains(
		t,
		ran(func() error {
			_, err := right.Select(both).All(ctx)

			return err
		}),
		`RIGHT JOIN "authors" ON "books"."author_id" = "authors"."id"`,
	)

	require.Contains(
		t,
		ran(func() error {
			_, err := using.Select(both).All(ctx)

			return err
		}),
		`JOIN "authors" ON "books"."id" = "authors"."id"`,
	)

	innerSQL := ran(func() error {
		_, err := inner.Select(both).All(ctx)

		return err
	})
	require.Contains(t, innerSQL, `JOIN "authors" ON "books"."author_id" = "authors"."id"`)
	require.NotContains(t, innerSQL, "RIGHT")

	// A join after the right join, made from the same base, is its own too.
	require.Contains(
		t,
		ran(func() error {
			_, err := right.Where(ant.Eq(&Authors.Name, "a")).Select(both).All(ctx)

			return err
		}),
		`"authors"."name" = $2`,
	)

	require.Equal(t, before, sqlOf(t, base))

	require.Equal(t, before, ran(func() error {
		_, err := base.All(ctx)

		return err
	}))
}

// Each parent's related rows are a slice of its own, exactly as long as its
// rows, so that appending to one never writes into another's, though they are
// cut from one block. A tracked one moves on its first append, and the tracker
// follows its elements there.
func TestIncludedSlicesAreEachParentsOwn(t *testing.T) {
	for _, tracking := range []bool{false, true} {
		db, _, f := fakeDB(t, ant.WithTracking(tracking))
		ctx := context.Background()

		const fan = 3

		// More posts than one block holds, and more parents than one.
		f.rows.Store(1000)
		f.fan.Store(fan)

		users, err := db.Users.Include(&Users.Posts).All(ctx)
		require.NoError(t, err)
		require.Len(t, users, 1000)

		for i, u := range users {
			require.Len(t, u.Posts, fan)
			require.Equal(t, fan, cap(u.Posts), "user %d has room for rows that are not its own", i)

			for j, p := range u.Posts {
				require.Equal(t, u.ID, p.UserID)
				require.Equal(t, int64(fakeFirstID+i*fan+j), p.ID)
			}
		}

		users[0].Posts = append(users[0].Posts, Post{Title: "new"})
		users[0].Posts[0].Title = "changed"

		require.Equal(t, "some text", users[1].Posts[0].Title)
		require.Equal(t, int64(fakeFirstID+fan), users[1].Posts[0].ID)

		// No related rows is an empty slice, not a missing one.
		f.fan.Store(1)
		f.rows.Store(0)

		none, err := db.Posts.All(ctx)
		require.NoError(t, err)
		require.Empty(t, none)
		require.NotNil(t, none)
	}
}

// Rows that are streamed, and values that are collected, are each their own,
// though one value is scanned into for all of them.
func TestStreamedRowsAreCopies(t *testing.T) {
	db, _, f := fakeDB(t, ant.WithTracking(false))
	ctx := context.Background()

	f.rows.Store(50)

	var seen []User

	for u, err := range db.Users.Each(ctx) {
		require.NoError(t, err)

		seen = append(seen, u)
	}

	require.Len(t, seen, 50)

	for i, u := range seen {
		require.Equal(t, int64(fakeFirstID+i), u.ID)
		require.Equal(t, fakeTime, u.CreatedAt)
		require.Nil(t, u.Bio)
	}

	ids, err := ant.Pluck(ctx, db.Users.Query(), &Users.ID)
	require.NoError(t, err)
	require.Len(t, ids, 50)

	for i, id := range ids {
		require.Equal(t, int64(fakeFirstID+i), id)
	}
}

// First and Find read one row into a block of one, and All every row, in
// order, across blocks.
func TestRowsAcrossBlocks(t *testing.T) {
	db, _, f := fakeDB(t, ant.WithTracking(false))
	ctx := context.Background()

	for _, size := range []int{0, 1, 7, 8, 9, 1023, 1024, 1025, 5000} {
		f.rows.Store(int64(size))

		users, err := db.Users.All(ctx)
		require.NoError(t, err)
		require.Len(t, users, size)
		require.NotNil(t, users)

		for i, u := range users {
			require.Equal(t, int64(fakeFirstID+i), u.ID)
		}

		limited, err := db.Users.Limit(3).All(ctx)
		require.NoError(t, err)
		require.Len(t, limited, size) // the fake does not read LIMIT: every row it sends is kept
	}

	f.rows.Store(1)

	one, err := db.Users.Find(ctx, int64(fakeFirstID))
	require.NoError(t, err)
	require.Equal(t, int64(fakeFirstID), one.ID)

	f.rows.Store(0)

	_, err = db.Users.First(ctx)
	require.ErrorIs(t, err, ant.ErrNotFound)
}

// InsertMany binds every value of every row, in order, and gives each row its
// key, though the rows are built from two blocks and not a slice each.
func TestInsertManyBindsEveryRow(t *testing.T) {
	now := time.Date(2026, 3, 4, 5, 6, 7, 0, time.FixedZone("east", 3600))
	db, _, f := fakeDB(t, ant.WithTracking(false), ant.WithNow(func() time.Time { return now }))
	ctx := context.Background()

	bio := "b"
	users := []*User{
		{Name: "a", Email: "a@x", Age: 1},
		{
			Name:  "b",
			Email: "b@x",
			Age:   2,
			Bio:   &bio,
		},
		{Name: "c", Email: "c@x", Age: 300},
	}

	calls := f.recorded(func() {
		n, err := db.Users.InsertMany(ctx, users)
		require.NoError(t, err)
		require.Equal(t, int64(3), n)
	})

	require.Len(t, calls, 1)

	require.Equal(
		t,
		`INSERT INTO "users" ("created_at", "updated_at", "name", "email", "age", "bio") VALUES `+
			`($1, $2, $3, $4, $5, $6), ($7, $8, $9, $10, $11, $12), ($13, $14, $15, $16, $17, $18) RETURNING "id"`,
		calls[0].SQL,
	)

	at := now.UTC()

	require.Equal(
		t,
		[]any{
			at,
			at,
			"a",
			"a@x",
			int64(1),
			nil,
			at,
			at,
			"b",
			"b@x",
			int64(2),
			"b",
			at,
			at,
			"c",
			"c@x",
			int64(300),
			nil,
		},
		calls[0].Args,
	)

	for i, u := range users {
		require.Equal(t, int64(fakeInsertedID+i), u.ID)
		require.Equal(t, at, u.CreatedAt)
	}

	// One row goes the same way.
	one := &User{Name: "d", Email: "d@x", Age: 4}

	calls = f.recorded(func() { require.NoError(t, db.Users.Insert(ctx, one)) })

	require.Len(t, calls, 1)

	require.Equal(
		t,
		`INSERT INTO "users" ("created_at", "updated_at", "name", "email", "age", "bio") VALUES `+
			`($1, $2, $3, $4, $5, $6) RETURNING "id"`,
		calls[0].SQL,
	)

	require.Equal(t, []any{at, at, "d", "d@x", int64(4), nil}, calls[0].Args)
	require.Equal(t, int64(fakeInsertedID), one.ID)
}

// A save writes the rows that changed among many that did not, and nothing when
// nothing changed, though it no longer builds anything for the rows it leaves.
func TestSaveChangesWritesOnlyWhatChanged(t *testing.T) {
	now := time.Date(2026, 3, 4, 5, 6, 7, 0, time.UTC)
	db, _, f := fakeDB(t, ant.WithNow(func() time.Time { return now }))
	ctx := context.Background()

	f.rows.Store(3000)

	session := ant.Session(db)

	users, err := session.Users.All(ctx)
	require.NoError(t, err)
	require.Len(t, users, 3000)

	// The same rows again are the same instances.
	again, err := session.Users.All(ctx)
	require.NoError(t, err)
	require.Same(t, users[0], again[0])
	require.Same(t, users[2999], again[2999])

	require.Empty(t, f.recorded(func() { require.NoError(t, session.SaveChanges(ctx)) }))

	users[2999].Name = "last"
	users[7].Age = 77
	users[7].Email = "seven"

	calls := f.recorded(func() { require.NoError(t, session.SaveChanges(ctx)) })

	// Oldest first, as they were loaded.
	require.Equal(
		t,
		[]fakeCall{
			{
				`UPDATE "users" SET "email" = $1, "age" = $2, "updated_at" = $3 WHERE "users"."id" = $4`,
				[]any{"seven", int64(77), now, int64(fakeFirstID + 7)},
			},
			{
				`UPDATE "users" SET "name" = $1, "updated_at" = $2 WHERE "users"."id" = $3`,
				[]any{"last", now, int64(fakeFirstID + 2999)},
			},
		},
		calls,
	)

	require.Empty(t, f.recorded(func() { require.NoError(t, session.SaveChanges(ctx)) }))

	// Detached rows are left out, and the rest still save in order.
	for _, u := range users[:2990] {
		require.NoError(t, ant.Detach(ctx, session, u))
	}

	users[0].Name = "detached"
	users[2995].Name = "kept"

	calls = f.recorded(func() { require.NoError(t, session.SaveChanges(ctx)) })

	require.Len(t, calls, 1)
	require.Equal(t, []any{"kept", now, int64(fakeFirstID + 2995)}, calls[0].Args)

	tracked, err := ant.Tracked[User](ctx, session)
	require.NoError(t, err)
	require.Equal(t, users[2990:], tracked)
}

// A tracked relation slice has no room to spare, so an append moves it. The
// edits made to its elements are saved wherever they live now, whether the
// parent's relation was loaded with it or the parent was attached again.
func TestTrackedSlicesFollowedWhenTheyMove(t *testing.T) {
	now := time.Date(2026, 3, 4, 5, 6, 7, 0, time.UTC)

	for _, reattach := range []bool{false, true} {
		db, _, f := fakeDB(t, ant.WithNow(func() time.Time { return now }))
		ctx := context.Background()

		f.rows.Store(2)
		f.fan.Store(2)

		session := ant.Session(db)

		users, err := session.Users.Include(&Users.Posts).All(ctx)
		require.NoError(t, err)
		require.Len(t, users[0].Posts, 2)

		if reattach {
			require.NoError(t, ant.Detach(ctx, session, users[0]))
			require.NoError(t, ant.Attach(ctx, session, users[0]))
		}

		before := &users[0].Posts[0]
		users[0].Posts = append(users[0].Posts, Post{Title: "new"})
		require.NotSame(t, before, &users[0].Posts[0])

		users[0].Posts[1].Title = "edited"

		calls := f.recorded(func() { require.NoError(t, session.SaveChanges(ctx)) })

		var updates, inserts []fakeCall

		for _, c := range calls {
			switch {
			case strings.HasPrefix(c.SQL, `UPDATE "posts"`):
				updates = append(updates, c)
			case strings.HasPrefix(c.SQL, `INSERT INTO "posts"`):
				inserts = append(inserts, c)
			}
		}

		require.Len(t, updates, 1, "reattach=%v: %v", reattach, calls)

		require.Equal(
			t,
			`UPDATE "posts" SET "title" = $1, "updated_at" = $2 WHERE "posts"."id" = $3`,
			updates[0].SQL,
		)

		require.Equal(t, []any{"edited", now, int64(fakeFirstID + 1)}, updates[0].Args)
		require.Len(t, inserts, 1)

		// Nothing is left to write, so the moved elements are tracked where
		// they are.
		require.Empty(t, f.recorded(func() { require.NoError(t, session.SaveChanges(ctx)) }))

		users[0].Posts[0].Title = "again"

		calls = f.recorded(func() { require.NoError(t, session.SaveChanges(ctx)) })
		require.Len(t, calls, 1)
		require.Equal(t, []any{"again", now, int64(fakeFirstID)}, calls[0].Args)
	}
}
