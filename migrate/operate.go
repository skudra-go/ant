package migrate

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path"
	"slices"
	"strings"
	"time"

	"github.com/skudra-go/ant"
	"github.com/skudra-go/ant/internal/liveschema"
	"github.com/skudra-go/ant/schema"
)

// repair accepts the files as they are: failed rows go, so those migrations run
// again, stored checksums follow the files, and rows with no file are forgotten.
func (e *engine) repair(ctx context.Context) error {
	return e.locked(ctx, func(conn *sql.Conn) error {
		if err := e.history.ensure(ctx, conn); err != nil {
			return err
		}

		c, err := e.catalog(ctx)
		if err != nil {
			return err
		}

		rows, err := e.history.read(ctx, conn)
		if err != nil {
			return err
		}

		count := 0

		byKey := func(key string) *record {
			for i := range rows {
				if rows[i].Key == key {
					return &rows[i]
				}
			}

			return nil
		}

		for _, entry := range e.entries(c, rows) {
			switch {
			case entry.State == Failed:
				e.say(
					"Cleared the failed run of %s, so up will run it again. If you finished it by hand, run baseline %s.",
					entry.ID,
					versionOf(entry.ID),
				)

				err = e.history.remove(ctx, conn, Versioned, versionOf(entry.ID))
			case entry.State == Changed:
				e.say("Accepted the current contents of %s.", entry.ID)
				r := byKey(entry.migration.key())
				r.Checksum = entry.migration.Checksum()
				err = e.history.write(ctx, conn, *r)
			case entry.State == Missing:
				e.say("Forgot %s, which has no file.", entry.ID)
				err = e.history.remove(ctx, conn, Versioned, versionOf(entry.ID))
			case entry.State == Removed:
				e.say("Forgot %s, which has no file.", entry.ID)
				err = e.history.remove(ctx, conn, Repeatable, entry.ID)
			default:
				continue
			}

			if err != nil {
				return err
			}

			count++
		}

		if count == 0 {
			e.say("Nothing to repair.")
		}

		return nil
	})
}

// baseline records versioned migrations as applied without running them, through
// version, or all of them. It is how an existing database joins.
func (e *engine) baseline(ctx context.Context, version string) error {
	return e.locked(ctx, func(conn *sql.Conn) error {
		if err := e.history.ensure(ctx, conn); err != nil {
			return err
		}

		c, err := e.catalog(ctx)
		if err != nil {
			return err
		}

		if version != "" &&
			!slices.ContainsFunc(
				c.versioned,
				func(m Migration) bool { return m.Version == version },
			) {
			return fmt.Errorf("%w: no migration has version %s", ErrBadMigration, version)
		}

		rows, err := e.history.read(ctx, conn)
		if err != nil {
			return err
		}

		count := 0

		for _, m := range c.versioned {
			if version != "" && compareVersions(m.Version, version) > 0 {
				continue
			}

			if slices.ContainsFunc(
				rows,
				func(r record) bool {
					return r.Kind == Versioned && r.Key == m.Version && r.Success
				},
			) {
				continue
			}

			r := record{
				Kind:      Versioned,
				Key:       m.Version,
				Name:      m.Name,
				Checksum:  m.Checksum(),
				AppliedAt: time.Now(),
				Success:   true,
			}

			if err := e.history.write(ctx, conn, r); err != nil {
				return err
			}

			e.say("Recorded %s as applied, without running it.", m.ID())

			count++
		}

		if count == 0 {
			e.say("Nothing to record.")
		}

		return nil
	})
}

// reset removes everything in the schemas migrations use, then applies every
// migration from the start. confirm must be the database's name.
func (e *engine) reset(
	ctx context.Context,
	schemas []string,
	confirm func(database string) error,
) error {
	database, err := e.d.Database(ctx, e.pool)
	if err != nil {
		return err
	}

	if err := confirm(database); err != nil {
		return err
	}

	err = e.locked(ctx, func(conn *sql.Conn) error {
		wiped := slices.Concat([]string{""}, schemas)
		if e.history.schema != "" && !slices.Contains(wiped, e.history.schema) {
			wiped = append(wiped, e.history.schema)
		}

		for _, statement := range e.d.Wipe(wiped) {
			if _, err := conn.ExecContext(ctx, statement); err != nil {
				return fmt.Errorf("ant: reset: %w", err)
			}
		}

		return nil
	})
	if err != nil {
		return err
	}

	e.say("Reset database %s.", database)

	return e.up(ctx, "")
}

