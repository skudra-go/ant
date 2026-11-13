package api

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// stdlib are package names the skill calls that aren't ant's, and t, a test's
// *testing.T.
var stdlib = map[string]bool{
	"context": true,
	"errors":  true,
	"fmt":     true,
	"log":     true,
	"os":      true,
	"slog":    true,
	"sql":     true,
	"strings": true,
	"t":       true,
	"time":    true,
}

// foreign are calls from other ORMs that the skill names as ones that don't exist.
var foreign = map[string]bool{
	"Model":   true,
	"Preload": true,
}

var (
	// A package-qualified name, such as ant.Gt or anttest.Isolate.
	qualified = regexp.MustCompile(`\b([a-z]+)\.([A-Z]\w*)`)

	// A method call: db.Users.Where(, a chained line's Include(, or `First(ctx)`.
	call = regexp.MustCompile("(?:(\\w+)\\.|^\\s+|`)([A-Z]\\w*)(?:\\[[^\\]]*\\])?\\(")

	// The name a declaration line from declarations declares.
	declared = regexp.MustCompile(`^[\w/]+: (?:func (?:\([^)]*\) )?|type |const |var )(\w+)`)
)

// TestSkillNamesExist checks that every ant name the agent skill uses is part of
// the public API, so a rename or a removal can't leave the skill teaching code
// that doesn't compile. The samples are type-checked in full by ant-docs'
// doccheck: see CONTRIBUTING.md.
func TestSkillNamesExist(t *testing.T) {
	root := filepath.Join("..", "..")
	text, err := os.ReadFile(filepath.Join(root, "skills", "ant", "SKILL.md"))
	require.NoError(t, err)

	names := map[string]map[string]bool{}
	methods := map[string]bool{}

	for _, pkg := range packages {
		short := filepath.Base(pkg)

		if pkg == "." {
			short = "ant"
		}

		names[short] = map[string]bool{}

		for _, line := range declarations(t, root, pkg) {
			m := declared.FindStringSubmatch(line)
			if m == nil {
				continue
			}

			names[short][m[1]] = true

			if strings.Contains(line, ": func (") {
				methods[m[1]] = true
			}
		}
	}

	// A hook is a method of an interface, such as BeforeInsert.
	for _, name := range interfaceMethods(t, root) {
		methods[name] = true
	}

	var missing []string

	for _, line := range strings.Split(string(text), "\n") {
		for _, m := range qualified.FindAllStringSubmatch(line, -1) {
			if known, ok := names[m[1]]; ok && !known[m[2]] {
				missing = append(missing, m[1]+"."+m[2])
			}
		}

		for _, m := range call.FindAllStringSubmatch(line, -1) {
			if stdlib[m[1]] || names[m[1]] != nil {
				continue
			}

			if !methods[m[2]] && !foreign[m[2]] {
				missing = append(missing, "method "+m[2])
			}
		}
	}

	require.Empty(t, missing, "skills/ant/SKILL.md names what the API doesn't have")
}

// interfaceMethods lists the methods of the root package's exported interfaces.
func interfaceMethods(t *testing.T, root string) []string {
	t.Helper()

	fset := token.NewFileSet()
	entries, err := os.ReadDir(root)
	require.NoError(t, err)

	var out []string

	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), ".go") || strings.HasSuffix(entry.Name(), "_test.go") {
			continue
		}

		file, err := parser.ParseFile(fset, filepath.Join(root, entry.Name()), nil, 0)
		require.NoError(t, err)

		ast.Inspect(file, func(n ast.Node) bool {
			spec, ok := n.(*ast.TypeSpec)
			if !ok || !spec.Name.IsExported() {
				return true
			}

			if iface, ok := spec.Type.(*ast.InterfaceType); ok {
				for _, m := range iface.Methods.List {
					for _, name := range m.Names {
						out = append(out, name.Name)
					}
				}
			}

			return true
		})
	}

	return out
}
