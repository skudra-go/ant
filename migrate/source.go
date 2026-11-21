package migrate

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"

	"github.com/skudra-go/ant/schema"
)

const snapshotFile = "schema.snapshot.json"

// Source is where migrations come from. The default reads two folders, and any
// other storage works by implementing this, such as a database or object store.
type Source interface {
	// List returns every migration, in any order. Kind, Version, Name, and SQL
	// must be set, and File identifies a repeatable one.
	List(ctx context.Context) ([]Migration, error)
	// Snapshot returns the stored schema snapshot, or nil if there is none.
	Snapshot(ctx context.Context) ([]byte, error)
}

// Store is a Source that can also be written, which generating commands need.
type Store interface {
	Source
	// Save stores a new migration.
	Save(ctx context.Context, m Migration) error
	// Remove deletes a migration, when squashing.
	Remove(ctx context.Context, m Migration) error
	// SaveSnapshot replaces the stored snapshot.
	SaveSnapshot(ctx context.Context, data []byte) error
}

// folders is the default Source: versions and code folders in a file system.
type folders struct {
	fsys     fs.FS
	versions string
	code     string
	parse    func(string) (string, string, bool)
	dir      string // the folder on disk fsys reads, or "" for another file system
}

// FS returns a read-only Source over a file system, such as an embed.FS, that
// holds versions and code folders at its root.
func FS(fsys fs.FS) Source {
	return folders{fsys, "versions", "code", parseFile, ""}
}

// Dir returns a Store over a folder on disk that holds versions and code folders.
func Dir(path string) Store {
	return disk{folders{os.DirFS(path), "versions", "code", parseFile, path}, path}
}

// noFolderError is a versions folder that is not there. Only the commands that
// write the first migration take it for an empty one, since for any other the
// likely cause is a program run from another folder, or built without its
// migrations, which would otherwise find nothing to apply and succeed (#176).
type noFolderError struct{ at, hint string }

func (e *noFolderError) Error() string {
	return "ant: no migrations folder at " + e.at + e.hint
}

// Unwrap makes errors.Is(err, fs.ErrNotExist) true.
func (e *noFolderError) Unwrap() error { return fs.ErrNotExist }

// noFolder says where the versions folder was looked for.
func (f folders) noFolder() error {
	if f.dir == "" {
		return &noFolderError{
			f.versions + " in the file system",
			`; with //go:embed migrations, set Config.Dir to "migrations"`,
		}
	}

	cwd, err := os.Getwd()
	if err != nil {
		cwd = err.Error()
	}

	return &noFolderError{
		filepath.Join(f.dir, f.versions),
		fmt.Sprintf(
			" (working directory %s); run it from the project's folder, or set Config.Dir",
			cwd,
		),
	}
}

// creating is a Source whose missing versions folder is an empty one, for the
// commands that write the first migration into it.
type creating struct{ Source }

func (c creating) List(ctx context.Context) ([]Migration, error) {
	list, err := c.Source.List(ctx)

	var missing *noFolderError
	if errors.As(err, &missing) {
		return nil, nil
	}

	return list, err
}

// List returns the migrations in both folders. A SQL file that cannot be read as
// a migration is an error, since skipping it would silently skip a schema change.
// Down files of other tools are ignored, so an existing folder can stay as it is.
// A versions folder that is not there is an error; a code folder is optional.
func (f folders) List(context.Context) ([]Migration, error) {
	var out []Migration

	entries, err := fs.ReadDir(f.fsys, f.versions)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, f.noFolder()
	}

	if err != nil {
		return nil, err
	}

	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".sql") ||
			strings.HasSuffix(name, ".down.sql") {
			continue
		}

		version, label, ok := f.parse(name)
		if !ok {
			return nil, fmt.Errorf(
				"%w: cannot read a version from %s: name it like 0001_create_users.up.sql",
				ErrBadMigration,
				path.Join(f.versions, name),
			)
		}

		text, err := fs.ReadFile(f.fsys, path.Join(f.versions, name))
		if err != nil {
			return nil, err
		}

		out = append(out, Migration{
			Kind:    Versioned,
			Version: version,
			Name:    label,
			File:    name,
			SQL:     string(text),
		})
	}

	err = fs.WalkDir(f.fsys, f.code, func(name string, entry fs.DirEntry, err error) error {
		switch {
		case err != nil:
			return err
		case entry.IsDir() || !strings.HasSuffix(name, ".sql"):
			return nil
		}

		text, err := fs.ReadFile(f.fsys, name)
		if err != nil {
			return err
		}

		relative := strings.TrimPrefix(strings.TrimPrefix(name, f.code), "/")
		out = append(out, Migration{
			Kind: Repeatable,
			Name: strings.TrimSuffix(relative, ".sql"),
			File: relative,
			SQL:  string(text),
		})

		return nil
	})
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, err
	}

	return out, nil
}

// Snapshot reads the snapshot file.
func (f folders) Snapshot(context.Context) ([]byte, error) {
	data, err := fs.ReadFile(f.fsys, path.Join(f.versions, snapshotFile))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}

	return data, err
}

// disk adds writing to folders.
type disk struct {
	folders
	root string
}

func (d disk) file(parts ...string) string {
	return filepath.Join(append([]string{d.root}, parts...)...)
}

func (d disk) write(name string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(name), 0o755); err != nil {
		return err
	}
	// Written to the side and moved into place, so a crash leaves no half file.
	temp := name + ".tmp"
	if err := os.WriteFile(temp, data, 0o644); err != nil {
		return err
	}

	return os.Rename(temp, name)
}

