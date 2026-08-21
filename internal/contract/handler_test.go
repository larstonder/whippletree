// SPDX-FileCopyrightText: 2026 Lars Tønder
// SPDX-License-Identifier: Apache-2.0

package contract

import "testing"

func TestHandlerFor(t *testing.T) {
	both := Requirement{Handler: "./handlers/gate.sh", HandlerWindows: "./handlers/gate.ps1"}
	posixOnly := Requirement{Handler: "./handlers/gate.sh"}

	cases := []struct {
		name string
		req  Requirement
		goos string
		want string
	}{
		{"posix takes handler", both, "darwin", "./handlers/gate.sh"},
		{"linux takes handler", both, "linux", "./handlers/gate.sh"},
		{"windows takes handlerWindows", both, "windows", "./handlers/gate.ps1"},

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
