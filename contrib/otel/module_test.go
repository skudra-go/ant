package otel

import (
	"os"
	"regexp"
	"testing"
)

// A program that adds this module ignores its replace directive, so it resolves
// ant at the version required here, which must be a release: a zero version, or
// any pseudo-version, is one the module proxy may not have.
func TestRequiresAReleasedAnt(t *testing.T) {
	data, err := os.ReadFile("go.mod")
	if err != nil {
		t.Fatal(err)
	}

	line := regexp.MustCompile(`(?m)^\s*(?:require\s+)?github\.com/skudra-go/ant\s+(\S+)`).FindSubmatch(data)
	if line == nil {
		t.Fatal("go.mod does not require github.com/skudra-go/ant")
	}

	release := regexp.MustCompile(`^v\d+\.\d+\.\d+$`)
	if version := string(line[1]); !release.MatchString(version) {
		t.Fatalf("go.mod requires github.com/skudra-go/ant %s, which is not a release", version)
	}
}
