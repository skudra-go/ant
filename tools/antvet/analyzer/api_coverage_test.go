package analyzer

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestFieldPointerAPIsAreInTheTables walks ant's exported API, as docs/api.txt
// records it, and fails when a function or a method that takes a field or a
// relation pointer is not in the tables the analyzer judges by, or is in them
// at an argument the tables do not cover. A new API that takes one can then
// not be missed again: the tables, not a reviewer's memory, are what has to
// change with it.
//
// Only the ant package is walked: the analyzer checks its functions and its
// builders' methods, and nothing of the other packages api.txt lists.
//
// A parameter counts as a field or a relation pointer when it is *V or
// *time.Time, a relation in *[]R or **R form, or a key in *A or *C form, as
// the through methods take them. An entity pointer such as *T, a builder
// such as *ModelBuilder, and a FieldRef, which ant.F already checked when it
// was made, do not count.
func TestFieldPointerAPIsAreInTheTables(t *testing.T) {
	// Latest, Oldest, Load, LoadOne, LoadMissing, Recursive, and
	// RecursiveOne are being added to the tables by open PR #291, which
	// covers issue #102's first half; this test does not fail on them
	// while that PR is open. Delete this list once it merges: the walk
	// below then covers those names like every other.
	pending := map[string]bool{
		"Latest":       true,
		"Oldest":       true,
		"Load":         true,
		"LoadOne":      true,
		"LoadMissing":  true,
		"Recursive":    true,
		"RecursiveOne": true,
	}

	content, err := os.ReadFile(filepath.Join("..", "..", "..", "docs", "api.txt"))
	if err != nil {
		t.Fatal(err)
	}

	funcs := map[string]bool{}
	methods := map[string]bool{}

	for line := range strings.SplitSeq(string(content), "\n") {
		rest, ok := strings.CutPrefix(line, "ant: func ")
		if !ok {
			continue
		}

		receiver, name, params, ok := parseSignature(rest)
		if !ok {
			t.Errorf("cannot parse %q", line)

			continue
		}

		if receiver == "" {
			funcs[name] = true
		} else {
			methods[name] = true
		}

		if pending[name] {
			continue
		}

		for _, at := range fieldPointerArgs(params) {
			if receiver == "" {
				spec, ok := fieldFuncs[name]
				if !ok {
					t.Errorf(
						"ant.%s takes a field pointer at argument %d, which is not in fieldFuncs: %s",
						name,
						at,
						line,
					)

					continue
				}

				if !covers(spec, at, len(params)) {
					t.Errorf(
						"ant.%s takes a field pointer at argument %d, which fieldFuncs does not cover: %s",
						name,
						at,
						line,
					)
				}

				continue
			}

			if _, ok := fieldMethods[name]; !ok {
				t.Errorf(
					"%s.%s takes a field pointer at argument %d, which is not in fieldMethods: %s",
					receiver,
					name,
					at,
					line,
				)

				continue
			}

			spec := first

			if f, ok := methodFields[name]; ok {
				spec = f
			}

			if !covers(spec, at, len(params)) {
				t.Errorf(
					"%s.%s takes a field pointer at argument %d, which fieldMethods does not cover: %s",
					receiver,
					name,
					at,
					line,
				)
			}
		}
	}

	// The tables must not name an API that is gone, and the pending list
	// must not name one either: a stale entry hides a rename.
	for name := range fieldFuncs {
		if !funcs[name] {
			t.Errorf("fieldFuncs names ant.%s, which is not in docs/api.txt", name)
		}
	}

	for name := range fieldMethods {
		if !methods[name] {
			t.Errorf("fieldMethods names a method %s, which is not in docs/api.txt", name)
		}
	}

	for name := range methodFields {
		if fieldMethods[name] == 0 {
			t.Errorf("methodFields names %s, which is not in fieldMethods", name)
		}
	}

	for name := range pending {
		if !funcs[name] && !methods[name] {
			t.Errorf("pending names %s, which is not in docs/api.txt", name)
		}
	}
}

