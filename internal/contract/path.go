// SPDX-FileCopyrightText: 2026 Lars Tønder
// SPDX-License-Identifier: Apache-2.0

package contract

import (
	"fmt"
	"path"
	"path/filepath"
	"regexp"
	"strings"
)

// driveLetter matches a Windows volume prefix such as "C:" or "c:/".
var driveLetter = regexp.MustCompile(`^[A-Za-z]:`)

// ValidateBundleRelPath checks that p, joined onto a bundle root, cannot
// address anything outside it.
//
// internal/dispatch repeats this check at run time. That is not
// redundant: the dispatcher reads the vendored contract.json, which in a
// bundle whippletree did not compile never passed through Validate.
func ValidateBundleRelPath(p string) error {
	if p == "" {
		return fmt.Errorf("path is empty")
	}
	// A backslash would carry a Windows-shaped escape past the POSIX checks.
	if strings.ContainsRune(p, '\\') {
		return fmt.Errorf("path %q must use forward slashes", p)
	}
	if strings.HasPrefix(p, "/") || filepath.IsAbs(p) {
		return fmt.Errorf("path %q must be relative to the bundle root", p)
	}
	// filepath.IsAbs and filepath.VolumeName follow host semantics, so on
	// Unix they do not recognise "C:/..." as absolute. A bundle is built on
	// one platform and run on another, so the check has to be portable
	// rather than trusting the machine doing the validating.
	if driveLetter.MatchString(p) {
		return fmt.Errorf("path %q must not name a volume", p)
	}
	if filepath.VolumeName(p) != "" {
		return fmt.Errorf("path %q must not name a volume", p)
	}

	cleaned := path.Clean(p)
	if cleaned == ".." || strings.HasPrefix(cleaned, "../") {
		return fmt.Errorf("path %q escapes the bundle root", p)
	}
	if cleaned == "." {
		return fmt.Errorf("path %q does not name a file", p)
	}
	return nil
}
