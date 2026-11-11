package lifecycle

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/skudra-go/ant"
	"github.com/skudra-go/ant/tests/features/fixture"
)

type tenantKey struct{}

func withTenant(ctx context.Context, tenant int64) context.Context {
	return context.WithValue(ctx, tenantKey{}, tenant)
}

// A global filter applies to every query of a model, using values from the
// call's context. It keeps one tenant's data out of another's reach, even in
// joins and subqueries.
type Doc struct {
	ID       int64
	TenantID int64
	OwnerID  int64
	Title    string
	Secret   bool
	Owner    *Owner
}

func (Doc) Configure(b *ant.ModelBuilder[Doc]) {
	b.Filter("tenant", func(ctx context.Context) ant.Cond {
		tenant, ok := ctx.Value(tenantKey{}).(int64)
		if !ok {
			return ant.FailedCond(errors.New("no tenant in context"))
		}

		return ant.Eq(&Docs.TenantID, tenant)
	})

	b.Filter("public", func(context.Context) ant.Cond { return ant.Eq(&Docs.Secret, false) })
}

type Owner struct {
	ID   int64
	Name string
	Docs []Doc
}

type DocsDB struct {
	ant.Context
	Docs   ant.Table[Doc]
	Owners ant.Table[Owner]
}

var (
	Docs   = ant.Fields[Doc]()
	Owners = ant.Fields[Owner]()
)

func TestGlobalFilters(t *testing.T) {
	ddl := `
		CREATE TABLE owners (id BIGSERIAL PRIMARY KEY, name TEXT NOT NULL);
		CREATE TABLE docs (id BIGSERIAL PRIMARY KEY, tenant_id BIGINT NOT NULL, owner_id BIGINT NOT NULL REFERENCES owners,
			title TEXT NOT NULL, secret BOOL NOT NULL)`
	fixture.RunWith(t, ddl, func(t *testing.T, db *DocsDB) {
		one := withTenant(t.Context(), 1)
		two := withTenant(t.Context(), 2)
		owner := &Owner{Name: "Ann"}
		require.NoError(t, db.Owners.Insert(one, owner))

		require.NoError(t, db.Docs.Insert(one,
			&Doc{TenantID: 1, OwnerID: owner.ID, Title: "one"},
			&Doc{
				TenantID: 1,
				OwnerID:  owner.ID,
				Title:    "hidden",
				Secret:   true,
			},
			&Doc{TenantID: 2, OwnerID: owner.ID, Title: "two"}))

		titles := func(ctx context.Context, q *ant.Query[Doc]) []string {
			got, err := ant.Pluck(ctx, q, &Docs.Title)
			require.NoError(t, err)

			return got
		}

		t.Run("every query is filtered", func(t *testing.T) {
			require.Equal(t, []string{"one"}, titles(one, db.Docs.Where()))
			require.Equal(t, []string{"two"}, titles(two, db.Docs.Where()))
		})

		t.Run("the same query in another context gets other rows", func(t *testing.T) {
			q := db.Docs.Where()
			require.Equal(t, []string{"one"}, titles(one, q))
			require.Equal(t, []string{"two"}, titles(two, q))
		})

		t.Run("Count, Include, Has, and joins", func(t *testing.T) {
			n, err := db.Docs.Count(one)
			require.NoError(t, err)
			require.EqualValues(t, 1, n)

			got, err := db.Owners.Include(&Owners.Docs).Single(two)
			require.NoError(t, err)
			require.Len(t, got.Docs, 1)
			require.Equal(t, "two", got.Docs[0].Title)

			hits, err := db.Owners.Where(ant.Has(&Owners.Docs, ant.Eq(&Docs.Title, "two"))).
				Count(one)
			require.NoError(t, err)
			require.Zero(t, hits)

			rows, err := db.Owners.JoinMany(&Owners.Docs).
				Select(func(o *Owner, d *Doc) string { return d.Title }).
				All(two)
			require.NoError(t, err)
			require.Equal(t, []string{"two"}, rows)
		})

		// A subquery is built with no context, and still gets the caller's.
		t.Run("subqueries are filtered with the caller's context", func(t *testing.T) {
			sub := ant.SubOf(db.Docs.Where(), &Docs.OwnerID)
			q := db.Owners.Where(ant.InSub(&Owners.ID, sub))
			n, err := q.Count(one)
			require.NoError(t, err)
			require.EqualValues(t, 1, n)

			sub = ant.SubOf(db.Docs.Where(ant.Eq(&Docs.Title, "two")), &Docs.OwnerID)
			n, err = db.Owners.Where(ant.InSub(&Owners.ID, sub)).Count(one)
			require.NoError(t, err)
			require.Zero(t, n, "tenant two's row is invisible to tenant one")
		})

		t.Run("bulk writes are filtered", func(t *testing.T) {
			n, err := db.Docs.Unfiltered().Update(one, ant.Set(&Docs.Title, "ONE"))
			require.NoError(t, err)
			require.EqualValues(t, 1, n)
			n, err = db.Docs.Where(ant.Eq(&Docs.Title, "two")).Delete(one)
			require.NoError(t, err)
			require.Zero(t, n)
		})

		t.Run("a filter's error stops the query", func(t *testing.T) {
			_, err := db.Docs.All(t.Context())
			require.ErrorContains(t, err, "no tenant in context")
		})

		t.Run("IgnoreFilters by name", func(t *testing.T) {
			require.ElementsMatch(
				t,
				[]string{"ONE", "hidden"},
				titles(one, db.Docs.IgnoreFilters("public")),
			)
		})

		t.Run("IgnoreFilters with no names", func(t *testing.T) {
			got := titles(t.Context(), db.Docs.IgnoreFilters())
			require.ElementsMatch(t, []string{"ONE", "hidden", "two"}, got)
		})
	})
}

// A scope is a named, reusable piece of a query.
func Adults(q *ant.Query[fixture.User]) *ant.Query[fixture.User] {
	return q.Where(ant.Gte(&fixture.Users.Age, 18))
}

func OlderFirst(q *ant.Query[fixture.User]) *ant.Query[fixture.User] {
	return q.OrderByDesc(&fixture.Users.Age)
}

func TestScopes(t *testing.T) {
	fixture.Run(t, func(t *testing.T, db *fixture.AppDB) {
		fixture.Seed(t, db)
		got, err := db.Users.Scope(Adults, OlderFirst).All(t.Context())
		require.NoError(t, err)
		require.Equal(t, []string{"Carol", "Alice"}, fixture.Names(got))
	})
}
