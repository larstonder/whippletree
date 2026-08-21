// SPDX-FileCopyrightText: 2026 Lars Tønder
// SPDX-License-Identifier: Apache-2.0

package contract

import (
	"fmt"
	"path"
	"strings"
)

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

// windowsExecExts are the handler extensions Windows launches from a bare
// path. The dispatcher execs a handler directly, with no interpreter, so a
// handler outside this set fails in the loader; and because a spawn failure
// fails open, a hard-required gate would quietly stop enforcing. Rejecting it
// at build time is the only point where the author still sees it.
//
// .ps1 is the omission that surprises people. It is absent from the default
// PATHEXT and is not launchable this way, measured rather than assumed: see
// docs/windows-probe-findings.md. Wrap it in a .cmd.
var windowsExecExts = map[string]bool{".exe": true, ".com": true, ".bat": true, ".cmd": true}

// ValidateWindowsHandler checks that a handlerWindows path names something
// Windows can actually execute.
func ValidateWindowsHandler(p string) error {
	ext := strings.ToLower(path.Ext(p))
	if ext == "" {
		return fmt.Errorf("handlerWindows %q has no extension; Windows launches a handler by extension, so use .cmd, .bat or .exe", p)
	}
	if !windowsExecExts[ext] {
		return fmt.Errorf("handlerWindows %q: Windows cannot launch %s from a bare path; use .cmd, .bat or .exe", p, ext)
	}
	return nil
}
