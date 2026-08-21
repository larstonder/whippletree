package contract

import (
	"fmt"
	"path"
	"path/filepath"
	"strings"
)

// ValidateBundleRelPath checks that p is a safe bundle-relative path: a
// path that, joined onto a bundle root, cannot address anything outside
// that root.
//
// This is the string-level half of the containment rule. The rule is
// enforced twice, deliberately: here at build time, and again in
// internal/dispatch at run time. Build-time validation alone is not
// enough, because the dispatcher reads the *vendored*
// .whippletree/contract.json, which in a bundle whippletree did not
// compile is attacker-controlled input that never passed through
// Validate.
//
// Rejected: empty, absolute (POSIX or Windows), volume-qualified,
// backslash-separated (contract paths are always slash-separated, and
// accepting "\" would let a Windows-shaped escape through the POSIX
// checks), and anything that still climbs out of the root once cleaned.
func ValidateBundleRelPath(p string) error {
	if p == "" {
		return fmt.Errorf("path is empty")
	}
	if strings.ContainsRune(p, '\\') {
		return fmt.Errorf("path %q must use forward slashes", p)
	}
	if strings.HasPrefix(p, "/") || filepath.IsAbs(p) {
		return fmt.Errorf("path %q must be relative to the bundle root", p)
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
