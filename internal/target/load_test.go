package target

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/larstonder/whippletree/internal/contract"
	"github.com/larstonder/whippletree/targets"
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

func TestLoad_BackendDefaultsToHooksJSONWhenAbsent(t *testing.T) {
	defs, err := LoadDir("../../targets")
	if err != nil {
		t.Fatalf("LoadDir: %v", err)
	}

	for _, name := range []string{"codex", "claude-code"} {
		def, ok := defs[name]
		if !ok {
			t.Fatalf("expected defs to contain %q, got keys %v", name, keys(defs))
		}
		if def.Backend != "hooks-json" {
			t.Fatalf("expected %s.Backend == %q, got %q", name, "hooks-json", def.Backend)
		}
	}
}

func TestLoad_BackendExplicitTSPluginRoundTrips(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "target.yaml")
	tsPluginYAML := `
apiVersion: whippletree.dev/v1
kind: TargetDefinition
metadata:
  name: ts-plugin-probe
  class: 3
  schemaVersion: "1.0.0"
  testedVersions: ">=0.0.0"
spec:
  backend: ts-plugin
  probe:
    command: ["probe", "--version"]
    versionPattern: '(\d+\.\d+\.\d+)'
  events:
    session-start: { native: "event:session.created", blocking: false }
  toolClassMap:
    read: read
  strictness:
    unknownFieldsFatal: true
  env:
    pluginRoot: []
  capabilities:
    installerPath: true
`
	if err := os.WriteFile(path, []byte(tsPluginYAML), 0o644); err != nil {
		t.Fatalf("write temp yaml: %v", err)
	}

	def, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if def.Backend != "ts-plugin" {
		t.Fatalf("expected Backend == %q, got %q", "ts-plugin", def.Backend)
	}
}

func TestLoad_UnknownBackendErrors(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "target.yaml")
	badBackendYAML := `
apiVersion: whippletree.dev/v1
kind: TargetDefinition
metadata:
  name: bogus-backend
  class: 1
  schemaVersion: "1.0.0"
  testedVersions: ">=0.0.0"
spec:
  backend: not-a-real-backend
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
`
	if err := os.WriteFile(path, []byte(badBackendYAML), 0o644); err != nil {
		t.Fatalf("write temp yaml: %v", err)
	}

	if _, err := Load(path); err == nil {
		t.Fatalf("expected Load to error on unknown backend value, got nil error")
	}
}

func TestLoadDir_OpencodeTargetLoadsWithExpectedValues(t *testing.T) {
	defs, err := LoadDir("../../targets")
	if err != nil {
		t.Fatalf("LoadDir: %v", err)
	}

	oc, ok := defs["opencode"]
	if !ok {
		t.Fatalf("expected defs to contain %q, got keys %v", "opencode", keys(defs))
	}

	if oc.Backend != "ts-plugin" {
		t.Fatalf("expected opencode.Backend == %q, got %q", "ts-plugin", oc.Backend)
	}

	if got := oc.Events["session-start"].Native; got != "event:session.created" {
		t.Fatalf("expected opencode session-start.Native == %q, got %q", "event:session.created", got)
	}
	if got := oc.Events["tool-pre"]; got.Native != "tool.execute.before" || !got.Blocking {
		t.Fatalf("expected opencode tool-pre == {native: tool.execute.before, blocking: true}, got %+v", got)
	}
	if got := oc.Events["tool-post"]; got.Native != "tool.execute.after" || got.Blocking {
		t.Fatalf("expected opencode tool-post == {native: tool.execute.after, blocking: false}, got %+v", got)
	}

	if _, ok := oc.Events["turn-end"]; ok {
		t.Fatalf("expected opencode to declare no turn-end event, got %+v", oc.Events["turn-end"])
	}

	wantToolClassMap := map[string]string{"read": "read", "write": "write", "shell": "bash"}
	for class, want := range wantToolClassMap {
		v, ok := oc.ToolClassMap[class]
		if !ok || v == nil || *v != want {
			t.Fatalf("expected opencode.ToolClassMap[%q] == %q, got ok=%v v=%v", class, want, ok, v)
		}
	}

	if len(oc.Degradations) != 0 {
		t.Fatalf("expected opencode to declare no degradations, got %+v", oc.Degradations)
	}

	if len(oc.PluginRootVars) != 0 {
		t.Fatalf("expected opencode.PluginRootVars to be empty, got %v", oc.PluginRootVars)
	}

	wantProbe := []string{"opencode", "--version"}
	if len(oc.Probe.Command) != len(wantProbe) || oc.Probe.Command[0] != wantProbe[0] || oc.Probe.Command[1] != wantProbe[1] {
		t.Fatalf("expected opencode.Probe.Command == %v, got %v", wantProbe, oc.Probe.Command)
	}

	wantCapabilities := map[string]bool{
		"bundleChannel":      false,
		"installerPath":      true,
		"stopLoopGuard":      false,
		"matcherAlternation": false,
	}
	for k, want := range wantCapabilities {
		if got := oc.Capabilities[k]; got != want {
			t.Fatalf("expected opencode.Capabilities[%q] == %v, got %v", k, want, got)
		}
	}
}

