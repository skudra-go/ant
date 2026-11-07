package dialect_test

import (
	"errors"
	"strings"
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

	// IN refuses a LIMIT on MySQL and MariaDB, and an offset alone is written
	// with one.
	inPage := func(sub *qb.Select) *qb.Select {
		return &qb.Select{
			Columns: ages,
			From:    qb.Source{Table: users},
			Where:   []qb.Cond{qb.In{Left: age, Sub: sub}},
		}
	}

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
		{"mariadb in paged", mysql.MariaDB, inPage(page), "SELECT `age` FROM `users` WHERE `age` IN (SELECT * FROM (SELECT `age` FROM `users` LIMIT ? OFFSET ?) AS limited)"},
		{"mariadb in offset alone", mysql.MariaDB, inPage(onlyOffset), "SELECT `age` FROM `users` WHERE `age` IN (SELECT * FROM (SELECT `age` FROM `users` LIMIT 18446744073709551615 OFFSET ?) AS limited)"},
		{"mysql in offset alone", mysql.Dialect, inPage(onlyOffset), "SELECT `age` FROM `users` WHERE `age` IN (SELECT * FROM (SELECT `age` FROM `users` LIMIT 18446744073709551615 OFFSET ?) AS limited)"},
		{"postgres in offset alone", postgres.Dialect, inPage(onlyOffset), `SELECT "age" FROM "users" WHERE "age" IN (SELECT "age" FROM "users" OFFSET $1)`},

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

// SQL Server has no RECURSIVE and joins a recursive WITH only with UNION ALL, so
// a CTE with a key carries the path of keys to each row, which ends a cycle, and
// is read once per key. Other databases write it as it is.
func TestRecursiveWith(t *testing.T) {
	nodes := &qb.TableRef{Name: "nodes"}
	col := func(name string) qb.Column { return qb.Column{Table: nodes, Name: name} }
	columns := []qb.Selected{{Value: col("a")}, {Value: col("b")}, {Value: col("up")}}

	step := &qb.Select{
		Columns: columns,
		From:    qb.Source{Table: nodes},
		Joins: []qb.Join{
			{
				Source: qb.Source{Name: "tree"},
				On: []qb.Cond{
					qb.Compare{
						Left:  col("up"),
						Op:    qb.OpEq,
						Right: qb.Column{Alias: "tree", Name: "a"},
					},
				},
			},
		},
	}

	other := &qb.Select{Columns: columns, From: qb.Source{Table: nodes, Alias: "n"}}

	stmt := &qb.Select{
		With: []qb.CTE{{
			Name: "tree",
			Key:  []string{"a", "b"},
			Select: &qb.Select{
				Columns: columns,
				From:    qb.Source{Table: nodes},
				Where:   []qb.Cond{qb.IsNull{Left: col("up")}},
				Sets: []qb.SetOp{
					{Kind: "UNION", Select: step},
					{Kind: "UNION", Select: other},
				},
			},
		}},
		Recursive: true,
		Columns:   []qb.Selected{{Value: qb.Column{Alias: "nodes", Name: "a"}}},
		From:      qb.Source{Name: "tree", Alias: "nodes"},
	}

	got, _, err := sqlserver.Dialect.Render(stmt)
	if err != nil {
		t.Fatal(err)
	}

	key := func(alias string) string {
		return "CONVERT(varchar(max), CAST([" + alias + "].[a] AS varbinary(max)), 2), ':', " +
			"CONVERT(varchar(max), CAST([" + alias + "].[b] AS varbinary(max)), 2)"
	}

	want := "WITH [tree] AS (SELECT [nodes].[a], [nodes].[b], [nodes].[up], " +
		"CAST(CONCAT('/', " + key("nodes") + ", '/') AS varchar(max)) AS [ant_path] " +
		"FROM [nodes] WHERE [nodes].[up] IS NULL " +
		"UNION ALL (SELECT [nodes].[a], [nodes].[b], [nodes].[up], " +
		"CAST(CONCAT([tree].[ant_path], " + key("nodes") + ", '/') AS varchar(max)) " +
		"FROM [nodes] JOIN [tree] ON [nodes].[up] = [tree].[a] " +
		"WHERE (CHARINDEX(CONCAT('/', " + key("nodes") + ", '/'), [tree].[ant_path]) = 0)) " +
		"UNION ALL (SELECT [nodes].[a], [nodes].[b], [nodes].[up], " +
		"CAST(CONCAT('/', " + key("n") + ", '/') AS varchar(max)) FROM [nodes] AS [n])) " +
		"SELECT [nodes].[a] FROM (SELECT * FROM (SELECT *, ROW_NUMBER() OVER (PARTITION BY [a], [b] " +
		"ORDER BY (SELECT NULL)) AS [ant_n] FROM [tree]) AS [tree] WHERE [ant_n] = 1) AS [nodes] " +
		"OPTION (MAXRECURSION 0)"

	if got != want {
		t.Errorf("got  %s\nwant %s", got, want)
	}

	got, _, err = postgres.Dialect.Render(stmt)
	if err != nil {
		t.Fatal(err)
	}

	if !strings.HasPrefix(
		got,
		`WITH RECURSIVE "tree" AS (SELECT "nodes"."a", "nodes"."b", "nodes"."up" FROM`,
	) ||
		strings.Contains(got, "ant_path") ||
		!strings.HasSuffix(got, `FROM "tree" AS "nodes"`) {
		t.Errorf("postgres: %s", got)
	}

	// Without a key there is no path, and the CTE is written as it is.
	stmt.With[0].Key = nil

	got, _, err = sqlserver.Dialect.Render(stmt)
	if err != nil {
		t.Fatal(err)
	}

	if !strings.HasPrefix(got, "WITH [tree] AS (") || strings.Contains(got, "ant_path") ||
		strings.Contains(got, "MAXRECURSION") {
		t.Errorf("no key: %s", got)
	}
}

// An IN list longer than the parameters a statement takes goes as one array
// where the database takes arrays, and stays a list elsewhere, or when its
// values are not plain values of one type.
func TestLongInLists(t *testing.T) {
	list := func(n int, value func(i int) any) *qb.Select {
		values := make([]qb.Expr, n)
		for i := range values {
			values[i] = qb.Param{Value: value(i)}
		}

		return &qb.Select{
			Columns: ages,
			From:    qb.Source{Table: users},
			Where: []qb.Cond{
				qb.In{Left: age, Values: values},
				qb.In{Left: age, Values: values, Negate: true},
			},
		}
	}

	long := 65536

	got, args, err := postgres.Dialect.Render(list(long, func(i int) any { return int64(i) }))
	if err != nil {
		t.Fatal(err)
	}

	if want := `SELECT "age" FROM "users" WHERE "age" = ANY($1) AND "age" <> ALL($2)`; got != want {
		t.Errorf("got  %s\nwant %s", got, want)
	}

	if array, ok := args[0].([]int64); !ok || len(array) != long || array[long-1] != int64(long-1) {
		t.Errorf("args[0] is %T", args[0])
	}

	// A short list stays a list.
	got, _, err = postgres.Dialect.Render(list(2, func(i int) any { return int64(i) }))
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(got, `"age" IN ($1, $2)`) {
		t.Errorf("short: %s", got)
	}

	for name, value := range map[string]func(int) any{
		"mixed types": func(i int) any {
			if i%2 == 0 {
				return int64(i)
			}

			return "x"
		},
		"secrets": func(i int) any { return qb.Secret{V: i} },
	} {
		_, args, err := postgres.Dialect.Render(list(long, value))
		if err != nil {
			t.Fatal(err)
		}

		if len(args) != 2*long {
			t.Errorf("%s: %d args", name, len(args))
		}
	}

	_, args, err = sqlite.Dialect.Render(list(40000, func(i int) any { return int64(i) }))
	if err != nil {
		t.Fatal(err)
	}

	if len(args) != 80000 {
		t.Errorf("sqlite: %d args", len(args))
	}
}