// covers reports whether the field pointers at, of a call with n arguments,
// are among the ones spec takes: count of them from at, or all from there on
// if count is 0.
func covers(spec fields, at, n int) bool {
	if at < spec.at {
		return false
	}

	if spec.count == 0 {
		return at < n
	}

	return at < spec.at+spec.count
}

// parseSignature splits a signature from docs/api.txt into its receiver type,
// if it is a method, its name, and its parameters, each as the text of one
// parameter. It returns false where the signature is not in the shape api.txt
// writes.
func parseSignature(sig string) (receiver, name string, params []string, ok bool) {
	rest := sig

	if strings.HasPrefix(rest, "(") {
		end := strings.Index(rest, ")")
		if end < 0 {
			return "", "", nil, false
		}

		receiver = receiverName(rest[1:end])
		rest = strings.TrimSpace(rest[end+1:])
	}

	end := strings.IndexAny(rest, "[(")
	if end < 0 {
		return "", "", nil, false
	}

	name = rest[:end]
	rest = rest[end:]

	if strings.HasPrefix(rest, "[") {
		end := strings.Index(rest, "]")
		if end < 0 {
			return "", "", nil, false
		}

		rest = rest[end+1:]
	}

	if !strings.HasPrefix(rest, "(") {
		return "", "", nil, false
	}

	depth := 0

	for i, r := range rest {
		switch r {
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return receiver, name, splitParams(rest[1:i]), true
			}
		}
	}

	return "", "", nil, false
}

// receiverName names the type a method is declared on, without its
// receiver variable, its pointer, or its type arguments:
// (b *ModelBuilder[E]) is ModelBuilder.
func receiverName(decl string) string {
	fields := strings.Fields(decl)
	if len(fields) == 0 {
		return ""
	}

	typ := strings.TrimPrefix(fields[len(fields)-1], "*")

	if at := strings.Index(typ, "["); at >= 0 {
		typ = typ[:at]
	}

	return typ
}

// splitParams splits a parameter list on its top-level commas, expanding
// names that share a type, as a, b *V does, into one entry each.
func splitParams(list string) []string {
	var (
		out     []string
		pending []string
		depth   int
		cur     strings.Builder
	)

	flush := func() {
		part := strings.TrimSpace(cur.String())
		cur.Reset()

		if part == "" {
			return
		}

		if strings.Contains(part, " ") || strings.Contains(part, "func(") {
			typ := part

			if at := strings.LastIndex(part, " "); at >= 0 && !strings.Contains(part, "func(") {
				typ = part[at+1:]
			}

			for range pending {
				out = append(out, typ)
			}

			pending = nil

			out = append(out, typ)

			return
		}

		pending = append(pending, part)
	}

	for _, r := range list {
		switch r {
		case '(', '[':
			depth++
		case ')', ']':
			depth--
		}

		if r == ',' && depth == 0 {
			flush()

			continue
		}

		cur.WriteRune(r)
	}

	flush()

	return out
}

// fieldPointerArgs returns which of params are field or relation pointers,
// by their type alone. A parameter that only names a value, as a, b *V
// names a before its type, still counts: splitParams already gave it the
// shared type.
func fieldPointerArgs(params []string) []int {
	var out []int

	for i, typ := range params {
		if isFieldPointer(typ) {
			out = append(out, i)
		}
	}

	return out
}

// isFieldPointer reports whether a parameter type is a field or a relation
// pointer: *V, *time.Time, *A or *C, a pointer to a pointer or a slice, or
// a variadic of one of those. Anything else a pointer points to, an entity
// or a builder, is not a field.
func isFieldPointer(typ string) bool {
	typ = strings.TrimPrefix(typ, "...")

	if !strings.HasPrefix(typ, "*") {
		return false
	}

	inner := typ[1:]

	switch {
	case inner == "V" || inner == "time.Time" || inner == "A" || inner == "C":
		return true
	case strings.HasPrefix(inner, "*") || strings.HasPrefix(inner, "[]"):
		return true
	}

	return false
}
