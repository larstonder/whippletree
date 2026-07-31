package compile_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/larstonder/whippletree/internal/compile"
	"github.com/larstonder/whippletree/internal/target"
	"github.com/larstonder/whippletree/targets"
)

// setupBundle copies the kb-shaped fixture into a fresh temp bundle dir
// as plugin.json, returning the bundle dir. It also creates stand-in
// files for every handler/path the fixture's contract names, since
// compile.Build stats them.
func setupBundle(t *testing.T) string {
	t.Helper()

	src, err := os.ReadFile("testdata/bundle-plugin.json")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "plugin.json"), src, 0o644); err != nil {
		t.Fatalf("write bundle plugin.json: %v", err)
	}

	for _, rel := range []string{"handlers/capture.sh", "handlers/pull.sh", "handlers/log-read.sh", "bin/kb"} {
		writeStandIn(t, dir, rel)
	}
	return dir
}

// writeStandIn creates an executable placeholder file at dir/rel, for
// requirements whose handler/path fields compile.Build stats but whose
// actual content this test never invokes.
func writeStandIn(t *testing.T, dir, rel string) {
	t.Helper()
	p := filepath.Join(dir, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatalf("mkdir for %s: %v", rel, err)
	}
	if err := os.WriteFile(p, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatalf("write stand-in %s: %v", rel, err)
	}
}

func loadGolden(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", "golden", name))
	if err != nil {
		t.Fatalf("read golden %s: %v", name, err)
	}
	return b
}

func TestBuild_EmitsPerTargetVariants(t *testing.T) {
	bundleDir := setupBundle(t)

	targets, err := target.LoadDir("../../targets")
	if err != nil {
		t.Fatalf("target.LoadDir: %v", err)
	}

	if _, err := compile.Build(bundleDir, targets); err != nil {
		t.Fatalf("compile.Build: %v", err)
	}

	// hooks files byte-equal their goldens.
	for _, tc := range []struct {
		targetName string
		golden     string
	}{
		{"codex", "codex.json"},
		{"claude-code", "claude-code.json"},
	} {
		got, err := os.ReadFile(filepath.Join(bundleDir, "hooks", tc.targetName+".json"))
		if err != nil {
			t.Fatalf("read hooks/%s.json: %v", tc.targetName, err)
		}
		want := loadGolden(t, tc.golden)
		if string(got) != string(want) {
			t.Errorf("hooks/%s.json mismatch\n--- got ---\n%s\n--- want ---\n%s", tc.targetName, got, want)
		}
	}

	// manifest hooks keys point at the right hooks file per target.
	for _, tc := range []struct {
		manifestDir string
		wantHooks   string
	}{
		{".claude-plugin", "./hooks/claude-code.json"},
		{".codex-plugin", "./hooks/codex.json"},
	} {
		raw, err := os.ReadFile(filepath.Join(bundleDir, tc.manifestDir, "plugin.json"))
		if err != nil {
			t.Fatalf("read %s/plugin.json: %v", tc.manifestDir, err)
		}
		var m map[string]any
		if err := json.Unmarshal(raw, &m); err != nil {
			t.Fatalf("unmarshal %s/plugin.json: %v", tc.manifestDir, err)
		}
		if got := m["hooks"]; got != tc.wantHooks {
			t.Errorf("%s/plugin.json[\"hooks\"] = %v, want %v", tc.manifestDir, got, tc.wantHooks)
		}
		if m["name"] != "kb-shaped" {
			t.Errorf("%s/plugin.json[\"name\"] = %v, want carried-over %q", tc.manifestDir, m["name"], "kb-shaped")
		}
	}

	// hooks/hooks.json must never exist.
	if _, err := os.Stat(filepath.Join(bundleDir, "hooks", "hooks.json")); err == nil {
		t.Error("hooks/hooks.json exists, want it to never be emitted")
	} else if !os.IsNotExist(err) {
		t.Errorf("stat hooks/hooks.json: unexpected error %v", err)
	}

	// codex hooks file top level has only the "hooks" key.
	codexRaw, err := os.ReadFile(filepath.Join(bundleDir, "hooks", "codex.json"))
	if err != nil {
		t.Fatalf("read hooks/codex.json: %v", err)
	}
	var codexTop map[string]any
	if err := json.Unmarshal(codexRaw, &codexTop); err != nil {
		t.Fatalf("unmarshal hooks/codex.json: %v", err)
	}
	if len(codexTop) != 1 {
		t.Fatalf("codex hooks top-level keys = %v, want exactly {\"hooks\"}", keysOf(codexTop))
	}
	if _, ok := codexTop["hooks"]; !ok {
		t.Fatalf("codex hooks top-level keys = %v, want exactly {\"hooks\"}", keysOf(codexTop))
	}

	// vendored .whippletree files exist.
	for _, p := range []string{
		filepath.Join(".whippletree", "contract.json"),
		filepath.Join(".whippletree", "targets", "codex.yaml"),
	} {
		if _, err := os.Stat(filepath.Join(bundleDir, p)); err != nil {
			t.Errorf("expected vendored file %s to exist: %v", p, err)
		}
	}
}

