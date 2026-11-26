package migrate

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"slices"
	"strings"
	"syscall"
	"text/tabwriter"

	"github.com/skudra-go/ant"
	"github.com/skudra-go/ant/dialect"
	"github.com/skudra-go/ant/schema"
)

const usage = `Keep the database in step with your models.

Writing migrations (no database needed):
  diff <name>            write a migration for what changed in the models
    --empty                write an empty migration to fill by hand
    --rename a.b=c         treat a change as a rename, repeatable: users.name=title,
                           or an enum value: status.banned=blocked
    --dry-run              print the SQL and write nothing
  check                  fail if the models have changes with no migration, for CI
  snapshot               accept the models as they are, with no migration
  adopt <dump.sql>       start from an existing schema: the dump becomes the first migration
  squash <name>          replace all migrations with one baseline
    --keep                 join the old files, instead of writing from the models
  schema-dump            print the whole schema as SQL
    --snapshot             use the snapshot, instead of the models
  inspect [table]        list the tables and columns the models describe
  script                 print all migrations as one idempotent SQL file, stopping
                           before a Go migration, which only up can run
    --plain                no guards or history, for use with other tools
    --from <version>       leave out the migrations before this version, as applied

Running them (needs a database):
  up                     apply pending migrations, then changed repeatable ones
    --to <version>         stop after this version, which must exist
    --dry-run              print what would run, and change nothing
  status                 list every migration and whether it has run
    --check                fail if any is waiting, or up would refuse to run
  drift                  compare the live database with the migrations, and the
                           views that view models read
    --sql                  also print SQL that would close the gap
  db-show                summarize the live database
  baseline [version]     record migrations as applied without running them, through
                           version; with none, only if one is left to record
  repair                 accept changed files, clear failed runs, forget missing files
  seed [name...]         run data seeders
  reset                  delete everything in the database and apply all migrations
    --confirm <database>   skip the prompt, by naming the database
    --seed                 run seeders afterwards

Flags for commands that need a database:
  --dsn <dsn>            connection string; the default is $DATABASE_URL
`

// Main is the CLI. Call it from a main function in your module, and run that with
// go run ./cmd/migrate, or build it for deployment. It exits with status 1 on
// failure. The application imports the database driver as usual.
func Main[DB any](d dialect.Dialect, config ...Config) {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	err := Run[DB](ctx, d, os.Args[1:], config...)

	stop()

	switch {
	case errors.Is(err, flag.ErrHelp):
		fmt.Print(usage)
	case err != nil:
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}
}

