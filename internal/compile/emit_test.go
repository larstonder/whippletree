package compile_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/larstonder/whippletree/internal/compile"
	"github.com/larstonder/whippletree/internal/target"
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

// TestBuild_SkipsTSPluginTargetWithVisibleNotice: a ts-plugin target
// (opencode) has no emitter wired into Build yet. Build must not error
// on it, must write it no manifest, hooks file, or vendored target.yaml,
// and must record why in Result.Skipped so its absence from the build
// is visible rather than silent.
func TestBuild_SkipsTSPluginTargetWithVisibleNotice(t *testing.T) {
	dir := writeManifestOnly(t, `
		{"id":"session-start-signal","kind":"lifecycle-signal","event":"session-start",
		 "minTier":"T2","hardRequired":false,"handler":"./handlers/a.sh"}`)
	writeStandIn(t, dir, "handlers/a.sh")

	targets, err := target.LoadDir("../../targets")
	if err != nil {
		t.Fatalf("target.LoadDir: %v", err)
	}
	oc, ok := targets["opencode"]
	if !ok {
		t.Fatal("no opencode target loaded")
	}

	result, err := compile.Build(dir, map[string]*target.Def{"opencode": oc})
	if err != nil {
		t.Fatalf("compile.Build: %v", err)
	}

	if _, ok := result.PerTarget["opencode"]; ok {
		t.Errorf("PerTarget contains %q, want it absent for a skipped target", "opencode")
	}

	if _, err := os.Stat(filepath.Join(dir, "hooks", "opencode.json")); !os.IsNotExist(err) {
		t.Errorf("stat hooks/opencode.json = %v, want it to not exist", err)
	}
	if _, err := os.Stat(filepath.Join(dir, ".whippletree", "targets", "opencode.yaml")); !os.IsNotExist(err) {
		t.Errorf("stat .whippletree/targets/opencode.yaml = %v, want it to not exist", err)
	}

	if len(result.Skipped) != 1 {
		t.Fatalf("Skipped = %+v, want exactly one entry", result.Skipped)
	}
	if result.Skipped[0].Name != "opencode" {
		t.Errorf("Skipped[0].Name = %q, want %q", result.Skipped[0].Name, "opencode")
	}
	if !strings.Contains(result.Skipped[0].Reason, "ts-plugin") || !strings.Contains(result.Skipped[0].Reason, "not yet implemented") {
		t.Errorf("Skipped[0].Reason = %q, want it to mention ts-plugin and not yet implemented", result.Skipped[0].Reason)
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
