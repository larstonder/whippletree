// SPDX-FileCopyrightText: 2026 Lars Tønder
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseRunArgs(t *testing.T) {
	cases := []struct {
		name       string
		args       []string
		wantEvent  string
		wantTarget string
		wantErr    string
	}{
		{"event then target", []string{"run", "session-start", "--target", "codex"}, "session-start", "codex", ""},
		{"target then event", []string{"run", "--target", "codex", "session-start"}, "session-start", "codex", ""},
		{"alias event", []string{"run", "file-read", "--target", "claude-code"}, "file-read", "claude-code", ""},
		{"no args", nil, "", "", `expected subcommand "run"`},
		{"wrong subcommand", []string{"dispatch", "session-start"}, "", "", `expected subcommand "run"`},
		{"missing event", []string{"run", "--target", "codex"}, "", "", "missing <event>"},
		{"missing target", []string{"run", "session-start"}, "", "", "missing --target"},
		{"target without value", []string{"run", "session-start", "--target"}, "", "", "--target requires a value"},
		{"unknown flag", []string{"run", "--verbose", "session-start", "--target", "codex"}, "", "", `unknown flag "--verbose"`},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			event, targetName, err := parseRunArgs(tc.args)
			if tc.wantErr != "" {
				if err == nil {
					t.Fatalf("parseRunArgs(%q) = (%q, %q, nil), want an error", tc.args, event, targetName)
				}
				if !strings.Contains(err.Error(), tc.wantErr) {
					t.Errorf("error = %q, want it to mention %q", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseRunArgs(%q) = %v, want no error", tc.args, err)
			}
			if event != tc.wantEvent || targetName != tc.wantTarget {
				t.Errorf("parseRunArgs(%q) = (%q, %q), want (%q, %q)", tc.args, event, targetName, tc.wantEvent, tc.wantTarget)
			}
		})
	}
}

// TestSelfBundleRootIsGrandparent pins the layout assumption every
// installed bundle depends on: whippletree-hook lives at
// <bundleRoot>/bin/whippletree-hook, so the root is two levels up.
func TestSelfBundleRootIsGrandparent(t *testing.T) {
	got, err := selfBundleRoot()
	if err != nil {
		t.Fatalf("selfBundleRoot: %v", err)
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	real, err := filepath.EvalSymlinks(exe)
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Dir(filepath.Dir(real)); got != want {
		t.Errorf("selfBundleRoot() = %q, want %q", got, want)
	}
}

// TestResolveBundleRootPrefersPluginRootVar covers the env-var branch:
// with a vendored target definition next to the binary declaring a
// pluginRoot var, a set var wins over the self-located root.
func TestResolveBundleRootPrefersPluginRootVar(t *testing.T) {
	selfRoot, err := selfBundleRoot()
	if err != nil {
		t.Fatalf("selfBundleRoot: %v", err)
	}

	vendored := filepath.Join(selfRoot, ".whippletree", "targets", "probe-target.yaml")
	if err := os.MkdirAll(filepath.Dir(vendored), 0o755); err != nil {
		t.Skipf("cannot write beside the test binary: %v", err)
	}
	src, err := os.ReadFile("../../targets/codex/target.yaml")
	if err != nil {
		t.Fatalf("read codex target.yaml: %v", err)
	}
	// The loader keys on metadata.name, but resolveBundleRoot looks the
	// file up by filename, so only the path has to match.
	if err := os.WriteFile(vendored, src, 0o644); err != nil {
		t.Skipf("cannot write beside the test binary: %v", err)
	}
	t.Cleanup(func() { os.Remove(vendored) })

	// codex declares pluginRoot: ["PLUGIN_ROOT", "CLAUDE_PLUGIN_ROOT"].
	want := t.TempDir()
	t.Setenv("PLUGIN_ROOT", want)

	got, err := resolveBundleRoot("probe-target")
	if err != nil {
		t.Fatalf("resolveBundleRoot: %v", err)
	}
	if got != want {
		t.Errorf("resolveBundleRoot = %q, want the PLUGIN_ROOT value %q", got, want)
	}
}

// TestResolveBundleRootFallsBackToSelf: with no plugin-root var set,
// the binary's own grandparent is the answer. This is the path that
// carries whippletree on harnesses where the plugin-root variable is
// missing or unexpanded.
func TestResolveBundleRootFallsBackToSelf(t *testing.T) {
	t.Setenv("PLUGIN_ROOT", "")
	t.Setenv("CLAUDE_PLUGIN_ROOT", "")

	got, err := resolveBundleRoot("no-such-target")
	if err != nil {
		t.Fatalf("resolveBundleRoot: %v", err)
	}
	want, err := selfBundleRoot()
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Errorf("resolveBundleRoot = %q, want the self-located root %q", got, want)
	}
}

func TestRunRejectsBadArgsWithUsage(t *testing.T) {
	var stderr bytes.Buffer
	if code := run([]string{"run", "session-start"}, strings.NewReader(""), &stderr); code != 1 {
		t.Fatalf("run = %d, want 1", code)
	}
	if !strings.Contains(stderr.String(), "usage: whippletree-hook run <event> --target <name>") {
		t.Errorf("stderr = %q, want it to include the usage line", stderr.String())
	}
}
