// Package docs checks the design decisions in docs/decisions: each file says what
// it is about in a header the index is built from, and the index in
// docs/decisions/README.md is up to date. To rewrite the index:
//
//	go test ./tests/docs -update
package docs

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

var update = flag.Bool("update", false, "rewrite the index in docs/decisions/README.md")

const (
	root    = "../.."
	folder  = "docs/decisions"
	readme  = "docs/decisions/README.md"
	opening = "<!-- index: written by go test ./tests/docs -update -->"
	closing = "<!-- end of index -->"
)

// areas are the groups of the index, in its order.
var areas = []struct{ key, name string }{
	{"models", "Models and mapping"},
	{"values", "Values and types"},
	{"relations", "Relations"},
	{"queries", "Queries"},
	{"writes", "Writes"},
	{"tracking", "Change tracking"},
	{"transactions", "Transactions"},
	{"hooks", "Hooks and events"},
	{"errors", "Errors"},
	{"dialects", "Databases and dialects"},
	{"migrations", "Migrations and schema"},
	{"performance", "Performance"},
	{"observability", "Logging and tracing"},
	{"security", "Security"},
	{"testing", "Testing"},
	{"tooling", "Tooling"},
	{"process", "Process and history"},
}

// statuses are what a decision's status may be.
var statuses = []string{"active", "amended", "superseded", "historical", "pending"}

// decision is the header of one file.
type decision struct {
	file      string
	id        int
	title     string
	area      string
	status    string
	pr        int
	tags      []string
	issues    []int
	related   []int
	changes   []int
	changedBy []int
	code      []string
	tests     []string
}

var namePattern = regexp.MustCompile(`^(\d{3})-[a-z0-9-]+\.md$`)

func TestDecisions(t *testing.T) {
	decisions := load(t)
	byID := map[int]*decision{}

	for _, d := range decisions {
		require.Nil(t, byID[d.id], "decision %d is in two files", d.id)
		byID[d.id] = d
	}

	for _, d := range decisions {
		t.Run(d.file, func(t *testing.T) {
			check(t, d, byID)
		})
	}

	index := render(decisions)
	path := filepath.Join(root, readme)

	text, err := os.ReadFile(path)
	require.NoError(t, err)

	start := strings.Index(string(text), opening)
	end := strings.Index(string(text), closing)
	require.True(t, start >= 0 && end > start, "%s has no index markers", readme)

	want := string(text[:start]) + opening + "\n\n" + index + "\n" + string(text[end:])

	if *update {
		require.NoError(t, os.WriteFile(path, []byte(want), 0o644))

		return
	}

	require.Equal(t, want, string(text), "the index is out of date: go test ./tests/docs -update")
}

// check verifies one decision's header and title.
func check(t *testing.T, d *decision, byID map[int]*decision) {
	t.Helper()

	require.NotEmpty(t, d.title, "title")

	require.True(
		t,
		slices.ContainsFunc(areas, func(a struct{ key, name string }) bool {
			return a.key == d.area
		}),
		"area %q is not one of the index's",
		d.area,
	)

	require.Contains(t, statuses, d.status, "status")
	require.GreaterOrEqual(t, len(d.tags), 1, "tags")

	text, err := os.ReadFile(filepath.Join(root, folder, d.file))
	require.NoError(t, err)

	require.Contains(
		t,
		string(text),
		fmt.Sprintf("\n# %d. %s\n", d.id, d.title),
		"the heading repeats the title",
	)

	require.Contains(t, string(text), "\n**In short:** ", "a one-sentence summary")
	require.Contains(t, string(text), "\n## Decision\n", "the decision's text")

	switch d.status {
	case "amended", "superseded":
		require.True(
			t,
			len(d.changedBy) > 0 || strings.Contains(string(text), "> **"),
			"say what changed it, in changed_by or a note",
		)
	case "pending":
		require.NotZero(t, d.pr, "a pending decision names its pull request")
	}

	for _, id := range slices.Concat(d.related, d.changes, d.changedBy) {
		require.NotNil(t, byID[id], "decision %d refers to %d, which has no file", d.id, id)
	}

	// The files a pending decision names may exist only in its pull request.
	if d.status == "pending" {
		return
	}

	for _, p := range slices.Concat(d.code, d.tests) {
		_, err := os.Stat(filepath.Join(root, p))
		require.NoError(t, err, "decision %d names %s, which does not exist", d.id, p)
	}
}

// load reads every decision's header.
func load(t *testing.T) []*decision {
	t.Helper()

	entries, err := os.ReadDir(filepath.Join(root, folder))
	require.NoError(t, err)

	var out []*decision

	for _, e := range entries {
		if e.Name() == "README.md" {
			continue
		}

		m := namePattern.FindStringSubmatch(e.Name())
		require.NotNil(t, m, "%s: name a decision NNN-short-title.md", e.Name())

		text, err := os.ReadFile(filepath.Join(root, folder, e.Name()))
		require.NoError(t, err)

		d := parse(t, e.Name(), string(text))
		require.Equal(
			t,
			m[1],
			fmt.Sprintf("%03d", d.id),
			"%s: the file's number is its id",
			e.Name(),
		)

		out = append(out, d)
	}

	sort.Slice(out, func(i, j int) bool { return out[i].id < out[j].id })

	return out
}

