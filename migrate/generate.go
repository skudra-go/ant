package migrate

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/skudra-go/ant/schema"
)

// tool runs the commands that need no database.
type tool struct {
	d          schema.Dialect
	config     Config
	source     Source
	registered []Migration
	models     func() (*schema.Schema, error)
	// ask puts a yes or no question to a person, if one is there.
	ask func(question string) bool
}

func (t *tool) say(format string, args ...any) { fmt.Fprintf(t.config.Out, format+"\n", args...) }

func (t *tool) catalog(ctx context.Context) (*catalog, error) {
	return load(ctx, t.source, t.registered)
}

// snapshot returns what the last migration left, or nil if nothing was generated.
func (t *tool) snapshot(ctx context.Context) (*schema.Schema, error) {
	s, err := readSnapshot(ctx, t.source)
	if err != nil {
		return nil, err
	}

	if s != nil && s.Dialect != "" && s.Dialect != t.d.Name() {
		return nil, fmt.Errorf(
			"ant: snapshot is for %s and this is %s: set Config.PerDialect to keep them apart",
			s.Dialect,
			t.d.Name(),
		)
	}

	return s, nil
}

// plan compares the snapshot with the models.
func (t *tool) plan(ctx context.Context, renames []string) (*schema.Plan, *schema.Schema, error) {
	old, err := t.snapshot(ctx)
	if err != nil {
		return nil, nil, err
	}

	target, err := t.models()
	if err != nil {
		return nil, nil, err
	}

	plan, err := schema.Diff(old, target, renames...)
	if err != nil {
		return nil, nil, err
	}

	schema.Backfill(t.d, plan)

	return plan, target, nil
}

// compose names a new migration.
func (t *tool) compose(c *catalog, name, text string) (Migration, error) {
	cleaned := slug(name)
	if cleaned == "" {
		return Migration{}, fmt.Errorf(
			"%w: give the migration a name, such as add_bio",
			ErrBadMigration,
		)
	}

	file := t.config.Name(Next{Version: nextVersion(c.versioned), Name: cleaned, Time: time.Now()})

	version, label, ok := t.config.Parse(file)
	if !ok {
		return Migration{}, fmt.Errorf(
			"%w: Config.Name made %q, which Config.Parse cannot read",
			ErrBadMigration,
			file,
		)
	}

	return Migration{
		Kind:    Versioned,
		Version: version,
		Name:    label,
		File:    file,
		SQL:     text,
	}, nil
}

// diff writes a migration for what changed in the models, and moves the snapshot.
func (t *tool) diff(ctx context.Context, name string, renames []string, empty, dry bool) error {
	store, err := t.config.store()
	if err != nil {
		return err
	}

	c, err := t.catalog(ctx)
	if err != nil {
		return err
	}

	if empty {
		return t.writeEmpty(ctx, store, c, name, dry)
	}

	plan, target, err := t.plan(ctx, renames)
	if err != nil {
		return err
	}

	if plan.Empty() {
		t.say("No changes: the models match the snapshot.")

		return nil
	}

	if plan, target, err = t.resolveHints(ctx, plan, target, renames); err != nil {
		return err
	}

	text, err := t.config.Render(plan, t.d)
	if err != nil {
		return err
	}

	m, err := t.compose(c, name, text)
	if err != nil {
		return err
	}

	if dry {
		fmt.Fprint(t.config.Out, text)

		return nil
	}

	target.Latest = m.ID()

	data, err := target.Encode()
	if err != nil {
		return err
	}

	if err := store.Save(ctx, m); err != nil {
		return err
	}

	if err := store.SaveSnapshot(ctx, data); err != nil {
		return err
	}

	t.say("Wrote %s", t.location(m))

	for _, change := range plan.Changes {
		t.say("  %s", change)
	}

	for _, warning := range plan.Warnings() {
		t.say("Warning: %s", warning)
	}

	for _, hint := range plan.Hints {
		t.say("Possible rename: %s. The file says how to keep the data.", hint)
	}

	t.say("Review it, then apply it with: up")

	return nil
}

// resolveHints asks about each possible rename, when someone is there to answer.
// A rename can use up the columns of other hints, so each answer means a new plan.
func (t *tool) resolveHints(
	ctx context.Context,
	plan *schema.Plan,
	target *schema.Schema,
	renames []string,
) (*schema.Plan, *schema.Schema, error) {
	declined := map[string]bool{}

	for {
		var hint *schema.Hint

		for i := range plan.Hints {
			if !declined[plan.Hints[i].Flag()] {
				hint = &plan.Hints[i]

				break
			}
		}

		if hint == nil || t.ask == nil {
			return plan, target, nil
		}

		if !t.ask(fmt.Sprintf("%s. Is this a rename?", *hint)) {
			declined[hint.Flag()] = true

			continue
		}

		renames = append(renames, hint.Flag())

		var err error
		if plan, target, err = t.plan(ctx, renames); err != nil {
			return nil, nil, err
		}
	}
}

