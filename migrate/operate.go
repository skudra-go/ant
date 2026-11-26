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
		// The migrations first, so that a folder that is not there records
		// nothing.
		c, err := e.catalog(ctx)
		if err != nil {
			return err
		}

		if err := e.history.ensure(ctx, conn); err != nil {
			return err
		}

		rows, err := e.history.read(ctx, conn)
		if err != nil {
			return err
		}

		entries := e.entries(c, rows)

		// A file that is another migration than the one applied is no edit to
		// accept: it never ran here.
		var others []error

		for _, entry := range entries {
			if entry.State == Changed && entry.appliedAs != "" {
				others = append(others, renumbered(entry))
			}
		}

		if len(others) > 0 {
			return errors.Join(others...)
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

		for _, entry := range entries {
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
// version, or with no version the one migration not yet recorded. It is how an
// existing database joins.
func (e *engine) baseline(ctx context.Context, version string) error {
	return e.locked(ctx, func(conn *sql.Conn) error {
		// The migrations first, so that a folder that is not there records
		// nothing.
		c, err := e.catalog(ctx)
		if err != nil {
			return err
		}

		if err := e.history.ensure(ctx, conn); err != nil {
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

		var unrecorded []Migration

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

			unrecorded = append(unrecorded, m)
		}

		// With no version, which migrations the database has is a guess: one
		// adopted after more were written would have them recorded without
		// ever running them (#190). One migration to record is the baseline
		// itself; more need the version the database has reached.
		if version == "" && len(unrecorded) > 1 {
			ids := make([]string, len(unrecorded))

			for i, m := range unrecorded {
				ids[i] = m.ID()
			}

			return fmt.Errorf(
				"%w: baseline would record %s as applied without running them. Name the "+
					"version this database has reached: baseline %s records only the first, "+
					"baseline %s all of them",
				ErrBadMigration,
				strings.Join(ids, ", "),
				unrecorded[0].Version,
				unrecorded[len(unrecorded)-1].Version,
			)
		}

		count := 0

		for _, m := range unrecorded {
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

// pendingNote says how many migrations are still to run, in the singular
// when there is one.
func pendingNote(pending int) string {
	if pending == 1 {
		return "1 migration is pending, so the database is expected to differ. Run up first."
	}

	return fmt.Sprintf(
		"%d migrations are pending, so the database is expected to differ. Run up first.",
		pending,
	)
}

// driftNote says how many differences drift found, in the singular when
// there is one.
func driftNote(differences int) string {
	if differences == 1 {
		return "1 difference"
	}

	return fmt.Sprintf("%d differences", differences)
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
	// Adopting a database runs drift before the first migration exists, so a
	// versions folder that is not there yet has nothing pending.
	entries, err := e.statusFrom(ctx, creating{e.source})
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

	sequences := serialSequenceChanges(pruned, expected)

	if pending > 0 {
		e.say("%s", pendingNote(pending))
	}

	external, err := e.externalTypes(ctx, conn, target)
	if err != nil {
		return err
	}

	views, err := e.viewChanges(ctx, conn, target)
	if err != nil {
		return err
	}

	tables := len(plan.Changes) + len(sequences)

	if tables == 0 && len(views) == 0 {
		e.say("No drift: the database matches %s.", source)
		e.sayExternal(external)

		return nil
	}

	defer e.sayExternal(external)

	if tables > 0 {
		e.say("The database differs from %s. To match, it would need to:", source)
	}

	for _, change := range plan.Changes {
		e.say("  %s", change)
	}

	for _, change := range sequences {
		e.say("  %s", change)
	}

	if len(views) > 0 {
		header := "The views the models read differ from the database:"

		if tables > 0 {
			header = "\n" + header
		}

		e.say("%s", header)

		for _, change := range views {
			e.say("  %s", change)
		}
	}

	if slices.ContainsFunc(plan.Changes, defaultMissing) {
		e.say("\n%s", defaultNote)
	}

	if showSQL && !plan.Empty() {
		schema.Backfill(e.d, plan)

		text, err := schema.Script(e.d, plan)
		if err != nil {
			return err
		}

		e.say("\n%s", text)
	}

	return fmt.Errorf("%w: %s", ErrDrift, driftNote(tables+len(views)))
}

// viewChanges names each view of target that the database lacks, and each
// column a model reads that its view lacks, if the dialect can tell. A view may
// have more columns than the models read. Migrations do not create views, so
// these are for the repeatable migrations that do to fix (#232). A read-only
// model of a table that target has reads what migrations make of it.
func (e *engine) viewChanges(
	ctx context.Context,
	conn *sql.Conn,
	target *schema.Schema,
) ([]string, error) {
	d, ok := e.d.(schema.RelationColumns)
	if !ok {
		return nil, nil
	}

	kept, err := prune(target, e.config.Ignore)
	if err != nil {
		return nil, err
	}

	var out []string

	for _, v := range kept.Views {
		var (
			columns []string
			found   bool
		)

		if t := kept.Table(v.Ref()); t != nil {
			found = true

			for _, c := range t.Columns {
				columns = append(columns, c.Name)
			}
		} else if columns, found, err = d.RelationColumns(ctx, conn, v.Ref()); err != nil {
			return nil, err
		}

		if !found {
			out = append(out, fmt.Sprintf("view %s is missing", v.Ref()))

			continue
		}

		for _, column := range v.Columns {
			if !slices.Contains(columns, column) {
				out = append(out, fmt.Sprintf("view %s has no column %s", v.Ref(), column))
			}
		}
	}

	return out, nil
}

// externalTypes returns the types that target's columns use and target does not
// create, if the dialect can tell, leaving out the tables Config.Ignore names.
func (e *engine) externalTypes(
	ctx context.Context,
	conn *sql.Conn,
	target *schema.Schema,
) ([]schema.ExternalType, error) {
	d, ok := e.d.(schema.ExternalTypes)
	if !ok {
		return nil, nil
	}

	kept, err := prune(target, e.config.Ignore)
	if err != nil {
		return nil, err
	}

	return d.ExternalTypes(ctx, conn, kept)
}

// sayExternal names the types the models use and do not create. The database
// has them, so they are no drift, but an empty one does not, and a migration
// made from the models alone fails there (#193).
func (e *engine) sayExternal(types []schema.ExternalType) {
	if len(types) == 0 {
		return
	}

	e.say("\nThe models name types they do not create, which an empty database lacks:")

	for _, t := range types {
		what := t.Kind

		if t.Extension != "" {
			what = "from extension " + t.Extension
		}

		e.say("  %s, %s: %s", t.Name, what, strings.Join(t.Columns, ", "))
	}

	e.say(
		"A migration made from the models alone fails without them: create them in one of " +
			"your own (diff --empty) before the tables that use them, or adopt the database's dump.",
	)
}

// serialSequenceChanges reports different explicit serial defaults separately
// from migration plans, whose identity compatibility leaves them unchanged.
func serialSequenceChanges(live, expected *schema.Schema) []string {
	var changes []string

	for _, table := range expected.Tables {
		current := live.Table(table.Ref())
		if current == nil {
			continue
		}

		for _, column := range table.Columns {
			other := current.Column(column.Name)
			if other == nil || !other.Identity || !column.Identity ||
				!strings.HasPrefix(other.Default, "nextval(") ||
				!strings.HasPrefix(column.Default, "nextval(") || other.Default == column.Default {
				continue
			}

			changes = append(
				changes,
				fmt.Sprintf(
					"change column %s.%s: default %s to %s",
					table.Ref(),
					column.Name,
					other.Default,
					column.Default,
				),
			)
		}
	}

	return changes
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

	out.Views = slices.DeleteFunc(
		out.Views,
		func(v *schema.View) bool { return matches(v.Ref().String()) },
	)

	for _, v := range out.Views {
		v.Columns = slices.DeleteFunc(
			v.Columns,
			func(column string) bool { return matches(v.Name + "." + column) },
		)
	}

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
// with other tools. Otherwise the file can run any number of times, like up, and
// it stops before a Go migration, which only up can run: going on would apply
// later migrations first, and up would then refuse the Go one as older than an
// applied one (#183). With from, the versioned migrations before that version
// are left out, as applied.
func script(
	d schema.Dialect,
	h history,
	c *catalog,
	plain bool,
	from string,
	warn func(string),
) (string, error) {
	var b strings.Builder

	if from != "" && !slices.ContainsFunc(
		c.versioned,
		func(m Migration) bool { return compareVersions(m.Version, from) == 0 },
	) {
		return "", fmt.Errorf("%w: no migration has version %s", ErrBadMigration, from)
	}

	versioned := slices.DeleteFunc(slices.Clone(c.versioned), func(m Migration) bool {
		return from != "" && compareVersions(m.Version, from) < 0
	})

	if plain {
		for _, m := range slices.Concat(versioned, c.repeatable) {
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

	for i, m := range versioned {
		switch {
		case m.run != nil:
			// The repeatable files run after every versioned one, so they wait
			// too. Up --to leaves them out, so after the last versioned
			// migration plain up runs the rest.
			next := "run it with up"

			if i+1 < len(versioned) {
				next += " --to " + m.Version + ", then script --from " +
					versioned[i+1].Version + " for the rest"
			}

			warn("stopped before " + m.ID() + ", which is a Go migration: " + next)

			fmt.Fprintf(
				&b,
				"-- Stopped before %s, a Go migration, which only up can run: %s.\n",
				m.ID(),
				next,
			)

			return b.String(), nil
		case m.noTransaction:
			return "", noTransactionScript(m)
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
			return "", noTransactionScript(m)
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

// noTransactionScript refuses m in a guarded script: a guard is a block that runs
// in one transaction.
func noTransactionScript(m Migration) error {
	if m.Kind == Repeatable {
		return fmt.Errorf(
			"%w: %s cannot run in a transaction, so it cannot be guarded. Use script --plain, or up",
			ErrBadMigration,
			m.ID(),
		)
	}

	return fmt.Errorf(
		"%w: %s cannot run in a transaction, so it cannot be guarded. Use script --plain, or up; "+
			"once it has run, script --from the version after it leaves it out",
		ErrBadMigration,
		m.ID(),
	)
}

// defaultNote goes with a default the database has and the models lack. A
// default in a model also says that a zero value is left to the database on
// insert, so adding one where the zero must be stored would lose it (#192).
const defaultNote = "A column with a default in the database has none in the models. " +
	"A default in a model also leaves a zero value to the database on insert, so a bool or " +
	"number field with one never inserts false or 0. Where the zero must be stored, make the " +
	"field a pointer with .NotNull() and .Default(...): nil takes the default."

// defaultMissing reports whether change gives a column the default the models
// have, where the database has one they lack.
func defaultMissing(change schema.Change) bool {
	c, ok := change.(schema.AlterColumn)

	return ok && c.From.Default != "" && c.To.Default == ""
}