// parse reads the header between the --- lines at the top of a file: one key
// and value per line, lists in brackets.
func parse(t *testing.T, file, text string) *decision {
	t.Helper()

	require.True(t, strings.HasPrefix(text, "---\n"), "%s: starts with a --- header", file)

	header, _, ok := strings.Cut(text[len("---\n"):], "\n---\n")
	require.True(t, ok, "%s: the header ends with ---", file)

	d := &decision{file: file}

	for _, line := range strings.Split(header, "\n") {
		key, value, ok := strings.Cut(line, ": ")
		require.True(t, ok, "%s: header line %q", file, line)

		switch key {
		case "id":
			d.id = number(t, file, value)
		case "title":
			unquoted, err := strconv.Unquote(value)
			require.NoError(t, err, "%s: title is a quoted string", file)

			d.title = unquoted
		case "area":
			d.area = value
		case "status":
			d.status = value
		case "pr":
			d.pr = number(t, file, value)
		case "tags":
			d.tags = list(t, file, value)
		case "issues":
			d.issues = numbers(t, file, value)
		case "related":
			d.related = numbers(t, file, value)
		case "changes":
			d.changes = numbers(t, file, value)
		case "changed_by":
			d.changedBy = numbers(t, file, value)
		case "code":
			d.code = list(t, file, value)
		case "tests":
			d.tests = list(t, file, value)
		default:
			t.Fatalf("%s: unknown header key %q", file, key)
		}
	}

	return d
}

func list(t *testing.T, file, value string) []string {
	t.Helper()

	require.True(
		t,
		strings.HasPrefix(value, "[") && strings.HasSuffix(value, "]"),
		"%s: %q is a [list]",
		file,
		value,
	)

	inner := strings.TrimSpace(value[1 : len(value)-1])
	if inner == "" {
		return nil
	}

	var out []string

	for _, item := range strings.Split(inner, ",") {
		out = append(out, strings.TrimSpace(item))
	}

	return out
}

func numbers(t *testing.T, file, value string) []int {
	t.Helper()

	var out []int

	for _, item := range list(t, file, value) {
		out = append(out, number(t, file, item))
	}

	return out
}

func number(t *testing.T, file, value string) int {
	t.Helper()

	n, err := strconv.Atoi(value)
	require.NoError(t, err, "%s: %q is a number", file, value)

	return n
}

// render writes the index: by area, pending decisions, by file, and by tag.
func render(decisions []*decision) string {
	var b strings.Builder

	link := func(d *decision) string { return fmt.Sprintf("[%d](%s)", d.id, d.file) }

	b.WriteString("### By area\n")

	for _, area := range areas {
		var in []*decision

		for _, d := range decisions {
			if d.area == area.key {
				in = append(in, d)
			}
		}

		if len(in) == 0 {
			continue
		}

		fmt.Fprintf(
			&b,
			"\n#### %s\n\n| # | Decision | Status | Tags |\n|---|---|---|---|\n",
			area.name,
		)

		for _, d := range in {
			status := d.status

			if len(d.changedBy) > 0 {
				status += " by " + ids(decisions, d.changedBy)
			}

			if d.pr != 0 {
				status += fmt.Sprintf(" in #%d", d.pr)
			}

			fmt.Fprintf(
				&b,
				"| %s | %s | %s | %s |\n",
				link(d),
				strings.ReplaceAll(d.title, "|", `\|`),
				status,
				"`"+strings.Join(d.tags, "` `")+"`",
			)
		}
	}

	files := map[string][]*decision{}

	for _, d := range decisions {
		for _, p := range slices.Concat(d.code, d.tests) {
			if !slices.Contains(files[p], d) {
				files[p] = append(files[p], d)
			}
		}
	}

	paths := make([]string, 0, len(files))

	for p := range files {
		paths = append(paths, p)
	}

	sort.Strings(paths)

	b.WriteString(
		"\n### By file\n\nBefore changing a file, read the decisions about it.\n\n| File | Decisions |\n|---|---|\n",
	)

	for _, p := range paths {
		var list []string

		for _, d := range files[p] {
			list = append(list, link(d))
		}

		fmt.Fprintf(&b, "| `%s` | %s |\n", p, strings.Join(list, " "))
	}

	tags := map[string][]*decision{}

	for _, d := range decisions {
		for _, tag := range d.tags {
			tags[tag] = append(tags[tag], d)
		}
	}

	names := make([]string, 0, len(tags))

	for tag := range tags {
		names = append(names, tag)
	}

	sort.Slice(names, func(i, j int) bool {
		a, b := strings.ToLower(names[i]), strings.ToLower(names[j])
		if a != b {
			return a < b
		}

		return names[i] < names[j]
	})

	b.WriteString(
		"\n### By tag\n\nTags on two or more decisions. Search the headers for the rest: `rg -l '^tags: .*word' docs/decisions`.\n\n",
	)

	for _, tag := range names {
		if len(tags[tag]) < 2 {
			continue
		}

		var list []string

		for _, d := range tags[tag] {
			list = append(list, link(d))
		}

		fmt.Fprintf(&b, "- `%s`: %s\n", tag, strings.Join(list, " "))
	}

	return b.String()
}

func ids(decisions []*decision, of []int) string {
	var out []string

	for _, id := range of {
		for _, d := range decisions {
			if d.id == id {
				out = append(out, fmt.Sprintf("[%d](%s)", d.id, d.file))
			}
		}
	}

	return strings.Join(out, ", ")
}