// Run is Main without the exit, for embedding the commands in a larger program.
func Run[DB any](ctx context.Context, d dialect.Dialect, args []string, config ...Config) error {
	c := single(config)
	if c.Out == nil {
		c.Out = os.Stdout
	}

	ddl, err := schema.Of(d)
	if err != nil {
		return err
	}

	c = c.resolve(ddl.Name())

	if len(args) == 0 || args[0] == "help" || args[0] == "-h" || args[0] == "--help" {
		return flag.ErrHelp
	}

	source, err := c.source()
	if err != nil {
		return err
	}

	t := &tool{
		d:          ddl,
		config:     c,
		source:     source,
		registered: registeredMigrations[DB](),
		models:     func() (*schema.Schema, error) { return ant.SchemaOf[DB](d, c.Options...) },
		ask:        terminalQuestions(os.Stdin, c.Out),
	}

	command, rest := args[0], args[1:]
	flags := newFlags(command)

	var (
		renames            repeated
		dsn, to, from      string
		confirmName        string
		empty, dry, keep   bool
		plain, showSQL     bool
		fromSnapshot, seed bool
		checkStatus        bool
	)
	flags.StringVar(&dsn, "dsn", "", "")

	switch command {
	case "diff":
		flags.BoolVar(&empty, "empty", false, "")
		flags.BoolVar(&dry, "dry-run", false, "")
		flags.Var(&renames, "rename", "")
	case "up":
		flags.StringVar(&to, "to", "", "")
		flags.BoolVar(&dry, "dry-run", false, "")
	case "status":
		flags.BoolVar(&checkStatus, "check", false, "")
	case "drift":
		flags.BoolVar(&showSQL, "sql", false, "")
	case "squash":
		flags.BoolVar(&keep, "keep", false, "")
	case "script":
		flags.BoolVar(&plain, "plain", false, "")
		flags.StringVar(&from, "from", "", "")
	case "schema-dump":
		flags.BoolVar(&fromSnapshot, "snapshot", false, "")
	case "reset":
		flags.StringVar(&confirmName, "confirm", "", "")
		flags.BoolVar(&seed, "seed", false, "")
	}

	positional, err := parseFlags(flags, rest)
	if err != nil {
		return err
	}

	name := ""

	if len(positional) > 0 {
		name = positional[0]
	}

	if len(positional) > 1 && command != "seed" {
		return fmt.Errorf("%s takes one name, and got: %s", command, strings.Join(positional, " "))
	}

	// A folder is found from the working directory, so the commands that
	// compare or write migrations say which they read. SQL meant for a pipe
	// stays alone.
	if command == "check" || command == "status" || command == "diff" && !dry {
		fmt.Fprintln(c.Out, "Migrations:", c.describe(source))
	}

	switch command {
	case "diff":
		if name == "" {
			return errors.New("diff needs a name, such as: diff add_bio")
		}

		return t.diff(ctx, name, renames, empty, dry)
	case "check":
		return t.check(ctx)
	case "snapshot":
		return t.acceptModels(ctx)
	case "adopt":
		if name == "" {
			return errors.New("adopt needs a schema dump, such as: adopt schema.sql")
		}

		return t.adopt(ctx, name, "baseline")
	case "squash":
		if name == "" {
			name = "baseline"
		}

		return t.squash(ctx, name, keep)
	case "schema-dump":
		text, err := t.dump(ctx, fromSnapshot)
		if err != nil {
			return err
		}

		fmt.Fprint(c.Out, text)

		return nil
	case "inspect":
		return t.inspect(name)
	case "script":
		cat, err := t.catalog(ctx)
		if err != nil {
			return err
		}

		text, err := script(
			ddl,
			history{ddl, c.HistorySchema, c.HistoryTable},
			cat,
			plain,
			from,
			func(warning string) { fmt.Fprintln(os.Stderr, "Warning:", warning) },
		)
		if err != nil {
			return err
		}

		fmt.Fprint(c.Out, text)

		return nil
	case "up", "status", "drift", "baseline", "repair", "seed", "reset", "db-show":
	default:
		return fmt.Errorf("unknown command %q. Run help for the list", command)
	}

	if dsn == "" {
		dsn = os.Getenv("DATABASE_URL")
	}

	if dsn == "" {
		dsn = c.DSN
	}

	if dsn == "" {
		return fmt.Errorf("%s needs a database: pass --dsn, or set DATABASE_URL", command)
	}

	options := append(slices.Clip(c.Options), ant.WithTracking(false))

	db, err := ant.Open[DB](ctx, d, dsn, options...)
	if err != nil {
		return fmt.Errorf("connect: %w", err)
	}
	defer any(db).(interface{ Close() error }).Close()

	e, err := newEngine(db, c)
	if err != nil {
		return err
	}

	switch command {
	case "up":
		if err := e.knows(ctx, to); err != nil {
			return err
		}

		if dry {
			return e.preview(ctx, to)
		}

		return e.up(ctx, to)
	case "status":
		return e.printStatus(ctx, checkStatus)
	case "drift":
		target, label, err := t.expected(ctx)
		if err != nil {
			return err
		}

		return e.drift(ctx, target, label, showSQL)
	case "baseline":
		return e.baseline(ctx, name)
	case "repair":
		return e.repair(ctx)
	case "seed":
		return e.seed(ctx, positional)
	case "db-show":
		return e.show(ctx)
	}

	target, err := t.models()
	if err != nil {
		return err
	}

	err = e.reset(ctx, target.Schemas, confirmReset(confirmName, os.Stdin, c.Out))
	if err == nil && seed {
		return e.seed(ctx, nil)
	}

	return err
}

// expected is what the database should hold: the snapshot, since that is what
// migrations produce, or the models if nothing was generated yet. Migrations do
// not create views, so the views always come from the models (#232).
func (t *tool) expected(ctx context.Context) (*schema.Schema, string, error) {
	snapshot, err := t.snapshot(ctx)
	if err != nil {
		return nil, "", err
	}

	target, err := t.models()
	if err != nil {
		return nil, "", err
	}

	if snapshot == nil {
		return target, "the models", nil
	}

	snapshot.Views = target.Views

	return snapshot, "the snapshot", nil
}

// terminalQuestions returns a way to ask a person a question, or nil in CI and
// other places with nobody at the keyboard.
func terminalQuestions(in *os.File, out io.Writer) func(string) bool {
	if !interactive(in) {
		return nil
	}

	reader := bufio.NewReader(in)

	return func(question string) bool {
		fmt.Fprintf(out, "%s [y/N] ", question)

		line, err := reader.ReadString('\n')
		answer := strings.ToLower(strings.TrimSpace(line))

		return err == nil && (answer == "y" || answer == "yes")
	}
}

// interactive reports whether in is a terminal. The null device is a character
// device too, and it is what stdin usually is in CI and containers.
func interactive(in *os.File) bool {
	info, err := in.Stat()
	if err != nil || info.Mode()&os.ModeCharDevice == 0 {
		return false
	}

	null, err := os.Stat(os.DevNull)

	return err != nil || !os.SameFile(info, null)
}

// confirmReset asks for the database's name, which is hard to type by accident,
// and proves the operator knows which database this is.
func confirmReset(given string, in *os.File, out io.Writer) func(string) error {
	return func(database string) error {
		if given == database {
			return nil
		}

		if given != "" {
			return fmt.Errorf("%w: the database is %s, not %s", ErrUnconfirmed, database, given)
		}

		if !interactive(in) {
			return fmt.Errorf("%w: pass --confirm %s", ErrUnconfirmed, database)
		}

		fmt.Fprintf(
			out,
			"This deletes everything in database %s. Type its name to continue: ",
			database,
		)

		line, err := bufio.NewReader(in).ReadString('\n')
		if err != nil && !errors.Is(err, io.EOF) {
			return err
		}

		if strings.TrimSpace(line) != database {
			return fmt.Errorf("%w: names do not match", ErrUnconfirmed)
		}

		return nil
	}
}

