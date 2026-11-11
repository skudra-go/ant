package writing

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/skudra-go/ant"
	"github.com/skudra-go/ant/tests/features/fixture"
)

func many(n int, prefix string) []*fixture.User {
	out := make([]*fixture.User, n)
	for i := range out {
		name := fmt.Sprintf("%s%d", prefix, i)
		out[i] = &fixture.User{Name: name, Email: name, Age: i}
	}

	return out
}

// InsertMany writes many rows in few statements.
func TestInsertMany(t *testing.T) {
	fixture.Run(t, func(t *testing.T, db *fixture.AppDB) {
		ctx := t.Context()

		t.Run("keys match their rows", func(t *testing.T) {
			users := many(2500, "a")
			n, err := db.Users.InsertMany(ctx, users)
			require.NoError(t, err)
			require.EqualValues(t, 2500, n)

			for _, u := range []*fixture.User{users[0], users[1299], users[2499]} {
				stored, err := db.Users.Find(ctx, u.ID)
				require.NoError(t, err)
				require.Equal(t, u.Name, stored.Name)
			}
		})

		t.Run("BatchSize", func(t *testing.T) {
			n, err := db.Users.InsertMany(ctx, many(25, "b"), ant.BatchSize(10))
			require.NoError(t, err)
			require.EqualValues(t, 25, n)
		})

		t.Run("more rows than one statement can bind", func(t *testing.T) {
			n, err := db.Users.InsertMany(ctx, many(12000, "c"), ant.BatchSize(100000))
			require.NoError(t, err)
			require.EqualValues(t, 12000, n)
		})

		t.Run("generated and supplied keys mix", func(t *testing.T) {
			users := many(2, "d")
			users[1].ID = 9_000_000
			_, err := db.Users.InsertMany(ctx, users)
			require.NoError(t, err)
			require.NotZero(t, users[0].ID)
			require.EqualValues(t, 9_000_000, users[1].ID)
		})

		t.Run("all or nothing, and keys are cleared", func(t *testing.T) {
			users := many(5, "e")
			users[4].Email = users[0].Email
			before, err := db.Users.Count(ctx)
			require.NoError(t, err)
			_, err = db.Users.InsertMany(ctx, users, ant.BatchSize(2))
			require.ErrorContains(t, err, "duplicate key")
			require.Zero(t, users[0].ID)

			after, err := db.Users.Count(ctx)
			require.NoError(t, err)
			require.Equal(t, before, after)
		})

		t.Run("WithConflict skips existing rows", func(t *testing.T) {
			users := many(3, "a")
			n, err := db.Users.InsertMany(
				ctx,
				users,
				ant.WithConflict(ant.OnConflict(&Users.Email).DoNothing()),
			)
			require.NoError(t, err)
			require.Zero(t, n)
		})

		t.Run("WithConflict can update them", func(t *testing.T) {
			users := many(2, "a")
			users[0].Age = 77

			_, err := db.Users.InsertMany(
				ctx,
				users,
				ant.WithConflict(ant.OnConflict(&Users.Email)),
			)
			require.NoError(t, err)
			got, err := db.Users.Where(ant.Eq(&Users.Email, "a0")).Single(ctx)
			require.NoError(t, err)
			require.Equal(t, 77, got.Age)
		})

		t.Run("edge cases", func(t *testing.T) {
			n, err := db.Users.InsertMany(ctx, nil)
			require.NoError(t, err)
			require.Zero(t, n)

			_, err = db.Users.InsertMany(ctx, []*fixture.User{nil})
			require.ErrorIs(t, err, ant.ErrNilEntity)
			_, err = db.Users.InsertMany(ctx, many(1, "f"), ant.BatchSize(0))
			require.Error(t, err)
		})
	})
}

type Copy struct {
	ID    int64
	Title string
	Tag   string
}

type ArchiveDB struct {
	ant.Context
	Posts  ant.Table[fixture.Post]
	Users  ant.Table[fixture.User]
	Copies ant.Table[Copy]
}

var Copies = ant.Fields[Copy]()

// InsertFrom copies rows with one statement.
func TestInsertFrom(t *testing.T) {
	ddl := `CREATE TABLE copies (id BIGSERIAL PRIMARY KEY, title TEXT NOT NULL, tag TEXT NOT NULL)`
	fixture.RunWith(t, ddl, func(t *testing.T, db *ArchiveDB) {
		ctx := t.Context()
		owner := &fixture.User{Name: "o", Email: "o"}
		require.NoError(t, db.Users.Insert(ctx, owner))

		require.NoError(t, db.Posts.Insert(ctx,
			&fixture.Post{UserID: owner.ID, Title: "one", Published: true},
			&fixture.Post{UserID: owner.ID, Title: "two"}))

		n, err := db.Copies.InsertFrom(ctx, ant.Project[Copy](
			db.Posts.Where(ant.Eq(&Posts.Published, true)),
			ant.Into(&Copies.Title, &Posts.Title),
			ant.IntoExpr(&Copies.Tag, ant.SQL[string]("'archived'")),
		))
		require.NoError(t, err)
		require.EqualValues(t, 1, n)

		got, err := db.Copies.Single(ctx)
		require.NoError(t, err)
		require.Equal(t, Copy{ID: got.ID, Title: "one", Tag: "archived"}, *got)

		_, err = db.Copies.InsertFrom(ctx, ant.Project[Copy](db.Posts.Where()))
		require.ErrorIs(t, err, ant.ErrNoColumns)
	})
}
