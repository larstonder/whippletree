// SPDX-FileCopyrightText: 2026 Lars Tønder
// SPDX-License-Identifier: Apache-2.0

package contract

import (
	"strings"
	"testing"
)

func TestValidateBundleRelPathAccepts(t *testing.T) {
	for _, p := range []string{
		"./handlers/capture.sh",
		"handlers/capture.sh",
		"./bin/kb",
		"a/b/c/d.sh",
		"./handlers/../handlers/capture.sh", // climbs, but stays inside
		"./weird name/with spaces.sh",
	} {
		if err := ValidateBundleRelPath(p); err != nil {
			t.Errorf("ValidateBundleRelPath(%q) = %v, want nil", p, err)
		}
	}
}

func TestValidateBundleRelPathRejects(t *testing.T) {
	cases := []struct{ name, path, wantErr string }{
		{"empty", "", "empty"},
		{"dot", ".", "does not name a file"},
		{"dotdot", "..", "escapes"},
		{"parent", "../evil.sh", "escapes"},
		{"deep traversal", "../../../../bin/sh", "escapes"},
		{"traversal after a real segment", "./handlers/../../../../bin/sh", "escapes"},
		{"absolute posix", "/bin/sh", "relative"},
		{"absolute posix nested", "/etc/passwd", "relative"},
		{"backslash traversal", `..\..\evil.bat`, "forward slashes"},
		{"backslash separator", `handlers\capture.sh`, "forward slashes"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateBundleRelPath(tc.path)
			if err == nil {
				t.Fatalf("ValidateBundleRelPath(%q) = nil, want an error", tc.path)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("ValidateBundleRelPath(%q) error = %q, want it to mention %q", tc.path, err, tc.wantErr)
			}
		})
	}
}
