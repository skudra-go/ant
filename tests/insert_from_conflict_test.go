package tests

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/skudra-go/ant"
	"github.com/skudra-go/ant/dialect"
	"github.com/skudra-go/ant/internal/testdb"
)

// Membership puts a user on a list once, by a unique pair.
type Membership struct {
	ID        int64
	UserID    int64
	Label     string
	Note      string
	UpdatedAt time.Time
}

func (Membership) Configure(b *ant.ModelBuilder[Membership]) {
	b.Table("conf_memberships")
	b.Index(&Memberships.UserID).And(&Memberships.Label).Unique()
}

var Memberships = ant.Fields[Membership]()

type MembershipsDB struct {
	ant.Context
	Users       ant.Table[User]
	Memberships ant.Table[Membership]
}

func openMemberships(t *testing.T, target testdb.Target) *MembershipsDB {
	t.Helper()

	ctx := t.Context()

	_, err := target.DB.ExecContext(ctx, `CREATE TABLE conf_memberships (
		id `+serials[target.Name]+`,
		user_id BIGINT NOT NULL,
		label VARCHAR(30) NOT NULL,
		note VARCHAR(30) NOT NULL DEFAULT '',
		updated_at `+timestamps[target.Name]+` NULL,
		UNIQUE (user_id, label))`)
	require.NoError(t, err)

	t.Cleanup(func() {
		_, _ = target.DB.ExecContext(context.Background(), "DROP TABLE conf_memberships")
	})

	db, err := ant.Open[MembershipsDB](ctx, target.Dialect, target.DSN)
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })

	return db
}

// InsertFrom takes WithConflict, as InsertMany does, so adding every matching
// user to a list skips, or updates, the pairs that are already there instead
// of failing on them (#231). SQL Server has no conflict clause.
func TestConformanceInsertFromWithConflict(t *testing.T) {
	testdb.ForEach(t, func(t *testing.T, target testdb.Target) {
		ctx := t.Context()
		app := openApp(t, target)
		users := seedUsers(t, app, "alice", 30, "bob", 25, "carol", 40)
		db := openMemberships(t, target)

		list := func(label, note string, q *ant.Query[User]) *ant.Projection[Membership] {
			return ant.Project[Membership](
				q,
				ant.Into(&Memberships.UserID, &Users.ID),
				ant.IntoExpr(&Memberships.Label, ant.SQL[string]("?", label)),
				ant.IntoExpr(&Memberships.Note, ant.SQL[string]("?", note)),
				ant.Into(&Memberships.UpdatedAt, &Users.UpdatedAt),
			)
		}

		byPair := ant.OnConflict(&Memberships.UserID).And(ant.F(&Memberships.Label))
		older := app.Users.Where(ant.Gte(&Users.Age, 30))

		if !has(target, dialect.OnConflict) {
			_, err := db.Memberships.InsertFrom(
				ctx,
				list("news", "first", older),
				ant.WithConflict(byPair.DoNothing()),
			)
			require.ErrorIs(t, err, ant.ErrUnsupported)

			return
		}

		n, err := db.Memberships.InsertFrom(ctx, list("news", "first", older))
		require.NoError(t, err)
		require.EqualValues(t, 2, n, "alice and carol")

		_, err = db.Memberships.InsertFrom(ctx, list("news", "again", app.Users.Query()))
		require.True(t, ant.IsUniqueViolation(err), "without a conflict clause: %v", err)

		n, err = db.Memberships.InsertFrom(
			ctx,
			list("news", "second", app.Users.Query()),
			ant.WithConflict(byPair.DoNothing()),
		)
		require.NoError(t, err)
		require.EqualValues(t, 1, n, "only bob is new")

		notes := func() map[int64]string {
			t.Helper()

			rows, err := db.Memberships.OrderBy(&Memberships.UserID).All(ctx)
			require.NoError(t, err)

			out := map[int64]string{}

			for _, m := range rows {
				out[m.UserID] = m.Note
			}

			return out
		}
		require.Equal(
			t,
			map[int64]string{users[0].ID: "first", users[1].ID: "second", users[2].ID: "first"},
			notes(),
			"DoNothing keeps the rows that were there",
		)

		copied := map[int64]time.Time{}

		rows, err := db.Memberships.All(ctx)
		require.NoError(t, err)

		for _, m := range rows {
			copied[m.UserID] = m.UpdatedAt
		}

		_, err = db.Memberships.InsertFrom(
			ctx,
			list("news", "third", older),
			ant.WithConflict(byPair),
		)
		require.NoError(t, err)

		require.Equal(
			t,
			map[int64]string{users[0].ID: "third", users[1].ID: "second", users[2].ID: "third"},
			notes(),
			"the update overwrites the columns the projection maps",
		)

		stamped, err := db.Memberships.Where(ant.Eq(&Memberships.UserID, users[0].ID)).First(ctx)
		require.NoError(t, err)
		require.False(t, stamped.UpdatedAt.Equal(copied[users[0].ID]), "the update sets UpdatedAt")

		kept, err := db.Memberships.Where(ant.Eq(&Memberships.UserID, users[1].ID)).First(ctx)
		require.NoError(t, err)
		require.True(t, kept.UpdatedAt.Equal(copied[users[1].ID]), "a row the update missed")

		_, err = db.Memberships.InsertFrom(
			ctx,
			list("news", "fourth", older),
			ant.WithConflict(byPair.DoUpdate(ant.F(&Memberships.Note))),
		)
		require.NoError(t, err)
		require.Equal(t, "fourth", notes()[users[2].ID], "DoUpdate names the column")

		_, err = db.Memberships.InsertFrom(
			ctx,
			list("news", "fifth", older),
			ant.WithConflict(byPair.Set(ant.Set(&Memberships.Note, "set"))),
		)
		require.NoError(t, err)
		require.Equal(t, "set", notes()[users[0].ID], "Set assigns on conflict")

		ids, err := ant.Pluck(ctx, db.Memberships.Query(), &Memberships.ID)
		require.NoError(t, err)
		require.Len(t, ids, 3, "no conflict made a row")

		// A list copied from the table it goes into names the same columns on
		// both sides of the statement.
		copyTo := func(label string) *ant.Projection[Membership] {
			return ant.Project[Membership](
				db.Memberships.Where(ant.Eq(&Memberships.Label, "news")),
				ant.Into(&Memberships.UserID, &Memberships.UserID),
				ant.IntoExpr(&Memberships.Label, ant.SQL[string]("?", label)),
				ant.Into(&Memberships.Note, &Memberships.Note),
			)
		}

		n, err = db.Memberships.InsertFrom(
			ctx,
			copyTo("sports"),
			ant.WithConflict(byPair.DoNothing()),
		)
		require.NoError(t, err)
		require.EqualValues(t, 3, n)

		n, err = db.Memberships.InsertFrom(
			ctx,
			copyTo("sports"),
			ant.WithConflict(byPair.DoNothing()),
		)
		require.NoError(t, err)
		require.Zero(t, n, "every pair is there")

		_, err = db.Memberships.InsertFrom(ctx, copyTo("sports"), ant.WithConflict(byPair))
		require.NoError(t, err)

		sports, err := db.Memberships.Where(ant.Eq(&Memberships.Label, "sports")).Count(ctx)
		require.NoError(t, err)
		require.EqualValues(t, 3, sports)

		// A conflict update may read the table it writes. MySQL refuses that
		// after VALUES (error 1093), and takes it after a SELECT.
		latest := ant.SubExpr(db.Memberships.Query(), ant.Max(&Memberships.Note)).Expr()

		_, err = db.Memberships.InsertFrom(
			ctx,
			copyTo("sports"),
			ant.WithConflict(byPair.Set(ant.SetExpr(&Memberships.Note, latest))),
		)
		require.NoError(t, err)

		sportsNotes, err := ant.Pluck(
			ctx,
			db.Memberships.Where(ant.Eq(&Memberships.Label, "sports")),
			&Memberships.Note,
		)
		require.NoError(t, err)
		require.Equal(t, []string{"set", "set", "set"}, sportsNotes, "the largest note")
	})
}