// seed runs seeders, all of them or those named.
func (e *engine) seed(ctx context.Context, names []string) error {
	for _, name := range names {
		if !slices.ContainsFunc(e.seeders, func(s seeder) bool { return s.name == name }) {
			return fmt.Errorf("ant: no seeder named %q", name)
		}
	}

	if len(e.seeders) == 0 {
		e.say("No seeders are registered.")

		return nil
	}

	if !e.config.SeedHooks {
		ctx = ant.WithoutHooks(ctx)
	}

	for _, s := range e.seeders {
		if len(names) > 0 && !slices.Contains(names, s.name) {
			continue
		}

		tx, err := e.pool.BeginTx(ctx, nil)
		if err != nil {
			return err
		}

		db, err := e.bind(tx)
		if err == nil {
			err = s.run(ctx, db)
		}

		if err != nil {
			return errors.Join(fmt.Errorf("ant: seed %s: %w", s.name, err), tx.Rollback())
		}

		if err := tx.Commit(); err != nil {
			return err
		}

		e.say("Seeded %s", s.name)
	}

	return nil
}

// drift compares the live database with what migrations describe. Both sides are
// read back from the server itself, so a difference is a real one, not a matter of
// how an expression is spelled.
func (e *engine) drift(
	ctx context.Context,
	target *schema.Schema,
	source string,
	showSQL bool,
) error {
	entries, err := e.status(ctx)
	if err != nil {
		return err
	}

	pending := 0

	for _, entry := range entries {
		if entry.Kind == Versioned && (entry.State == Pending || entry.State == Late) {
			pending++
		}
	}

	conn, err := e.pool.Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()

	// Inspect names the default schema "", which the target may name outright.
	target, err = liveschema.Relative(ctx, e.d, conn, target)
	if err != nil {
		return err
	}

	canonical, err := e.d.Canonical(ctx, conn, target)
	if err != nil {
		return err
	}

	live, err := e.d.Inspect(ctx, conn, target.Schemas)
	if err != nil {
		return err
	}

	ignored := slices.Concat(e.config.Ignore, []string{e.history.table})

	if e.history.schema != "" {
		ignored = append(ignored, e.history.schema+"."+e.history.table)
	}

	for _, ref := range target.Unmanaged {
		ignored = append(ignored, ref.String())
	}

	pruned, err := prune(live, ignored)
	if err != nil {
		return err
	}

	expected, err := prune(canonical, e.config.Ignore)
	if err != nil {
		return err
	}

	reconcile(e.d, pruned, expected)

	plan, err := schema.Diff(pruned, expected)
	if err != nil {
		return err
	}

	if pending > 0 {
		e.say(
			"%d migrations are pending, so the database is expected to differ. Run up first.",
			pending,
		)
	}

	if plan.Empty() {
		e.say("No drift: the database matches %s.", source)

		return nil
	}

	e.say("The database differs from %s. To match, it would need to:", source)

	for _, change := range plan.Changes {
		e.say("  %s", change)
	}

	if showSQL {
		schema.Backfill(e.d, plan)

		text, err := schema.Script(e.d, plan)
		if err != nil {
			return err
		}

		e.say("\n%s", text)
	}

	return fmt.Errorf("%w: %d differences", ErrDrift, len(plan.Changes))
}

// reconcile makes live use expected's text wherever the two mean the same, so
// that Diff, which compares text, sees no change.
func reconcile(d schema.Dialect, live, expected *schema.Schema) {
	// A type or sequence no model declares may be in use through a raw column
	// type, so only those the models know about are compared.
	live.Enums = slices.DeleteFunc(
		live.Enums,
		func(e *schema.Enum) bool { return expected.Enum(e.Name) == nil },
	)

	live.Sequences = slices.DeleteFunc(
		live.Sequences,
		func(q *schema.Sequence) bool { return expected.Sequence(q.Name) == nil },
	)

	same := func(have *string, want string) {
		if *have != want && d.SameExpression(*have, want) {
			*have = want
		}
	}

	for _, t := range expected.Tables {
		other := live.Table(t.Ref())
		if other == nil {
			continue
		}

		for _, c := range t.Columns {
			if column := other.Column(c.Name); column != nil {
				same(&column.Default, c.Default)
				same(&column.Generated, c.Generated)
			}
		}

		for _, check := range t.Checks {
			for _, have := range other.Checks {
				if have.Name == check.Name {
					same(&have.Expr, check.Expr)
				}
			}
		}

		for _, index := range t.Indexes {
			for _, have := range other.Indexes {
				if have.Name != index.Name || len(have.Parts) != len(index.Parts) {
					continue
				}

				same(&have.Where, index.Where)

				for i := range index.Parts {
					same(&have.Parts[i].Expr, index.Parts[i].Expr)
				}
			}
		}
	}
}

