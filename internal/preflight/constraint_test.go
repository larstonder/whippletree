package preflight_test

import (
	"testing"

	"github.com/larstonder/whippletree/internal/preflight"
)

func TestParseConstraintAndCheck(t *testing.T) {
	cases := []struct {
		name       string
		constraint string
		version    preflight.Version
		wantOK     bool
	}{
		{"no constraint admits everything", "", "0.0.1", true},
		{"unprobed is never flagged", ">=2.1.0", "", true},
		{"at the floor", ">=2.1.0", "2.1.0", true},
		{"above the floor", ">=2.1.0", "2.1.220", true},
		{"below the floor", ">=2.1.0", "2.0.9", false},
		{"far below the floor", ">=0.144.0", "0.100.0", false},
		{"minor below the floor", ">=1.18.10", "1.18.9", false},
		{"inside a window", ">=2.1.0 <3.0.0", "2.5.0", true},
		{"at the ceiling is outside", ">=2.1.0 <3.0.0", "3.0.0", false},
		{"above the ceiling", ">=2.1.0 <3.0.0", "3.1.0", false},
		{"unparseable probe is not flagged", ">=2.1.0", "nightly", true},
		{"two-component version", ">=2.1.0", "2.2", true},
		{"v prefix", ">=2.1.0", "v2.2.0", true},
		{"prerelease suffix is ignored", ">=2.1.0", "2.2.0-beta.1", true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c, err := preflight.ParseConstraint(tc.constraint)
			if err != nil {
				t.Fatalf("preflight.ParseConstraint(%q) = %v", tc.constraint, err)
			}
			ok, reason := c.Check(tc.version)
			if ok != tc.wantOK {
				t.Errorf("Check(%q) against %q = %v (%q), want %v", tc.version, tc.constraint, ok, reason, tc.wantOK)
			}
			if !ok && reason == "" {
				t.Error("a failing Check must explain itself")
			}
		})
	}
}

// TestParseConstraintRejectsUnsupportedGrammar: the grammar is
// deliberately tiny, and anything outside it must fail loudly rather
// than be silently ignored.
func TestParseConstraintRejectsUnsupportedGrammar(t *testing.T) {
	for _, s := range []string{"^2.1.0", "~2.1.0", ">2.1.0", "2.1.0", ">=abc", ">=2", "<=3.0.0"} {
		if _, err := preflight.ParseConstraint(s); err == nil {
			t.Errorf("preflight.ParseConstraint(%q) = nil, want an error", s)
		}
	}
}

// TestShippedTargetsDeclareParseableRanges pins that every target
// definition in the repo actually carries a range this code can read.
func TestShippedTargetsDeclareParseableRanges(t *testing.T) {
	defs := loadTargets(t)
	for name, td := range defs {
		if td.TestedVersions == "" {
			t.Errorf("target %s declares no testedVersions", name)
			continue
		}
		if _, err := preflight.ParseConstraint(td.TestedVersions); err != nil {
			t.Errorf("target %s: %v", name, err)
		}
	}
}