func TestLoad_UnknownKeyAnywhereErrors(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "target.yaml")
	badYAML := `
apiVersion: whippletree.dev/v1
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

// TestLoadDir_ErrorsOnZeroTargets: a targets dir that resolves but
// contains no target.yaml must error, not return an empty map callers
// would read as "compiled for zero targets."
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

// TestLoadFS_MatchesLoadDir confirms LoadFS(targets.FS) returns the
// same set of Defs as LoadDir("../../targets") reading straight off
// disk: same name set, spot-checked fields equal, and RawYAML on each
// embedded Def byte-equal to the on-disk source it was embedded from.
func TestLoadFS_MatchesLoadDir(t *testing.T) {
	fromDir, err := LoadDir("../../targets")
	if err != nil {
		t.Fatalf("LoadDir: %v", err)
	}

	fromFS, err := LoadFS(targets.FS)
	if err != nil {
		t.Fatalf("LoadFS: %v", err)
	}

	if len(fromFS) != len(fromDir) {
		t.Fatalf("LoadFS returned %d defs, LoadDir returned %d: got %v vs %v", len(fromFS), len(fromDir), keys(fromFS), keys(fromDir))
	}
	for name := range fromDir {
		if _, ok := fromFS[name]; !ok {
			t.Fatalf("LoadFS is missing target %q present in LoadDir", name)
		}
	}

	if fromFS["opencode"].Backend != fromDir["opencode"].Backend {
		t.Errorf("opencode.Backend: LoadFS=%q LoadDir=%q", fromFS["opencode"].Backend, fromDir["opencode"].Backend)
	}
	if fromFS["claude-code"].ManifestDir != fromDir["claude-code"].ManifestDir {
		t.Errorf("claude-code.ManifestDir: LoadFS=%q LoadDir=%q", fromFS["claude-code"].ManifestDir, fromDir["claude-code"].ManifestDir)
	}

	for name, def := range fromFS {
		if len(def.RawYAML) == 0 {
			t.Errorf("LoadFS target %q: RawYAML is empty", name)
		}
		want := fromDir[name].RawYAML
		if string(def.RawYAML) != string(want) {
			t.Errorf("LoadFS target %q RawYAML does not byte-match LoadDir's RawYAML", name)
		}
		wantSourcePath := "embedded:" + name + "/target.yaml"
		if def.SourcePath != wantSourcePath {
			t.Errorf("LoadFS target %q SourcePath = %q, want %q", name, def.SourcePath, wantSourcePath)
		}
	}
}

// TestLoadDir_PopulatesRawYAML confirms LoadDir (via Load) fills
// RawYAML with the exact on-disk bytes, not just SourcePath.
func TestLoadDir_PopulatesRawYAML(t *testing.T) {
	defs, err := LoadDir("../../targets")
	if err != nil {
		t.Fatalf("LoadDir: %v", err)
	}

	for name, def := range defs {
		if len(def.RawYAML) == 0 {
			t.Fatalf("target %q: RawYAML is empty", name)
		}
		onDisk, err := os.ReadFile(def.SourcePath)
		if err != nil {
			t.Fatalf("read %s: %v", def.SourcePath, err)
		}
		if string(def.RawYAML) != string(onDisk) {
			t.Errorf("target %q: RawYAML does not byte-match %s", name, def.SourcePath)
		}
	}
}

func keys(m map[string]*Def) []string {
	ks := make([]string, 0, len(m))
	for k := range m {
		ks = append(ks, k)
	}
	return ks
}
