package preflight

import (
	"fmt"
	"strings"

	"github.com/larstonder/whippletree/internal/contract"
)

// Constraint is a parsed metadata.testedVersions range: the window of
// harness versions a target definition has actually been probed
// against.
//
// The grammar is deliberately tiny, covering what target definitions
// really write and nothing more: one or two space-separated clauses,
// each an operator followed by a dotted version. ">=1.18.10" and
// ">=2.1.0 <3.0.0" both parse; anything richer is an error rather than
// a silent misreading, because a constraint that is quietly ignored is
// worse than one that fails loudly.
type Constraint struct {
	Raw string
	min *contract.Semver // inclusive lower bound, nil if unbounded
	max *contract.Semver // exclusive upper bound, nil if unbounded
}

// ParseConstraint parses a testedVersions string. An empty string
// yields a zero Constraint that admits everything, so a target
// definition that declares no range is simply unchecked.
func ParseConstraint(s string) (Constraint, error) {
	c := Constraint{Raw: s}
	s = strings.TrimSpace(s)
	if s == "" {
		return c, nil
	}

	for _, clause := range strings.Fields(s) {
		switch {
		case strings.HasPrefix(clause, ">="):
			v, err := contract.ParseSemver(strings.TrimPrefix(clause, ">="))
			if err != nil {
				return Constraint{}, fmt.Errorf("testedVersions %q: %w", c.Raw, err)
			}
			c.min = &v
		case strings.HasPrefix(clause, "<"):
			v, err := contract.ParseSemver(strings.TrimPrefix(clause, "<"))
			if err != nil {
				return Constraint{}, fmt.Errorf("testedVersions %q: %w", c.Raw, err)
			}
			c.max = &v
		default:
			return Constraint{}, fmt.Errorf("testedVersions %q: unsupported clause %q; want >=X.Y.Z or <X.Y.Z", c.Raw, clause)
		}
	}
	return c, nil
}

// Check reports whether v falls inside the constraint. The returned
// string is a human-readable reason when it does not, and empty when it
// does. An unparseable or empty v is treated as inside: preflight
// already reports an unprobed target as "unknown", and this should not
// invent a second failure mode for it.
func (c Constraint) Check(v Version) (ok bool, reason string) {
	if string(v) == "" || (c.min == nil && c.max == nil) {
		return true, ""
	}
	got, err := contract.ParseSemver(string(v))
	if err != nil {
		return true, ""
	}
	if c.min != nil && got.Less(*c.min) {
		return false, fmt.Sprintf("probed %s is below the tested range %s", v, c.Raw)
	}
	if c.max != nil && !got.Less(*c.max) {
		return false, fmt.Sprintf("probed %s is at or above the tested range %s", v, c.Raw)
	}
	return true, ""
}