// TestBuild_VendoredTargetYAMLMatchesEmbeddedSource confirms that when
// Build vendors a target's target.yaml into
// .whippletree/targets/<name>.yaml, it writes exactly the bytes that
// were embedded into the binary (Def.RawYAML), not a re-read of some
// on-disk path, by loading targets through target.LoadFS(targets.FS)
// (no disk-backed SourcePath at all) and comparing the vendored output
// straight against targets.FS's own bytes.
func TestBuild_VendoredTargetYAMLMatchesEmbeddedSource(t *testing.T) {
	bundleDir := setupBundle(t)

	defs, err := target.LoadFS(targets.FS)
	if err != nil {
		t.Fatalf("target.LoadFS: %v", err)
	}

	if _, err := compile.Build(bundleDir, defs); err != nil {
		t.Fatalf("compile.Build: %v", err)
	}

	for _, name := range []string{"codex", "claude-code"} {
		got, err := os.ReadFile(filepath.Join(bundleDir, ".whippletree", "targets", name+".yaml"))
		if err != nil {
			t.Fatalf("read vendored %s.yaml: %v", name, err)
		}
		want, err := targets.FS.ReadFile(name + "/target.yaml")
		if err != nil {
			t.Fatalf("read embedded %s/target.yaml: %v", name, err)
		}
		if string(got) != string(want) {
			t.Errorf("vendored %s.yaml does not byte-match embedded source", name)
		}
	}
}

func keysOf(m map[string]any) []string {
	ks := make([]string, 0, len(m))
	for k := range m {
		ks = append(ks, k)
	}
	return ks
}

// writeManifestOnly writes a plugin.json with the given contract JSON
// body (the extensions.dev.whippletree.v1 payload) to a fresh temp
// bundle dir, without any stand-in handler/path files. Callers that
// need those files present create them explicitly.
func writeManifestOnly(t *testing.T, requiresJSON string) string {
	t.Helper()
	dir := t.TempDir()
	manifest := `{"name":"x","extensions":{"dev.whippletree.v1":{"contractVersion":"1.0.0","requires":[` + requiresJSON + `]}}}`
	if err := os.WriteFile(filepath.Join(dir, "plugin.json"), []byte(manifest), 0o644); err != nil {
		t.Fatalf("write plugin.json: %v", err)
	}
	return dir
}

