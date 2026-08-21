package preflight

import (
	"fmt"
	"strings"

	"whippletree.dev/internal/contract"
)

// Constraint is a parsed metadata.testedVersions range, e.g. ">=1.18.10"
// or ">=2.1.0 <3.0.0". The grammar is deliberately tiny and anything
// richer is an error: a constraint that is quietly ignored is worse than
// one that fails loudly.
type Constraint struct {
	Raw string
	min *contract.Semver // inclusive lower bound, nil if unbounded
	max *contract.Semver // exclusive upper bound, nil if unbounded
}

// ParseConstraint parses a testedVersions string. Empty admits
// everything, so a target declaring no range is simply unchecked.
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

// Check reports whether v falls inside the constraint, with a reason
// when it does not. An unparseable or empty v counts as inside;
// preflight already reports an unprobed target as "unknown".
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
