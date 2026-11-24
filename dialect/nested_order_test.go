package dialect_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/skudra-go/ant/dialect/postgres"
	"github.com/skudra-go/ant/dialect/sqlserver"
	"github.com/skudra-go/ant/qb"
)

// SQL Server keeps an ORDER BY where it counts: on the statement, on an INSERT's
// SELECT, and on a nested query with paging. Elsewhere the order is left out,
// and other databases keep it (#93).
func TestNestedOrder(t *testing.T) {
	users := &qb.TableRef{Name: "users"}
	name := qb.Column{Table: users, Name: "name"}
	ordered := func() *qb.Select {
		return &qb.Select{
			Columns: []qb.Selected{{Value: name}},
			From:    qb.Source{Table: users},
			Order:   []qb.Order{{Value: name}},
		}
	}

	one := 1
	paged := ordered()
	paged.Limit = &one

	for _, c := range []struct {
		name      string
		stmt      qb.Statement
		sqlserver string
		postgres  string
	}{
		{
			name:      "statement",
			stmt:      ordered(),
			sqlserver: `SELECT [users].[name] FROM [users] ORDER BY [users].[name]`,
			postgres:  `SELECT "users"."name" FROM "users" ORDER BY "users"."name"`,
		},
		{
			name:      "rows of an insert",
			stmt:      &qb.Insert{Table: users, Columns: []string{"name"}, From: ordered()},
			sqlserver: `INSERT INTO [users] ([name]) SELECT [users].[name] FROM [users] ORDER BY [users].[name]`,
			postgres:  `INSERT INTO "users" ("name") SELECT "users"."name" FROM "users" ORDER BY "users"."name"`,
		},
		{
			name:      "derived table",
			stmt:      &qb.Select{Columns: []qb.Selected{{Value: qb.Star{}}}, From: qb.Source{Sub: ordered(), Alias: "u"}},
			sqlserver: `SELECT * FROM (SELECT [users].[name] FROM [users]) AS [u]`,
			postgres:  `SELECT * FROM (SELECT "users"."name" FROM "users" ORDER BY "users"."name") AS "u"`,
		},
		{
			name:      "paged derived table",
			stmt:      &qb.Select{Columns: []qb.Selected{{Value: qb.Star{}}}, From: qb.Source{Sub: paged, Alias: "u"}},
			sqlserver: `SELECT * FROM (SELECT [users].[name] FROM [users] ORDER BY [users].[name] OFFSET 0 ROWS FETCH NEXT @p1 ROWS ONLY) AS [u]`,
			postgres:  `SELECT * FROM (SELECT "users"."name" FROM "users" ORDER BY "users"."name" LIMIT $1) AS "u"`,
		},
		{
			name: "CTE",
			stmt: &qb.Select{
				With:    []qb.CTE{{Name: "c", Select: ordered()}},
				Columns: []qb.Selected{{Value: qb.Star{}}},
				From:    qb.Source{Name: "c"},
			},
			sqlserver: `WITH [c] AS (SELECT [users].[name] FROM [users]) SELECT * FROM [c]`,
			postgres:  `WITH "c" AS (SELECT "users"."name" FROM "users" ORDER BY "users"."name") SELECT * FROM "c"`,
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			got, _, err := sqlserver.Dialect.Render(c.stmt)
			require.NoError(t, err)
			require.Equal(t, c.sqlserver, got)

			got, _, err = postgres.Dialect.Render(c.stmt)
			require.NoError(t, err)
			require.Equal(t, c.postgres, got)
		})
	}
}
