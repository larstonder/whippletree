// SPDX-FileCopyrightText: 2026 Lars Tønder
// SPDX-License-Identifier: Apache-2.0

package contract

// HandlerFor returns the handler to run on goos, or "" when the
// requirement declares none for that platform.
//
// Returning "" rather than falling back to Handler is deliberate. On
// Windows a #!/usr/bin/env bash script is not executable at all, so
// falling back would trade a clear "no handler for this platform" for a
// loader error that looks like a broken install.
func (r Requirement) HandlerFor(goos string) string {
	if goos == "windows" {
		return r.HandlerWindows
	}
	return r.Handler
}
