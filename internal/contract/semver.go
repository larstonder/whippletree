// SPDX-FileCopyrightText: 2026 Lars Tønder
// SPDX-License-Identifier: Apache-2.0

package contract

import (
	"fmt"
	"strconv"
	"strings"
)

// Semver is a major.minor.patch triple. preflight reuses it for harness
// version ranges so there is one parser, not two that disagree.
type Semver struct {
	Major, Minor, Patch int
}

// ParseSemver parses "X.Y" or "X.Y.Z", tolerating a leading "v" and
// ignoring any prerelease or build suffix: harness version strings are
// not reliably semver, and every comparison here is coarse.
func ParseSemver(s string) (Semver, error) {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "v")
	if i := strings.IndexAny(s, "-+"); i >= 0 {
		s = s[:i]
	}
	parts := strings.Split(s, ".")
	if len(parts) < 2 || len(parts) > 3 {
		return Semver{}, fmt.Errorf("invalid version %q; want X.Y or X.Y.Z", s)
	}

	var out [3]int
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return Semver{}, fmt.Errorf("invalid version %q; want X.Y or X.Y.Z", s)
		}
		out[i] = n
	}
	return Semver{out[0], out[1], out[2]}, nil
}

// Less reports whether a orders before b.
func (a Semver) Less(b Semver) bool {
	if a.Major != b.Major {
		return a.Major < b.Major
	}
	if a.Minor != b.Minor {
		return a.Minor < b.Minor
	}
	return a.Patch < b.Patch
}

func (a Semver) String() string {
	return fmt.Sprintf("%d.%d.%d", a.Major, a.Minor, a.Patch)
}