// TestBuild_DedupesDuplicateHookEntries: two requirements resolving to
// the same primitive (here, two lifecycle-signal requirements both on
// session-start) must produce exactly one SessionStart entry in the
// hooks file; duplicates would make the harness run the same handler
// invocation twice per firing.
func TestBuild_DedupesDuplicateHookEntries(t *testing.T) {
	dir := writeManifestOnly(t, `
		{"id":"session-start-signal-a","kind":"lifecycle-signal","event":"session-start",
		 "minTier":"T2","hardRequired":false,"handler":"./handlers/a.sh"},
		{"id":"session-start-signal-b","kind":"lifecycle-signal","event":"session-start",
		 "minTier":"T2","hardRequired":false,"handler":"./handlers/b.sh"}`)
	writeStandIn(t, dir, "handlers/a.sh")
	writeStandIn(t, dir, "handlers/b.sh")

	targets, err := target.LoadDir("../../targets")
	if err != nil {
		t.Fatalf("target.LoadDir: %v", err)
	}
	if _, err := compile.Build(dir, targets); err != nil {
		t.Fatalf("compile.Build: %v", err)
	}

	raw, err := os.ReadFile(filepath.Join(dir, "hooks", "claude-code.json"))
	if err != nil {
		t.Fatalf("read hooks/claude-code.json: %v", err)
	}
	var doc struct {
		Hooks map[string][]json.RawMessage `json:"hooks"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("unmarshal hooks file: %v", err)
	}
	if got := len(doc.Hooks["SessionStart"]); got != 1 {
		t.Fatalf("SessionStart entries = %d, want exactly 1 (deduped); raw:\n%s", got, raw)
	}
}

func TestBuild_ErrorsOnMissingHandler(t *testing.T) {
	dir := writeManifestOnly(t, `
		{"id":"session-start-signal","kind":"lifecycle-signal","event":"session-start",
		 "minTier":"T2","hardRequired":false,"handler":"./handlers/typo.sh"}`)

	targets, err := target.LoadDir("../../targets")
	if err != nil {
		t.Fatalf("target.LoadDir: %v", err)
	}

	_, err = compile.Build(dir, targets)
	if err == nil {
		t.Fatal("want error for missing handler file, got nil")
	}
	if !strings.Contains(err.Error(), "session-start-signal") || !strings.Contains(err.Error(), "handlers/typo.sh") {
		t.Errorf("error = %q, want it to name the requirement id and the missing path", err.Error())
	}
}

func TestBuild_ErrorsOnMissingExecutablePath(t *testing.T) {
	dir := writeManifestOnly(t, `
		{"id":"bin-reachable","kind":"executable-path","minTier":"T1",
		 "hardRequired":true,"path":"./bin/missing"}`)

	targets, err := target.LoadDir("../../targets")
	if err != nil {
		t.Fatalf("target.LoadDir: %v", err)
	}

	_, err = compile.Build(dir, targets)
	if err == nil {
		t.Fatal("want error for missing executable-path file, got nil")
	}
	if !strings.Contains(err.Error(), "bin-reachable") || !strings.Contains(err.Error(), "bin/missing") {
		t.Errorf("error = %q, want it to name the requirement id and the missing path", err.Error())
	}
}

func TestBuild_RejectsHooksAsTargetName(t *testing.T) {
	dir := writeManifestOnly(t, `
		{"id":"session-start-signal","kind":"lifecycle-signal","event":"session-start",
		 "minTier":"T2","hardRequired":false,"handler":"./handlers/a.sh"}`)
	writeStandIn(t, dir, "handlers/a.sh")

	targets := map[string]*target.Def{
		"hooks": {
			Name:           "hooks",
			ManifestDir:    ".hooks-plugin",
			Events:         map[string]target.EventMapping{"session-start": {Native: "SessionStart"}},
			PluginRootVars: []string{"PLUGIN_ROOT"},
			SourcePath:     filepath.Join("..", "..", "targets", "codex", "target.yaml"),
		},
	}

	if _, err := compile.Build(dir, targets); err == nil {
		t.Fatal("want error for target named \"hooks\", got nil")
	} else if !strings.Contains(err.Error(), `"hooks"`) {
		t.Errorf("error = %q, want it to name the reserved target name", err.Error())
	}
}