// prune removes ignored tables and objects from a copy of s.
func prune(s *schema.Schema, patterns []string) (*schema.Schema, error) {
	out := s.Clone()

	var failure error

	matches := func(name string) bool {
		for _, pattern := range patterns {
			ok, err := path.Match(pattern, name)
			if err != nil {
				failure = fmt.Errorf("ant: bad pattern in Config.Ignore: %q: %w", pattern, err)
			}

			if ok {
				return true
			}
		}

		return false
	}

	out.Tables = slices.DeleteFunc(
		out.Tables,
		func(t *schema.Table) bool { return matches(t.Ref().String()) },
	)

	for _, t := range out.Tables {
		object := func(name string) bool { return matches(t.Name + "." + name) }
		t.Columns = slices.DeleteFunc(
			t.Columns,
			func(c *schema.Column) bool { return object(c.Name) },
		)

		t.Uniques = slices.DeleteFunc(t.Uniques, func(k *schema.Key) bool { return object(k.Name) })
		t.Checks = slices.DeleteFunc(t.Checks, func(c *schema.Check) bool { return object(c.Name) })

		t.Indexes = slices.DeleteFunc(
			t.Indexes,
			func(i *schema.Index) bool { return object(i.Name) },
		)

		t.ForeignKeys = slices.DeleteFunc(
			t.ForeignKeys,
			func(k *schema.ForeignKey) bool { return object(k.Name) },
		)
	}

	return out, failure
}

// script writes migrations as one SQL file, for databases ant cannot reach, such
// as a DBA's review queue. Plain leaves out the guards and the history, for use
// with other tools. Otherwise the file can run any number of times, like up.
func script(
	d schema.Dialect,
	h history,
	c *catalog,
	plain bool,
	warn func(string),
) (string, error) {
	var b strings.Builder

	if plain {
		for _, m := range slices.Concat(c.versioned, c.repeatable) {
			if m.run != nil {
				warn("left out " + m.ID() + ", which is a Go migration")
				fmt.Fprintf(&b, "-- %s is a Go migration, and is not in this file.\n\n", m.ID())

				continue
			}

			fmt.Fprintf(&b, "-- %s\n%s\n\n", m.ID(), strings.TrimSpace(m.SQL))
		}

		return b.String(), nil
	}

	b.WriteString(
		"-- Written by ant. Safe to run more than once: each block checks the history table first.\n\n",
	)

	t, err := h.definition()
	if err != nil {
		return "", err
	}

	statements, err := d.EnsureTable(t)
	if err != nil {
		return "", err
	}

	for _, statement := range statements {
		b.WriteString(statement + ";\n\n")
	}

	for _, m := range c.versioned {
		switch {
		case m.run != nil:
			warn("left out " + m.ID() + ", which is a Go migration: run it with up")

			fmt.Fprintf(
				&b,
				"-- %s is a Go migration, and is not in this file. Run it with up.\n\n",
				m.ID(),
			)

			continue
		case m.noTransaction:
			return "", fmt.Errorf(
				"%w: %s cannot run in a transaction, so it cannot be guarded. Use script --plain, or up",
				ErrBadMigration,
				m.ID(),
			)
		}

		condition := fmt.Sprintf(
			"NOT EXISTS (SELECT 1 FROM %s WHERE kind = %s AND version = %s AND success)",
			h.name(),
			literal(string(Versioned)),
			literal(m.key()),
		)
		fmt.Fprintf(&b, "-- %s\n%s;\n\n", m.ID(), d.Guarded(condition, m.SQL, h.insertScript(m)))
	}

	for _, m := range c.repeatable {
		if m.noTransaction {
			return "", fmt.Errorf(
				"%w: %s cannot run in a transaction, so it cannot be guarded. Use script --plain, or up",
				ErrBadMigration,
				m.ID(),
			)
		}

		condition := fmt.Sprintf(
			"NOT EXISTS (SELECT 1 FROM %s WHERE kind = %s AND version = %s AND checksum = %s)",
			h.name(),
			literal(string(Repeatable)),
			literal(m.key()),
			literal(m.Checksum()),
		)

		clear := fmt.Sprintf(
			"DELETE FROM %s WHERE kind = %s AND version = %s",
			h.name(),
			literal(string(Repeatable)),
			literal(m.key()),
		)
		fmt.Fprintf(
			&b,
			"-- %s\n%s;\n\n",
			m.ID(),
			d.Guarded(condition, m.SQL, clear, h.insertScript(m)),
		)
	}

	return b.String(), nil
}
