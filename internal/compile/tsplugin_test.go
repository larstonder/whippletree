// SPDX-FileCopyrightText: 2026 Lars Tønder
// SPDX-License-Identifier: Apache-2.0

package compile_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"whippletree.dev/internal/compile"
	"whippletree.dev/internal/target"
)

// TestBuild_EmitsTSPluginShim: a ts-plugin target (opencode) gets a
// hooks/<name>.ts shim instead of a manifest+hooks-json pair. The kb
// bundle fixture lands session-start-signal and file-read-signal at
// T1 on opencode and leaves stop-gate Absent (no turn-end mapping), so
// the shim should carry exactly those two dispatch calls.
func TestBuild_EmitsTSPluginShim(t *testing.T) {
	bundleDir := setupBundle(t)

	targets, err := target.LoadDir("../../targets")
	if err != nil {
		t.Fatalf("target.LoadDir: %v", err)
	}
	oc, ok := targets["opencode"]
	if !ok {
		t.Fatal("no opencode target loaded")
	}

	if _, err := compile.Build(bundleDir, map[string]*target.Def{"opencode": oc}); err != nil {
		t.Fatalf("compile.Build: %v", err)
	}

	got, err := os.ReadFile(filepath.Join(bundleDir, "hooks", "opencode.ts"))
	if err != nil {
		t.Fatalf("read hooks/opencode.ts: %v", err)
	}
	want := loadGolden(t, "opencode.ts")
	if string(got) != string(want) {
		t.Errorf("hooks/opencode.ts mismatch\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}

	if n := strings.Count(string(got), "__WHIPPLETREE_HOOK__"); n != 1 {
		t.Errorf("__WHIPPLETREE_HOOK__ occurs %d times in hooks/opencode.ts, want exactly 1", n)
	}

	// No manifest pair: opencode has no manifest.
	if _, err := os.Stat(filepath.Join(bundleDir, ".opencode-plugin")); !os.IsNotExist(err) {
		t.Errorf("stat .opencode-plugin = %v, want it to not exist", err)
	}
	if _, err := os.Stat(filepath.Join(bundleDir, "hooks", "opencode.json")); !os.IsNotExist(err) {
		t.Errorf("stat hooks/opencode.json = %v, want it to not exist", err)
	}

	// The vendored target.yaml is still written for a ts-plugin target.
	if _, err := os.Stat(filepath.Join(bundleDir, ".whippletree", "targets", "opencode.yaml")); err != nil {
		t.Errorf("expected vendored .whippletree/targets/opencode.yaml to exist: %v", err)
	}
}