// A conflict update overwrites only the columns the projection maps: one it
// leaves out keeps its stored value, and DoUpdate refuses to name one, since
// the row offered holds the column's default there (#231).
func TestConformanceInsertFromConflictColumns(t *testing.T) {
	testdb.ForEach(t, func(t *testing.T, target testdb.Target) {
		if !has(target, dialect.OnConflict) {
			t.Skip("no conflict clause on " + target.Name)
		}

		ctx := t.Context()
		app := openApp(t, target)
		users := seedUsers(t, app, "alice", 30)
		db := openMemberships(t, target)

		require.NoError(t, db.Memberships.Insert(ctx, &Membership{
			UserID: users[0].ID,
			Label:  "news",
			Note:   "kept",
		}))

		labelOnly := ant.Project[Membership](
			app.Users.Query(),
			ant.Into(&Memberships.UserID, &Users.ID),
			ant.IntoExpr(&Memberships.Label, ant.SQL[string]("?", "news")),
		)

		byPair := ant.OnConflict(&Memberships.UserID).And(ant.F(&Memberships.Label))

		_, err := db.Memberships.InsertFrom(ctx, labelOnly, ant.WithConflict(byPair))
		require.NoError(t, err)

		got, err := db.Memberships.First(ctx)
		require.NoError(t, err)
		require.Equal(t, "kept", got.Note, "note is not mapped, so the update leaves it")

		_, err = db.Memberships.InsertFrom(
			ctx,
			labelOnly,
			ant.WithConflict(byPair.DoUpdate(ant.F(&Memberships.Note))),
		)
		require.ErrorContains(t, err, "Membership.Note")

		_, err = db.Memberships.InsertFrom(
			ctx,
			labelOnly,
			ant.WithConflict(byPair.DoUpdate(ant.F(&Users.Name))),
		)
		require.ErrorContains(t, err, "column name", "a field of another model")
	})
}