func (t *tool) location(m Migration) string {
	return path.Join(t.config.Dir, t.config.VersionsDir, m.File)
}

// writeEmpty adds a file to fill by hand. Models and snapshot stay as they are.
func (t *tool) writeEmpty(
	ctx context.Context,
	store Store,
	c *catalog,
	name string,
	dry bool,
) error {
	text := "-- Written by hand. Plain SQL, run in one transaction.\n-- Add -- ant:no-transaction above for statements that cannot run in one.\n\n"

	m, err := t.compose(c, name, text)
	if err != nil {
		return err
	}

	if dry {
		fmt.Fprint(t.config.Out, text)

		return nil
	}

	if err := store.Save(ctx, m); err != nil {
		return err
	}

	old, err := t.snapshot(ctx)
	if err != nil {
		return err
	}

	if old == nil {
		old = &schema.Schema{Format: schema.Format, Dialect: t.d.Name()}
	}

	old.Latest = m.ID()

	data, err := old.Encode()
	if err != nil {
		return err
	}

	if err := store.SaveSnapshot(ctx, data); err != nil {
		return err
	}

	t.say("Wrote %s", t.location(m))

	return nil
}

// acceptModels records the models as the snapshot without writing a migration,
// for changes made by hand and for resolving a merge.
func (t *tool) acceptModels(ctx context.Context) error {
	store, err := t.config.store()
	if err != nil {
		return err
	}

	c, err := t.catalog(ctx)
	if err != nil {
		return err
	}

	target, err := t.models()
	if err != nil {
		return err
	}

	if last := c.lastFile(); last != nil {
		target.Latest = last.ID()
	}

	data, err := target.Encode()
	if err != nil {
		return err
	}

	if err := store.SaveSnapshot(ctx, data); err != nil {
		return err
	}

	t.say("The snapshot now matches the models, and no migration was written.")

	return nil
}

// adopt starts migrations on an existing project: a dump of the schema becomes
// the first migration, so new databases get everything, including what models do
// not describe, and the models become the snapshot.
func (t *tool) adopt(ctx context.Context, file, name string) error {
	store, err := t.config.store()
	if err != nil {
		return err
	}

	c, err := t.catalog(ctx)
	if err != nil {
		return err
	}

	if len(c.versioned) > 0 {
		return fmt.Errorf(
			"%w: this project already has migrations. Use baseline to record them as applied",
			ErrBadMigration,
		)
	}

	data, err := os.ReadFile(file)
	if err != nil {
		return err
	}

	text := fmt.Sprintf(
		"-- The schema as it was when this project adopted ant, from %s.\n\n%s",
		schema.Comment(filepath.Base(file)),
		withoutMetaCommands(string(data)),
	)

	m, err := t.compose(c, name, text)
	if err != nil {
		return err
	}

	target, err := t.models()
	if err != nil {
		return err
	}

	target.Latest = m.ID()

	snapshot, err := target.Encode()
	if err != nil {
		return err
	}

	if err := store.Save(ctx, m); err != nil {
		return err
	}

	if err := store.SaveSnapshot(ctx, snapshot); err != nil {
		return err
	}

	t.say("Wrote %s, and the snapshot from your models.", t.location(m))

	t.say(
		"New databases run that file. On each database that already has this schema, run: baseline",
	)

	return nil
}

// withoutMetaCommands removes psql commands, such as the \\restrict lines recent
// versions of pg_dump write, which only psql understands.
func withoutMetaCommands(text string) string {
	var b strings.Builder

	for line := range strings.Lines(text) {
		if !strings.HasPrefix(line, "\\") {
			b.WriteString(line)
		}
	}

	return b.String()
}

// check fails if the migration files and models are not in step. It needs no
// database, so it suits CI.
func (t *tool) check(ctx context.Context) error {
	c, err := t.catalog(ctx)
	if err != nil {
		return err
	}

	old, err := t.snapshot(ctx)
	if err != nil {
		return err
	}

	if last := c.lastFile(); last != nil && old != nil && old.Latest != "" &&
		old.Latest != last.ID() {
		return fmt.Errorf(
			"%w: it was written for %s, and the newest file is %s. After a merge, renumber if needed, then run snapshot",
			ErrStaleSnapshot,
			old.Latest,
			last.ID(),
		)
	}

	plan, _, err := t.plan(ctx, nil)
	if err != nil {
		return err
	}

	if !plan.Empty() {
		lines := make([]string, len(plan.Changes))

		for i, change := range plan.Changes {
			lines[i] = "  " + change.String()
		}

		return fmt.Errorf(
			"%w. Run diff to write them:\n%s",
			ErrPendingChanges,
			strings.Join(lines, "\n"),
		)
	}

	t.say("OK: models, snapshot, and migrations agree.")

	return nil
}

