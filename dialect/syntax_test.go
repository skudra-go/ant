package dialect_test

import (
	"errors"
	"testing"

	"github.com/skudra-go/ant/dialect"
	"github.com/skudra-go/ant/dialect/mysql"
	"github.com/skudra-go/ant/dialect/postgres"
	"github.com/skudra-go/ant/dialect/sqlite"
	"github.com/skudra-go/ant/dialect/sqlserver"
	"github.com/skudra-go/ant/internal/errs"
	"github.com/skudra-go/ant/qb"
)

var (
	users  = &qb.TableRef{Name: "users"}
	age    = qb.Column{Name: "age"}
	id     = qb.Selected{Value: qb.Column{Name: "id"}}
	ages   = []qb.Selected{{Value: age}}
	limit  = 10
	offset = 20
)

func TestSyntax(t *testing.T) {
	page := &qb.Select{
		Columns: ages,
		From:    qb.Source{Table: users},
		Limit:   &limit,
		Offset:  &offset,
	}

	onlyOffset := &qb.Select{Columns: ages, From: qb.Source{Table: users}, Offset: &offset}

	insert := &qb.Insert{
		Table:     users,
		Columns:   []string{"age"},
		Rows:      [][]qb.Expr{{qb.Param{Value: 1}}},
		Returning: []qb.Selected{id},
	}

	upsert := &qb.Insert{
		Table:   users,
		Columns: []string{"age"},
		Rows:    [][]qb.Expr{{qb.Param{Value: 1}}},
		Conflict: &qb.Conflict{
			Columns: []string{"age"},
			Set: []qb.Assignment{
				{Column: "age", Value: qb.Column{Alias: "excluded", Name: "age"}},
			},
		},
	}

	nothing := &qb.Insert{
		Table:    users,
		Columns:  []string{"age"},
		Rows:     [][]qb.Expr{{qb.Param{Value: 1}}},
		Conflict: &qb.Conflict{Columns: []string{"age"}},
	}

	remove := &qb.Delete{
		Table:     users,
		Where:     []qb.Cond{qb.Compare{Left: age, Op: qb.OpEq, Right: qb.Param{Value: 1}}},
		Returning: []qb.Selected{id},
	}

	like := &qb.Select{
		Columns: ages,
		From:    qb.Source{Table: users},
		Where:   []qb.Cond{qb.Compare{Left: age, Op: qb.OpILike, Right: qb.Param{Value: "a"}}},
	}

	filtered := &qb.Select{
		From: qb.Source{Table: users},
		Columns: []qb.Selected{{
			Value: qb.Func{
				Name:   "COUNT",
				Args:   []qb.Expr{qb.Star{}},
				Filter: []qb.Cond{qb.IsNull{Left: age}},
			},
		}},
	}

	nulls := &qb.Select{
		Columns: ages,
		From:    qb.Source{Table: users},
		Order:   []qb.Order{{Value: age, Nulls: qb.NullsLast}},
	}

	empty := &qb.Insert{Table: users}

	merge := &qb.Merge{
		Table: users,
		Using: qb.Source{Table: users},
		On:    []qb.Cond{qb.Compare{Left: age, Op: qb.OpEq, Right: qb.Param{Value: 1}}},
		Actions: []qb.MergeAction{
			{Columns: []string{"age"}, Values: []qb.Expr{qb.Param{Value: 1}}},
		},
	}

	for _, tt := range []struct {
		name string
		d    dialect.Dialect
		stmt qb.Statement
		want string
	}{
		{"postgres paging", postgres.Dialect, page, `SELECT "age" FROM "users" LIMIT $1 OFFSET $2`},
		{"postgres upsert", postgres.Dialect, upsert, `INSERT INTO "users" ("age") VALUES ($1) ON CONFLICT ("age") DO UPDATE SET "age" = "excluded"."age"`},

		{"sqlite offset alone", sqlite.Dialect, onlyOffset, `SELECT "age" FROM "users" LIMIT -1 OFFSET ?`},
		{"sqlite ilike", sqlite.Dialect, like, `SELECT "age" FROM "users" WHERE LOWER("age") LIKE LOWER(?)`},

		{"mysql offset alone", mysql.Dialect, onlyOffset, "SELECT `age` FROM `users` LIMIT 18446744073709551615 OFFSET ?"},
		{"mysql upsert", mysql.Dialect, upsert, "INSERT INTO `users` (`age`) VALUES (?) ON DUPLICATE KEY UPDATE `age` = VALUES(`age`)"},
		{"mysql do nothing", mysql.Dialect, nothing, "INSERT INTO `users` (`age`) VALUES (?) ON DUPLICATE KEY UPDATE `age` = `age`"},
		{"mysql filter", mysql.Dialect, filtered, "SELECT COUNT(CASE WHEN `age` IS NULL THEN 1 END) FROM `users`"},
		{"mysql nulls", mysql.Dialect, nulls, "SELECT `age` FROM `users` ORDER BY CASE WHEN `age` IS NULL THEN 1 ELSE 0 END, `age`"},
		{"mysql empty insert", mysql.Dialect, empty, "INSERT INTO `users` VALUES ()"},

		{"mariadb returning", mysql.MariaDB, insert, "INSERT INTO `users` (`age`) VALUES (?) RETURNING `id`"},

		{"sqlserver paging", sqlserver.Dialect, page, "SELECT [age] FROM [users] ORDER BY (SELECT NULL) OFFSET @p1 ROWS FETCH NEXT @p2 ROWS ONLY"},
		{"sqlserver insert", sqlserver.Dialect, insert, "INSERT INTO [users] ([age]) OUTPUT INSERTED.[id] VALUES (@p1)"},
		{"sqlserver delete", sqlserver.Dialect, remove, "DELETE FROM [users] OUTPUT DELETED.[id] WHERE [age] = @p1"},
		// Two MERGEs at once must not both find no row and both insert it.
		{"sqlserver merge", sqlserver.Dialect, merge, "MERGE INTO [users] WITH (HOLDLOCK) USING [users] ON [age] = @p1 WHEN NOT MATCHED THEN INSERT ([age]) VALUES (@p2);"},
		{"postgres merge", postgres.Dialect, merge, `MERGE INTO "users" USING "users" ON "age" = $1 WHEN NOT MATCHED THEN INSERT ("age") VALUES ($2)`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got, _, err := tt.d.Render(tt.stmt)
			if err != nil {
				t.Fatal(err)
			}

			if got != tt.want {
				t.Errorf("got  %s\nwant %s", got, tt.want)
			}
		})
	}
}