// Save writes a new migration, refusing to overwrite one.
func (d disk) Save(_ context.Context, m Migration) error {
	name := d.file(d.versions, m.File)
	if _, err := os.Stat(name); err == nil {
		return fmt.Errorf("%w: %s already exists", ErrDuplicateVersion, name)
	}

	return d.write(name, []byte(m.SQL))
}

// Remove deletes a migration file.
func (d disk) Remove(_ context.Context, m Migration) error {
	return os.Remove(d.file(d.versions, m.File))
}

// SaveSnapshot writes the snapshot file.
func (d disk) SaveSnapshot(_ context.Context, data []byte) error {
	return d.write(d.file(d.versions, snapshotFile), data)
}

// source returns the configured Source.
func (c Config) source() (Source, error) {
	switch {
	case c.Source != nil:
		return c.Source, nil
	case c.FS != nil:
		fsys := c.FS

		if c.Dir != "" && c.Dir != "." {
			sub, err := fs.Sub(fsys, c.Dir)
			if err != nil {
				return nil, err
			}

			fsys = sub
		}

		return folders{fsys, c.VersionsDir, c.CodeDir, c.Parse, ""}, nil
	case c.Embedded != nil:
		if _, err := os.Stat(c.Dir); errors.Is(err, fs.ErrNotExist) {
			return folders{c.Embedded, c.VersionsDir, c.CodeDir, c.Parse, ""}, nil
		}
	}

	return disk{folders{os.DirFS(c.Dir), c.VersionsDir, c.CodeDir, c.Parse, c.Dir}, c.Dir}, nil
}

// store returns the configured Source, for commands that write.
func (c Config) store() (Store, error) {
	source, err := c.source()
	if err != nil {
		return nil, err
	}

	store, ok := source.(Store)
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrReadOnly, c.describe(source))
	}

	return store, nil
}

// describe says which migrations source reads: a folder is found from the
// working directory, so a command run from another folder reads, or writes,
// other migrations, and with Embedded set, a folder that is not there means the
// embedded copy (#177). It follows the order source picks them in, so a Source
// from FS set as Config.Source is named as Config.Source.
func (c Config) describe(source Source) string {
	if s, ok := source.(disk); ok {
		if abs, err := filepath.Abs(s.root); err == nil {
			return abs
		}

		return s.root
	}

	switch {
	case c.Source != nil:
		return "Config.Source"
	case c.FS != nil:
		return "the files in Config.FS"
	}

	at, err := filepath.Abs(c.Dir)
	if err != nil {
		at = c.Dir
	}

	return "the embedded files, since there is no folder " + at
}

// catalog is every known migration, in the order they run.
type catalog struct {
	versioned  []Migration
	repeatable []Migration
}

// load reads a source and merges in the Go migrations registered for dbType,
// checking that the whole makes sense.
func load(ctx context.Context, source Source, registered []Migration) (*catalog, error) {
	list, err := source.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("ant: read migrations: %w", err)
	}

	out := new(catalog)
	files := map[string]bool{}

	for _, m := range slices.Concat(list, registered) {
		if err := m.readDirectives(); err != nil {
			return nil, err
		}

		switch m.Kind {
		case Repeatable:
			if m.File == "" || files[m.File] {
				return nil, fmt.Errorf(
					"%w: a repeatable migration needs a File of its own, and got %q",
					ErrBadMigration,
					m.File,
				)
			}

			files[m.File] = true
			out.repeatable = append(out.repeatable, m)
		case Versioned, "":
			if m.Version == "" {
				return nil, fmt.Errorf("%w: migration %q has no version", ErrBadMigration, m.Name)
			}

			m.Kind = Versioned
			out.versioned = append(out.versioned, m)
		default:
			return nil, fmt.Errorf("%w: %s has unknown kind %q", ErrBadMigration, m.ID(), m.Kind)
		}
	}

	slices.SortStableFunc(
		out.versioned,
		func(a, b Migration) int { return compareVersions(a.Version, b.Version) },
	)

	slices.SortStableFunc(
		out.repeatable,
		func(a, b Migration) int { return strings.Compare(a.File, b.File) },
	)

	for i := 1; i < len(out.versioned); i++ {
		a, b := out.versioned[i-1], out.versioned[i]
		if compareVersions(a.Version, b.Version) == 0 {
			return nil, fmt.Errorf(
				"%w: %s and %s. Renumber the one not yet applied anywhere",
				ErrDuplicateVersion,
				a.ID(),
				b.ID(),
			)
		}
	}

	return out, nil
}

// lastFile returns the newest versioned migration that is SQL, or nil.
func (c *catalog) lastFile() *Migration {
	for i := len(c.versioned) - 1; i >= 0; i-- {
		if c.versioned[i].run == nil {
			return &c.versioned[i]
		}
	}

	return nil
}

// supersededBy returns the baseline that replaces version, or nil.
func (c *catalog) supersededBy(version string) *Migration {
	for i := range c.versioned {
		m := &c.versioned[i]
		if m.replaces != "" && compareVersions(version, m.replaces) <= 0 &&
			compareVersions(version, m.Version) < 0 {
			return m
		}
	}

	return nil
}

func readSnapshot(ctx context.Context, source Source) (*schema.Schema, error) {
	data, err := source.Snapshot(ctx)
	if err != nil {
		return nil, fmt.Errorf("ant: read snapshot: %w", err)
	}

	if data == nil {
		return nil, nil
	}

	return schema.Decode(data)
}
