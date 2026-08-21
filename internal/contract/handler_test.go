// SPDX-FileCopyrightText: 2026 Lars Tønder
// SPDX-License-Identifier: Apache-2.0

package contract

import (
	"strings"
	"testing"
)

func TestHandlerFor(t *testing.T) {
	both := Requirement{Handler: "./handlers/gate.sh", HandlerWindows: "./handlers/gate.cmd"}
	posixOnly := Requirement{Handler: "./handlers/gate.sh"}

	cases := []struct {
		name string
		req  Requirement
		goos string
		want string
	}{
		{"posix takes handler", both, "darwin", "./handlers/gate.sh"},
		{"linux takes handler", both, "linux", "./handlers/gate.sh"},
		{"windows takes handlerWindows", both, "windows", "./handlers/gate.cmd"},

		// Not a fallback to Handler on purpose: a #!/usr/bin/env bash script
		// is not executable on Windows at all, so falling back would turn a
		// clear "no handler for this platform" into a loader error.
		{"windows with no variant declares nothing", posixOnly, "windows", ""},
		{"posix unaffected by the absent variant", posixOnly, "darwin", "./handlers/gate.sh"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.req.HandlerFor(tc.goos); got != tc.want {
				t.Errorf("HandlerFor(%q) = %q, want %q", tc.goos, got, tc.want)
			}
		})
	}
}

func TestValidateWindowsHandler(t *testing.T) {
	for _, tc := range []struct {
		path    string
		wantErr string
	}{
		{"./handlers/gate.cmd", ""},
		{"./handlers/gate.bat", ""},
		{"./handlers/gate.exe", ""},
		{"./handlers/gate.COM", ""},
		// Measured on windows-latest: exec of a .ps1 or a .sh fails in the
		// loader with "not a valid Win32 application".
		{"./handlers/gate.ps1", "cannot launch .ps1"},
		{"./handlers/gate.sh", "cannot launch .sh"},
		{"./handlers/gate", "has no extension"},
	} {
		err := ValidateWindowsHandler(tc.path)
		switch {
		case tc.wantErr == "" && err != nil:
			t.Errorf("%s: unexpected error: %v", tc.path, err)
		case tc.wantErr != "" && err == nil:
			t.Errorf("%s: expected error containing %q, got nil", tc.path, tc.wantErr)
		case tc.wantErr != "" && !strings.Contains(err.Error(), tc.wantErr):
			t.Errorf("%s: error %q does not contain %q", tc.path, err, tc.wantErr)
		}
	}
}
