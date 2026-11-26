package dialect_test

import (
	"testing"

	"github.com/skudra-go/ant/dialect"
	"github.com/skudra-go/ant/dialect/mysql"
	"github.com/skudra-go/ant/dialect/postgres"
	"github.com/skudra-go/ant/dialect/sqlite"
	"github.com/skudra-go/ant/qb"
)

// An INSERT ... SELECT with a conflict clause reads on every database that has
// one: SQLite needs a WHERE between the SELECT's FROM and ON CONFLICT, and
// MySQL reads the rows from a derived table whose columns cannot be taken for
// the table's own in ON DUPLICATE KEY UPDATE (#231).
func TestRenderInsertSelectWithConflict(t *testing.T) {
	lists := &qb.TableRef{Name: "lists"}
	users := &qb.TableRef{Name: "users"}
	from := func(where ...qb.Cond) *qb.Select {
		return &qb.Select{
			Columns: []qb.Selected{
				{Value: qb.Column{Table: users, Name: "id"}, Alias: "user_id"},
				{Value: qb.Column{Table: users, Name: "name"}, Alias: "note"},
			},
			From:  qb.Source{Table: users},
			Where: where,
		}
	}
	insert := func(conflict *qb.Conflict, where ...qb.Cond) *qb.Insert {
		return &qb.Insert{
			Table:    lists,
			Columns:  []string{"user_id", "note"},
			From:     from(where...),
			Conflict: conflict,
		}
	}

	nothing := &qb.Conflict{Columns: []string{"user_id"}}

	update := &qb.Conflict{
		Columns: []string{"user_id"},
		Set: []qb.Assignment{
			{Column: "note", Value: qb.Column{Alias: "excluded", Name: "note"}},
		},
	}

	adult := qb.Compare{
		Left:  qb.Column{Table: users, Name: "age"},
		Op:    qb.OpGte,
		Right: qb.Param{Value: 18},
	}

	cases := []struct {
		name string
		d    dialect.Dialect
		stmt *qb.Insert
		want string
	}{
		{
			"postgres, no conflict",
			postgres.Dialect,
			insert(nil),
			`INSERT INTO "lists" ("user_id", "note") SELECT "users"."id" AS "user_id", "users"."name" AS "note" FROM "users"`,
		},
		{
			"postgres",
			postgres.Dialect,
			insert(nothing),
			`INSERT INTO "lists" ("user_id", "note") SELECT "users"."id" AS "user_id", "users"."name" AS "note" FROM "users" WHERE (1 = 1) ON CONFLICT ("user_id") DO NOTHING`,
		},
		{
			"sqlite",
			sqlite.Dialect,
			insert(update),
			`INSERT INTO "lists" ("user_id", "note") SELECT "users"."id" AS "user_id", "users"."name" AS "note" FROM "users" WHERE (1 = 1) ON CONFLICT ("user_id") DO UPDATE SET "note" = "excluded"."note"`,
		},
		{
			"sqlite, a WHERE of its own",
			sqlite.Dialect,
			insert(nothing, adult),
			`INSERT INTO "lists" ("user_id", "note") SELECT "users"."id" AS "user_id", "users"."name" AS "note" FROM "users" WHERE "users"."age" >= ? ON CONFLICT ("user_id") DO NOTHING`,
		},
		{
			"mysql, nothing",
			mysql.Dialect,
			insert(nothing),
			"INSERT INTO `lists` (`user_id`, `note`) SELECT * FROM (SELECT `users`.`id` AS `ant_c1`, `users`.`name` AS `ant_c2` FROM `users`) AS `ant_rows` ON DUPLICATE KEY UPDATE `user_id` = `user_id`",
		},
		{
			"mysql, update",
			mysql.Dialect,
			insert(update, adult),
			"INSERT INTO `lists` (`user_id`, `note`) SELECT * FROM (SELECT `users`.`id` AS `ant_c1`, `users`.`name` AS `ant_c2` FROM `users` WHERE `users`.`age` >= ?) AS `ant_rows` ON DUPLICATE KEY UPDATE `note` = VALUES(`note`)",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, _, err := c.d.Render(c.stmt)
			if err != nil {
				t.Fatal(err)
			}

			if got != c.want {
				t.Fatalf("got\n%s\nwant\n%s", got, c.want)
			}
		})
	}
}
