package target

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/larstonder/adapter-sdk/internal/contract"
)

func TestLoadDir_ReturnsBothClass1Targets(t *testing.T) {
	defs, err := LoadDir("../../targets")
	if err != nil {
		t.Fatalf("LoadDir: %v", err)
	}

	codex, ok := defs["codex"]
	if !ok {
		t.Fatalf("expected defs to contain %q, got keys %v", "codex", keys(defs))
	}
	claudeCode, ok := defs["claude-code"]
	if !ok {
		t.Fatalf("expected defs to contain %q, got keys %v", "claude-code", keys(defs))
	}

	// codex declares "read" as absent (null), not simply undeclared.
	v, ok := codex.ToolClassMap["read"]
	if !ok || v != nil {
		t.Fatalf("expected codex.ToolClassMap[\"read\"] to be present with nil value, got ok=%v v=%v", ok, v)
	}

	if got := claudeCode.Events["turn-end"].Native; got != "Stop" {
		t.Fatalf("expected claude-code turn-end.Native == %q, got %q", "Stop", got)
	}

	deg, ok := codex.Degradations["file-read"]
	if !ok {
		t.Fatalf("expected codex to declare a %q degradation", "file-read")
	}
	if deg.Tier != contract.T2 {
		t.Fatalf("expected codex file-read degradation tier == %v, got %v", contract.T2, deg.Tier)
	}
}

func TestLoad_UnknownKeyAnywhereErrors(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "target.yaml")
	badYAML := `
apiVersion: adaptersdk.dev/v1
kind: TargetDefinition
metadata:
  name: bogus
  class: 1
  schemaVersion: "1.0.0"
  testedVersions: ">=0.0.0"
spec:
  discovery:
    manifestDir: ".bogus-plugin"
    hooksKey: "hooks"
    mergeSemantics: replace
  probe:
    command: ["bogus", "--version"]
    versionPattern: '(\d+\.\d+\.\d+)'
  events:
    session-start: { native: SessionStart, blocking: false }
  toolClassMap:
    read: Read
  strictness:
    unknownFieldsFatal: true
  env:
    pluginRoot: ["BOGUS_PLUGIN_ROOT"]
  capabilities:
    bundleChannel: true
  unexpectedTopLevelKey: "this should not be here"
`
	if err := os.WriteFile(path, []byte(badYAML), 0o644); err != nil {
		t.Fatalf("write temp yaml: %v", err)
	}

	if _, err := Load(path); err == nil {
		t.Fatalf("expected Load to error on unknown key, got nil error")
	}
}

// TestLoadDir_ErrorsOnZeroTargets is the regression test for finding 8:
// a targets dir that resolves (e.g. via a wrong --targets-dir, or cwd
// mismatch) but contains no target.yaml files must error loudly rather
// than silently returning an empty map, which upstream callers would
// otherwise treat as "compiled for zero targets, nothing to report."
func TestLoadDir_ErrorsOnZeroTargets(t *testing.T) {
	dir := t.TempDir() // empty: no subdirectories, no target.yaml files

	_, err := LoadDir(dir)
	if err == nil {
		t.Fatal("want error for a targets dir with zero target definitions, got nil")
	}
	if !strings.Contains(err.Error(), "no target definitions found") {
		t.Errorf("error = %q, want it to say no target definitions were found", err.Error())
	}
}

func keys(m map[string]*Def) []string {
	ks := make([]string, 0, len(m))
	for k := range m {
		ks = append(ks, k)
	}
	return ks
}
