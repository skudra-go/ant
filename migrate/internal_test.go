package migrate

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/skudra-go/ant/dialect/postgres"
	"github.com/skudra-go/ant/schema"
)

func TestCompareVersions(t *testing.T) {
	require.Negative(t, compareVersions("9", "10"), "numbers, not text")
	require.Negative(t, compareVersions("0009", "10"), "whatever the padding")
	require.Zero(t, compareVersions("0010", "10"))
	require.Negative(t, compareVersions("20261030000000", "20261031000000"))

	require.Negative(
		t,
		compareVersions("99999999999999999999999", "100000000000000000000000"),
		"beyond int64",
	)

	require.Negative(t, compareVersions("a", "b"), "text if not numbers")
}

func TestParseFile(t *testing.T) {
	for file, want := range map[string][2]string{
		"0001_create_users.up.sql":   {"0001", "create_users"},
		"000001_create_users.up.sql": {"000001", "create_users"},
		"1_create_users.sql":         {"1", "create_users"},
		"20261031120501_add-bio.sql": {"20261031120501", "add-bio"},
		"0002-add_bio.sql":           {"0002", "add_bio"},
		"V3__add_bio.sql":            {"3", "add_bio"},
	} {
		version, name, ok := parseFile(file)
		require.True(t, ok, file)
		require.Equal(t, want, [2]string{version, name}, file)
	}

	_, _, ok := parseFile("add_bio.sql")
	require.False(t, ok)
}

func TestNextVersion(t *testing.T) {
	require.Equal(t, "0001", nextVersion(nil))
	require.Equal(t, "0010", nextVersion([]Migration{{Version: "0002"}, {Version: "0009"}}))
	require.Equal(t, "000003", nextVersion([]Migration{{Version: "000002"}}), "keeps the padding")
	require.Equal(t, "10000", nextVersion([]Migration{{Version: "9999"}}))
	require.Equal(t, "0004", nextVersion([]Migration{{Version: "0003"}, {Version: "beta"}}))
}

func TestSlug(t *testing.T) {
	require.Equal(t, "add_user_bio", slug("Add User-Bio!"))
	require.Equal(t, "", slug("..."))
}

func TestChecksumIgnoresLineEndings(t *testing.T) {
	a := Migration{SQL: "SELECT 1;\nSELECT 2;\n"}
	b := Migration{SQL: "SELECT 1;\r\nSELECT 2;\r\n"}
	require.Equal(t, a.Checksum(), b.Checksum())
	require.NotEqual(t, a.Checksum(), Migration{SQL: "SELECT 3;\n"}.Checksum())
}

func TestDirectives(t *testing.T) {
	m := Migration{
		SQL: "-- a comment\n\n-- ant:no-transaction\n-- ant:replaces 0009\nSELECT 1;\n-- ant:ignored after code\n",
	}
	require.NoError(t, m.readDirectives())
	require.True(t, m.noTransaction)
	require.Equal(t, "0009", m.replaces)

	bad := Migration{Version: "1", Name: "x", SQL: "-- ant:replaces\nSELECT 1;"}
	require.ErrorIs(t, bad.readDirectives(), ErrBadMigration)
}

func TestWithoutMetaCommands(t *testing.T) {
	require.Equal(
		t,
		"SELECT 1;\n'\\keep';\n",
		withoutMetaCommands("\\restrict x\nSELECT 1;\n'\\keep';\n\\unrestrict x\n"),
	)
}

// In a terminal, diff asks about each possible rename, and a yes keeps the data.
func TestRenamePrompt(t *testing.T) {
	old := &schema.Schema{
		Tables: []*schema.Table{{
			Name: "users",
			Columns: []*schema.Column{
				{Name: "bio", Type: "text"}, {Name: "age", Type: "bigint"},
			},
		}},
	}

	target := &schema.Schema{
		Tables: []*schema.Table{{
			Name: "users",
			Columns: []*schema.Column{
				{Name: "about", Type: "text"}, {Name: "years", Type: "bigint"},
			},
		}},
	}

	data, err := old.Encode()
	require.NoError(t, err)
	d, err := schema.Of(postgres.Dialect)
	require.NoError(t, err)

	store := &memoryStore{snapshot: data}

	var (
		asked []string
		out   bytes.Buffer
	)

	config := Config{Source: store, Out: &out}.resolve(d.Name())
	tool := &tool{
		d:      d,
		config: config,
		source: store,
		models: func() (*schema.Schema, error) { return target.Clone(), nil },
		ask: func(question string) bool {
			asked = append(asked, question)

			return strings.Contains(question, "bio") // yes for bio, no for age
		},
	}
	require.NoError(t, tool.diff(t.Context(), "tidy", nil, false, false))

	require.Len(t, asked, 2)
	require.Contains(t, asked[0], "column users.")

	text := store.saved[0].SQL
	require.Contains(t, text, `RENAME COLUMN "bio" TO "about"`)
	require.Contains(t, text, `DROP COLUMN "age"`, "a no leaves the drop, and its warning")
	require.Contains(t, text, `RENAME COLUMN "age" TO "years"`, "as a suggestion in a comment")
}

func TestDriftMessagesUseTheSingular(t *testing.T) {
	require.Equal(
		t,
		"1 migration is pending, so the database is expected to differ. Run up first.",
		pendingNote(1),
	)
	require.Equal(
		t,
		"2 migrations are pending, so the database is expected to differ. Run up first.",
		pendingNote(2),
	)

	require.Equal(t, "1 difference", driftNote(1))
	require.Equal(t, "2 differences", driftNote(2))
}

func TestCheckNamesBothCausesWhenSnapshotIsBehind(t *testing.T) {
	snap := &schema.Schema{Latest: "0009_foo"}

	data, err := snap.Encode()
	require.NoError(t, err)

	d, err := schema.Of(postgres.Dialect)
	require.NoError(t, err)

	store := &memoryStore{
		snapshot: data,
		saved: []Migration{
			{Version: "0009", Name: "foo", File: "0009_foo.up.sql", SQL: "SELECT 1;"},
			{Version: "0010", Name: "hand", File: "0010_hand.up.sql", SQL: "SELECT 1;"},
		},
	}

	tool := &tool{d: d, source: store}

	c, err := tool.drafts(t.Context())
	require.NoError(t, err)

	err = tool.inStep(t.Context(), c)
	require.ErrorIs(t, err, ErrStaleSnapshot)
	require.Contains(t, err.Error(), "If a file was added by hand, start it with diff --empty <name>")
	require.Contains(t, err.Error(), "if a merge added it, renumber if needed, then run snapshot")
}

type memoryStore struct {
	saved    []Migration
	snapshot []byte
}

func (m *memoryStore) List(context.Context) ([]Migration, error) { return m.saved, nil }
func (m *memoryStore) Snapshot(context.Context) ([]byte, error)  { return m.snapshot, nil }
func (m *memoryStore) Save(_ context.Context, migration Migration) error {
	m.saved = append(m.saved, migration)

	return nil
}
func (m *memoryStore) Remove(context.Context, Migration) error { return nil }
func (m *memoryStore) SaveSnapshot(_ context.Context, data []byte) error {
	m.snapshot = data

	return nil
}
