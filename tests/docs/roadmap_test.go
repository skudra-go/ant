package docs

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// The roadmap groups features by area, not by release: releases are the git
// tags. Each item not done says why under it, so nobody starts one that was
// decided against.
func TestRoadmap(t *testing.T) {
	text, err := os.ReadFile(filepath.Join(root, "docs/ROADMAP.md"))
	require.NoError(t, err)

	lines := strings.Split(string(text), "\n")
	version := regexp.MustCompile(`^#+ .*\bv\d+\.\d+`)

	for i, line := range lines {
		require.False(
			t,
			version.MatchString(line),
			"line %d: a heading names a version: %s",
			i+1,
			line,
		)

		if !strings.HasPrefix(line, "- [ ] ") {
			continue
		}

		require.True(
			t,
			i+1 < len(lines) && strings.HasPrefix(lines[i+1], "  - Skipped: "),
			"line %d: %q needs a \"  - Skipped: reason\" line under it",
			i+1,
			line,
		)
	}
}