// preview prints what up would do.
// knows refuses an up --to version that no migration has. Compared with each
// version instead, a typo such as v2 sorted after every numbered one, so every
// migration ran (#185). A version matches without its leading zeros, as it does
// when compared.
func (e *engine) knows(ctx context.Context, to string) error {
	if to == "" {
		return nil
	}

	c, err := e.catalog(ctx)
	if err != nil {
		return err
	}

	if !slices.ContainsFunc(
		c.versioned,
		func(m Migration) bool { return compareVersions(m.Version, to) == 0 },
	) {
		return fmt.Errorf("%w: no migration has version %s", ErrBadMigration, to)
	}

	return nil
}

func (e *engine) preview(ctx context.Context, to string) error {
	entries, err := e.status(ctx)
	if err != nil {
		return err
	}

	count := 0
	versioned := false

	for _, entry := range entries {
		m := entry.migration
		if m == nil || !within(*m, to) || (!runs(entry) && !rerunsAfter(entry, versioned)) {
			continue
		}

		count++
		versioned = versioned || m.Kind == Versioned

		e.say("-- %s", m.ID())

		if m.run != nil {
			e.say("-- A Go migration.\n")

			continue
		}

		e.say("%s\n", strings.TrimSpace(m.SQL))
	}

	if count == 0 {
		e.say("Nothing to apply: the database is up to date.")
	}

	return nil
}

// show prints a summary of the live database.
func (e *engine) show(ctx context.Context) error {
	summary, err := e.d.Summary(ctx, e.pool)
	if err != nil {
		return err
	}

	e.say("%s on %s %s, %s", summary.Name, e.d.Name(), summary.Version, summary.Size)
	w := tabwriter.NewWriter(e.config.Out, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "TABLE\tROWS (ESTIMATE)\tSIZE")

	for _, t := range summary.Tables {
		fmt.Fprintf(w, "%s\t%d\t%s\n", t.Name, t.Rows, t.Size)
	}

	if len(summary.MaterializedViews) > 0 {
		fmt.Fprintln(w, "\nMATERIALIZED VIEW\tROWS (ESTIMATE)\tSIZE")

		for _, v := range summary.MaterializedViews {
			fmt.Fprintf(w, "%s\t%d\t%s\n", v.Name, v.Rows, v.Size)
		}
	}

	return w.Flush()
}

// printStatus lists the migrations. With check, it fails if any is waiting or
// up would refuse to run, which a deploy gate needs: status said "changed" and
// still exited 0, and nothing returned ErrPending (#188).
func (e *engine) printStatus(ctx context.Context, check bool) error {
	entries, err := e.status(ctx)
	if err != nil {
		return err
	}

	if len(entries) == 0 {
		e.say("No migrations yet.")

		return nil
	}

	w := tabwriter.NewWriter(e.config.Out, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "MIGRATION\tSTATE\tAPPLIED")

	waiting := 0

	for _, entry := range entries {
		when := ""

		if !entry.AppliedAt.IsZero() {
			when = entry.AppliedAt.UTC().Format("2006-01-02 15:04:05 UTC")
		}

		fmt.Fprintf(w, "%s\t%s\t%s\n", entry.ID, entry.State, when)

		if slices.Contains([]State{Pending, Late, Outdated, Absorbed}, entry.State) {
			waiting++
		}
	}

	if err := w.Flush(); err != nil {
		return err
	}

	if waiting > 0 {
		e.say("\n%d waiting. Apply with: up", waiting)
	}

	if !check {
		return nil
	}

	rows, err := e.history.read(ctx, e.pool)
	if err != nil {
		return err
	}

	var problems []error

	if waiting > 0 {
		problems = append(problems, fmt.Errorf("%w: %d waiting", ErrPending, waiting))
	}

	return errors.Join(append(problems, e.validate(entries, rows))...)
}

type repeated []string

func (r *repeated) String() string { return strings.Join(*r, ",") }

func (r *repeated) Set(value string) error {
	*r = append(*r, value)

	return nil
}

func newFlags(command string) *flag.FlagSet {
	flags := flag.NewFlagSet(command, flag.ContinueOnError)
	flags.SetOutput(io.Discard)

	return flags
}

// parseFlags lets flags and names come in any order, as in diff add_bio --empty.
func parseFlags(flags *flag.FlagSet, args []string) ([]string, error) {
	var positional []string

	for {
		if err := flags.Parse(args); err != nil {
			if errors.Is(err, flag.ErrHelp) {
				return nil, err
			}

			return nil, fmt.Errorf("%s: %w", flags.Name(), err)
		}

		if flags.NArg() == 0 {
			return positional, nil
		}

		positional = append(positional, flags.Arg(0))
		args = flags.Args()[1:]
	}
}