// inspect prints what the models describe, all of it or one table.
func (t *tool) inspect(only string) error {
	target, err := t.models()
	if err != nil {
		return err
	}

	found := false

	for _, table := range target.Tables {
		if only != "" && table.Name != only && table.Ref().String() != only {
			continue
		}

		found = true

		t.say("%s", table.Ref())

		for _, c := range table.Columns {
			line := "  " + c.Name + " " + c.Type

			if !c.Nullable {
				line += " NOT NULL"
			}

			if c.Identity {
				line += " GENERATED"
			}

			if c.Default != "" {
				line += " DEFAULT " + c.Default
			}

			t.say("%s", line)
		}

		if k := table.PrimaryKey; k != nil {
			t.say("  primary key (%s)", strings.Join(k.Columns, ", "))
		}

		for _, k := range table.Uniques {
			t.say("  unique (%s)", strings.Join(k.Columns, ", "))
		}

		for _, k := range table.ForeignKeys {
			t.say(
				"  foreign key (%s) to %s (%s)",
				strings.Join(k.Columns, ", "),
				k.RefTable,
				strings.Join(k.RefColumns, ", "),
			)
		}

		for _, i := range table.Indexes {
			t.say("  index %s", i.Name)
		}

		for _, c := range table.Checks {
			t.say("  check %s: %s", c.Name, c.Expr)
		}
	}

	if only != "" && !found {
		return fmt.Errorf("no table %s in the models", only)
	}

	return nil
}

// dump writes the whole schema as SQL.
func (t *tool) dump(ctx context.Context, fromSnapshot bool) (string, error) {
	var (
		target *schema.Schema
		err    error
	)

	if fromSnapshot {
		if target, err = t.snapshot(ctx); err == nil && target == nil {
			return "", errors.New("ant: no snapshot yet: run diff first")
		}
	} else {
		target, err = t.models()
	}

	if err != nil {
		return "", err
	}

	plan, err := schema.Diff(nil, target)
	if err != nil {
		return "", err
	}

	return t.config.Render(plan, t.d)
}

// squash replaces every versioned migration with one baseline. Databases that
// already have them absorb it, and fresh ones run it, so nothing needs
// coordinating between environments.
func (t *tool) squash(ctx context.Context, name string, keep bool) error {
	store, err := t.config.store()
	if err != nil {
		return err
	}

	c, err := t.catalog(ctx)
	if err != nil {
		return err
	}

	last := c.lastFile()
	if last == nil {
		return fmt.Errorf("%w: nothing to squash", ErrBadMigration)
	}

	if err := t.check(ctx); err != nil {
		return err
	}

	var body strings.Builder

	if keep {
		// A directive only counts at the top, so one from inside moves up.
		if slices.ContainsFunc(c.versioned, func(m Migration) bool { return m.noTransaction }) {
			body.WriteString(schema.NoTransaction + "\n\n")
		}

		for _, m := range c.versioned {
			if m.run == nil {
				fmt.Fprintf(&body, "-- From %s\n%s\n\n", m.ID(), strings.TrimSpace(m.SQL))
			}
		}
	} else {
		text, err := t.dump(ctx, true)
		if err != nil {
			return err
		}

		body.WriteString(text)
	}

	text := fmt.Sprintf(
		"-- %s %s\n-- Replaces every migration through %s. Databases that have them record this file\n-- without running it, and new databases run it.\n\n%s",
		replacesDirective,
		last.Version,
		last.Version,
		body.String(),
	)

	m, err := t.compose(c, name, text)
	if err != nil {
		return err
	}

	if err := store.Save(ctx, m); err != nil {
		return err
	}

	for _, old := range c.versioned {
		if old.run == nil {
			if err := store.Remove(ctx, old); err != nil {
				return err
			}
		}
	}

	snapshot, err := t.snapshot(ctx)
	if err != nil {
		return err
	}

	snapshot.Latest = m.ID()

	data, err := snapshot.Encode()
	if err != nil {
		return err
	}

	if err := store.SaveSnapshot(ctx, data); err != nil {
		return err
	}

	t.say(
		"Wrote %s, replacing %d migrations through %s.",
		t.location(m),
		countFiles(c),
		last.Version,
	)

	if !keep {
		t.say(
			"Warning: a squash keeps what the models describe. Anything written by hand, such as extensions, types, triggers, or data, is gone from this file. Add it back, or squash with --keep to join the old files instead.",
		)
	}

	return nil
}

func countFiles(c *catalog) int {
	return len(
		slices.DeleteFunc(
			slices.Clone(c.versioned),
			func(m Migration) bool { return m.run != nil },
		),
	)
}