// A database that lacks something says so, rather than sending SQL it refuses.
func TestMissingFeatures(t *testing.T) {
	merge := &qb.Merge{Table: users, Using: qb.Source{Table: users}}

	locked := &qb.Select{
		Columns: ages,
		From:    qb.Source{Table: users},
		Lock:    &qb.Lock{Mode: "UPDATE"},
	}

	upsert := &qb.Insert{
		Table:    users,
		Columns:  []string{"age"},
		Rows:     [][]qb.Expr{{qb.Param{Value: 1}}},
		Conflict: &qb.Conflict{},
	}

	guarded := &qb.Insert{
		Table:   users,
		Columns: []string{"age"},
		Rows:    [][]qb.Expr{{qb.Param{Value: 1}}},
		Conflict: &qb.Conflict{
			Set:   []qb.Assignment{{Column: "age", Value: age}},
			Where: []qb.Cond{qb.IsNull{Left: age}},
		},
	}

	returning := &qb.Insert{
		Table:     users,
		Columns:   []string{"age"},
		Rows:      [][]qb.Expr{{qb.Param{Value: 1}}},
		Returning: []qb.Selected{id},
	}

	for _, tt := range []struct {
		name string
		d    dialect.Dialect
		stmt qb.Statement
	}{
		{"merge on mysql", mysql.Dialect, merge},
		{"merge on sqlite", sqlite.Dialect, merge},
		{"locks on sqlite", sqlite.Dialect, locked},
		{"upsert on sqlserver", sqlserver.Dialect, upsert},
		{"upsert condition on mysql", mysql.Dialect, guarded},
		{"returning on mysql", mysql.Dialect, returning},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if _, _, err := tt.d.Render(tt.stmt); !errors.Is(err, errs.ErrUnsupported) {
				t.Errorf("got %v", err)
			}
		})
	}
}

func TestSupports(t *testing.T) {
	if !dialect.Supports(postgres.Dialect, dialect.Merge) {
		t.Error("PostgreSQL has MERGE")
	}

	if dialect.Supports(mysql.Dialect, dialect.Merge) {
		t.Error("MySQL has no MERGE")
	}

	if !dialect.Supports(mysql.MariaDB, dialect.ReturningInsert) {
		t.Error("MariaDB returns from INSERT")
	}
}
