package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeFile(t *testing.T, dir, rel, body string, perm os.FileMode) {
	t.Helper()
	p := filepath.Join(dir, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatalf("mkdir for %s: %v", rel, err)
	}
	if err := os.WriteFile(p, []byte(body), perm); err != nil {
		t.Fatalf("write %s: %v", rel, err)
	}
}

// fakeTargetYAML is a minimal, valid target.yaml body. When
// blockingLoopGuard is empty, turn-end is mapped blocking with no
// loop-guard field, so a loopGuardRequired blocking-gate requirement
// against it lands Absent (and, if hardRequired, REFUSE).
const fakeTargetYAML = `
apiVersion: whippletree.dev/v1
kind: TargetDefinition
metadata:
  name: faketarget
  class: 1
  schemaVersion: "1.0.0"
  testedVersions: ">=0.0.0"
spec:
  discovery:
    manifestDir: ".fake-plugin"
    hooksKey: "hooks"
    mergeSemantics: replace
  probe:
    command: ["definitely-not-a-real-binary-xyz"]
    versionPattern: '(\d+\.\d+\.\d+)'
  events:
    session-start: { native: SessionStart, blocking: false }
    turn-end:      { native: Stop, blocking: true }
  toolClassMap: {}
  strictness:
    unknownFieldsFatal: false
  env:
    pluginRoot: ["FAKE_PLUGIN_ROOT"]
  capabilities:
    bundleChannel: true
`

func writeFakeTargetsDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	writeFile(t, dir, filepath.Join("faketarget", "target.yaml"), fakeTargetYAML, 0o644)
	return dir
}

// TestRunBuild_RefusesAndExitsNonZero: a build whose contract has a
// hard-required requirement the target can't satisfy (here, a
// loopGuardRequired blocking-gate against a target with no loop-guard
// field) must exit 1 and name the refusing target/requirement, not
// silently exit 0.
func TestRunBuild_RefusesAndExitsNonZero(t *testing.T) {
	bundleDir := t.TempDir()
	writeFile(t, bundleDir, "plugin.json", `{
		"name": "x",
		"extensions": { "dev.whippletree.v1": {
			"contractVersion": "1.0.0",
			"requires": [
				{"id":"stop-gate","kind":"blocking-gate","event":"turn-end","minTier":"T1",
				 "hardRequired":true,"loopGuardRequired":true,"handler":"./handlers/capture.sh"}
			]}}}`, 0o644)
	writeFile(t, bundleDir, "handlers/capture.sh", "#!/bin/sh\n", 0o755)

	targetsDirPath := writeFakeTargetsDir(t)

	var stdout, stderr bytes.Buffer
	code := run([]string{"build", bundleDir, "--targets-dir", targetsDirPath, "--allow-missing-dispatcher"}, &stdout, &stderr)

	if code != 1 {
		t.Fatalf("run(build) = %d, want 1 (stdout=%s stderr=%s)", code, stdout.String(), stderr.String())
	}
	if !strings.Contains(stderr.String(), "faketarget") || !strings.Contains(stderr.String(), "stop-gate") {
		t.Errorf("stderr = %q, want it to name the refusing target and requirement", stderr.String())
	}
}

// TestRunBuild_AllowRefuseContinues confirms --allow-refuse downgrades
// the same refusal to a warning and an exit 0.
func TestRunBuild_AllowRefuseContinues(t *testing.T) {
	bundleDir := t.TempDir()
	writeFile(t, bundleDir, "plugin.json", `{
		"name": "x",
		"extensions": { "dev.whippletree.v1": {
			"contractVersion": "1.0.0",
			"requires": [
				{"id":"stop-gate","kind":"blocking-gate","event":"turn-end","minTier":"T1",
				 "hardRequired":true,"loopGuardRequired":true,"handler":"./handlers/capture.sh"}
			]}}}`, 0o644)
	writeFile(t, bundleDir, "handlers/capture.sh", "#!/bin/sh\n", 0o755)

	targetsDirPath := writeFakeTargetsDir(t)

	var stdout, stderr bytes.Buffer
	code := run([]string{"build", bundleDir, "--targets-dir", targetsDirPath, "--allow-missing-dispatcher", "--allow-refuse"}, &stdout, &stderr)

	if code != 0 {
		t.Fatalf("run(build --allow-refuse) = %d, want 0 (stdout=%s stderr=%s)", code, stdout.String(), stderr.String())
	}
	if !strings.Contains(stderr.String(), "continuing past refusals") {
		t.Errorf("stderr = %q, want a note that refusals were allowed through", stderr.String())
	}
}

// TestRunBuild_HappyPathExitsZero exercises a satisfiable contract
// against the repo's real class-1 targets.
func TestRunBuild_HappyPathExitsZero(t *testing.T) {
	bundleDir := t.TempDir()
	writeFile(t, bundleDir, "plugin.json", `{
		"name": "x",
		"extensions": { "dev.whippletree.v1": {
			"contractVersion": "1.0.0",
			"requires": [
				{"id":"session-start-signal","kind":"lifecycle-signal","event":"session-start",
				 "minTier":"T2","hardRequired":false,"handler":"./handlers/pull.sh"}
			]}}}`, 0o644)
	writeFile(t, bundleDir, "handlers/pull.sh", "#!/bin/sh\n", 0o755)

	var stdout, stderr bytes.Buffer
	code := run([]string{"build", bundleDir, "--targets-dir", "../../targets", "--allow-missing-dispatcher"}, &stdout, &stderr)

	if code != 0 {
		t.Fatalf("run(build) = %d, want 0 (stdout=%s stderr=%s)", code, stdout.String(), stderr.String())
	}
	if strings.Contains(stdout.String(), "refuse") && !strings.Contains(stdout.String(), "0 refuse") {
		t.Errorf("stdout = %q, want no refusals", stdout.String())
	}
}

// TestRunBuild_ForeignCWDUsesEmbeddedTargets confirms that omitting
// --targets-dir entirely still succeeds when the process's current
// working directory has no "targets" subdirectory at all: the CLI
// falls back to the embedded targets package rather than the old
// cwd-relative default.
func TestRunBuild_ForeignCWDUsesEmbeddedTargets(t *testing.T) {
	bundleDir := t.TempDir()
	writeFile(t, bundleDir, "plugin.json", `{
		"name": "x",
		"extensions": { "dev.whippletree.v1": {
			"contractVersion": "1.0.0",
			"requires": [
				{"id":"session-start-signal","kind":"lifecycle-signal","event":"session-start",
				 "minTier":"T2","hardRequired":false,"handler":"./handlers/pull.sh"}
			]}}}`, 0o644)
	writeFile(t, bundleDir, "handlers/pull.sh", "#!/bin/sh\n", 0o755)

	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	foreignDir := t.TempDir() // deliberately has no "targets" subdirectory
	if err := os.Chdir(foreignDir); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := os.Chdir(cwd); err != nil {
			t.Fatal(err)
		}
	}()

	var stdout, stderr bytes.Buffer
	code := run([]string{"build", bundleDir, "--allow-missing-dispatcher"}, &stdout, &stderr)

	if code != 0 {
		t.Fatalf("run(build) in foreign cwd without --targets-dir = %d, want 0 (stdout=%s stderr=%s)", code, stdout.String(), stderr.String())
	}
}

// TestRunBuild_TargetsDirWithZeroTargetsErrors: --targets-dir pointing
// at a directory with no target.yaml files must be a build error, not
// a silent "compiled for zero targets."
func TestRunBuild_TargetsDirWithZeroTargetsErrors(t *testing.T) {
	bundleDir := t.TempDir()
	writeFile(t, bundleDir, "plugin.json", `{"name":"x","extensions":{"dev.whippletree.v1":{"contractVersion":"1.0.0","requires":[]}}}`, 0o644)

	emptyTargetsDir := t.TempDir()

	var stdout, stderr bytes.Buffer
	code := run([]string{"build", bundleDir, "--targets-dir", emptyTargetsDir}, &stdout, &stderr)

	if code != 1 {
		t.Fatalf("run(build) = %d, want 1 (stdout=%s stderr=%s)", code, stdout.String(), stderr.String())
	}
	if !strings.Contains(stderr.String(), "no target definitions found") {
		t.Errorf("stderr = %q, want it to say no target definitions were found", stderr.String())
	}
}

// TestEnsureDispatcher_AlreadyPresent covers the no-op case: the
// binary is already there, so ensureDispatcher does nothing.
func TestEnsureDispatcher_AlreadyPresent(t *testing.T) {
	bundleDir := t.TempDir()
	writeFile(t, bundleDir, "bin/whippletree-hook", "already here", 0o755)

	var stderr bytes.Buffer
	if err := ensureDispatcher(bundleDir, false, &stderr); err != nil {
		t.Fatalf("ensureDispatcher = %v, want nil (already present)", err)
	}
	if stderr.String() != "" {
		t.Errorf("stderr = %q, want empty", stderr.String())
	}
}

// TestEnsureDispatcher_MissingNamesBuildCommand: a build whose bundle
// has no bin/whippletree-hook, and no sibling binary to copy from (the
// normal case under `go test`), must fail with the exact command to
// fix it, naming the bundle's bin path.
func TestEnsureDispatcher_MissingNamesBuildCommand(t *testing.T) {
	bundleDir := t.TempDir()

	var stderr bytes.Buffer
	err := ensureDispatcher(bundleDir, false, &stderr)
	if err == nil {
		t.Fatal("ensureDispatcher = nil, want an error naming the missing binary")
	}
	wantPath := filepath.Join(bundleDir, "bin", "whippletree-hook")
	if !strings.Contains(err.Error(), "go build -o "+wantPath) {
		t.Errorf("error = %q, want it to name the exact go build command for %s", err.Error(), wantPath)
	}
}

// TestEnsureDispatcher_AllowMissingDowngradesToWarning confirms
// --allow-missing-dispatcher turns the same failure into a warning
// with a nil error.
func TestEnsureDispatcher_AllowMissingDowngradesToWarning(t *testing.T) {
	bundleDir := t.TempDir()

	var stderr bytes.Buffer
	if err := ensureDispatcher(bundleDir, true, &stderr); err != nil {
		t.Fatalf("ensureDispatcher(allowMissing=true) = %v, want nil", err)
	}
	if !strings.Contains(stderr.String(), "warning") {
		t.Errorf("stderr = %q, want a warning about the missing dispatcher", stderr.String())
	}
}

// TestRunPreflight_ProbeFailureMessageIsDistinct: a Probe failure (the
// harness's version command itself failing) must be reported with its
// own "probe failed" message, not conflated with Check's fail-closed
// error.
func TestRunPreflight_ProbeFailureMessageIsDistinct(t *testing.T) {
	bundleDir := t.TempDir()
	writeFile(t, bundleDir, "plugin.json", `{"name":"x","extensions":{"dev.whippletree.v1":{"contractVersion":"1.0.0","requires":[]}}}`, 0o644)

	targetsDirPath := writeFakeTargetsDir(t) // faketarget's probe command does not exist

	var stdout, stderr bytes.Buffer
	code := run([]string{"preflight", bundleDir, "--target", "faketarget", "--targets-dir", targetsDirPath}, &stdout, &stderr)

	if code != 1 {
		t.Fatalf("run(preflight) = %d, want 1 (stdout=%s stderr=%s)", code, stdout.String(), stderr.String())
	}
	if !strings.Contains(stderr.String(), "preflight: probe failed:") {
		t.Errorf("stderr = %q, want the distinct probe-failure message", stderr.String())
	}
}

// tsPluginContractPluginJSON is a minimal, satisfiable plugin.json for
// a bundle called "acme-tool" exercised against the opencode (ts-plugin
// backend) target: a single non-hard lifecycle-signal on session-start,
// which opencode maps natively, so it always lands SATISFY.
func tsPluginContractPluginJSON(bundleName string) string {
	return `{
		"name": "` + bundleName + `",
		"extensions": { "dev.whippletree.v1": {
			"contractVersion": "1.0.0",
			"requires": [
				{"id":"session-start-signal","kind":"lifecycle-signal","event":"session-start",
				 "minTier":"T2","hardRequired":false,"handler":"./handlers/pull.sh"}
			]}}}`
}

// refusingTurnEndPluginJSON is a plugin.json whose single requirement
// hard-requires a loop-guarded blocking-gate on turn-end. opencode
// declares no turn-end event mapping at all, so this requirement lands
// Absent+hard, i.e. REFUSE.
const refusingTurnEndPluginJSON = `{
	"name": "acme-tool",
	"extensions": { "dev.whippletree.v1": {
		"contractVersion": "1.0.0",
		"requires": [
			{"id":"stop-gate","kind":"blocking-gate","event":"turn-end","minTier":"T1",
			 "hardRequired":true,"loopGuardRequired":true,"handler":"./handlers/capture.sh"}
		]}}}`

const generatedByMarkerLine = "// Generated by whippletree. Do not hand-edit; regenerate with `whippletree build`."

// fakeCompiledOpencodeShim mimics what `whippletree build` would have
// already written to <bundleDir>/hooks/opencode.ts: the generated-by
// marker as its first line, and the literal HOOK placeholder install
// is responsible for resolving.
const fakeCompiledOpencodeShim = generatedByMarkerLine + "\n" + `const HOOK = "__WHIPPLETREE_HOOK__"
const TARGET = "opencode"
`

func realTargetsDir() string { return filepath.Join("..", "..", "targets") }

// TestRunInstall_TSPluginPlacesResolvedShim: a successful install
// against a ts-plugin target reads the compiled hooks/<target>.ts,
// replaces the __WHIPPLETREE_HOOK__ placeholder with the absolute
// dispatcher path, and writes it to
// <project>/.opencode/plugin/whippletree-<bundle name>.ts.
func TestRunInstall_TSPluginPlacesResolvedShim(t *testing.T) {
	bundleDir := t.TempDir()
	writeFile(t, bundleDir, "plugin.json", tsPluginContractPluginJSON("acme-tool"), 0o644)
	writeFile(t, bundleDir, "bin/whippletree-hook", "#!/bin/sh\n", 0o755)
	writeFile(t, bundleDir, "hooks/opencode.ts", fakeCompiledOpencodeShim, 0o644)

	projectDir := t.TempDir()

	var stdout, stderr bytes.Buffer
	code := run([]string{
		"install", bundleDir,
		"--target", "opencode",
		"--project", projectDir,
		"--targets-dir", realTargetsDir(),
		"--assume-version", "1.20.0",
	}, &stdout, &stderr)

	if code != 0 {
		t.Fatalf("run(install) = %d, want 0 (stdout=%s stderr=%s)", code, stdout.String(), stderr.String())
	}

	dest := filepath.Join(projectDir, ".opencode", "plugin", "whippletree-acme-tool.ts")
	body, err := os.ReadFile(dest)
	if err != nil {
		t.Fatalf("read placed shim: %v", err)
	}

	absBundleDir, _ := filepath.Abs(bundleDir)
	wantHookPath := filepath.Join(absBundleDir, "bin", "whippletree-hook")
	if !strings.Contains(string(body), wantHookPath) {
		t.Errorf("placed shim = %q, want it to contain the resolved dispatcher path %q", body, wantHookPath)
	}
	if strings.Contains(string(body), "__WHIPPLETREE_HOOK__") {
		t.Errorf("placed shim = %q, want the placeholder fully replaced", body)
	}

	if _, err := os.Stat(filepath.Join(bundleDir, ".whippletree", "install-state.json")); err != nil {
		t.Errorf("expected install-state.json to be written: %v", err)
	}
}

// TestRunInstall_ZeroPlaceholderOccurrencesErrors: a compiled shim
// that's missing the __WHIPPLETREE_HOOK__ placeholder entirely (stale
// or hand-corrupted hooks/<target>.ts) must fail loudly rather than
// installing an unresolved file unchanged with exit 0.
func TestRunInstall_ZeroPlaceholderOccurrencesErrors(t *testing.T) {
	bundleDir := t.TempDir()
	writeFile(t, bundleDir, "plugin.json", tsPluginContractPluginJSON("acme-tool"), 0o644)
	writeFile(t, bundleDir, "bin/whippletree-hook", "#!/bin/sh\n", 0o755)
	noPlaceholderShim := generatedByMarkerLine + "\n" + `const HOOK = "/some/stale/absolute/path"
const TARGET = "opencode"
`
	writeFile(t, bundleDir, "hooks/opencode.ts", noPlaceholderShim, 0o644)

	projectDir := t.TempDir()

	var stdout, stderr bytes.Buffer
	code := run([]string{
		"install", bundleDir,
		"--target", "opencode",
		"--project", projectDir,
		"--targets-dir", realTargetsDir(),
		"--assume-version", "1.20.0",
	}, &stdout, &stderr)

	if code != 1 {
		t.Fatalf("run(install) = %d, want 1 (stdout=%s stderr=%s)", code, stdout.String(), stderr.String())
	}
	if !strings.Contains(stderr.String(), "0") {
		t.Errorf("stderr = %q, want it to name the occurrence count found (0)", stderr.String())
	}
	if _, err := os.Stat(filepath.Join(projectDir, ".opencode")); !os.IsNotExist(err) {
		t.Errorf("expected no .opencode dir to be created, stat err = %v", err)
	}
}

// TestRunInstall_TwoPlaceholderOccurrencesErrors: a compiled shim
// carrying the placeholder twice (a future compiler bug, or a
// hand-tampered file) must fail loudly rather than resolving only the
// first occurrence and shipping the literal placeholder in the second.
func TestRunInstall_TwoPlaceholderOccurrencesErrors(t *testing.T) {
	bundleDir := t.TempDir()
	writeFile(t, bundleDir, "plugin.json", tsPluginContractPluginJSON("acme-tool"), 0o644)
	writeFile(t, bundleDir, "bin/whippletree-hook", "#!/bin/sh\n", 0o755)
	doublePlaceholderShim := generatedByMarkerLine + "\n" + `const HOOK = "__WHIPPLETREE_HOOK__"
const HOOK2 = "__WHIPPLETREE_HOOK__"
const TARGET = "opencode"
`
	writeFile(t, bundleDir, "hooks/opencode.ts", doublePlaceholderShim, 0o644)

	projectDir := t.TempDir()

	var stdout, stderr bytes.Buffer
	code := run([]string{
		"install", bundleDir,
		"--target", "opencode",
		"--project", projectDir,
		"--targets-dir", realTargetsDir(),
		"--assume-version", "1.20.0",
	}, &stdout, &stderr)

	if code != 1 {
		t.Fatalf("run(install) = %d, want 1 (stdout=%s stderr=%s)", code, stdout.String(), stderr.String())
	}
	if !strings.Contains(stderr.String(), "2") {
		t.Errorf("stderr = %q, want it to name the occurrence count found (2)", stderr.String())
	}
	if _, err := os.Stat(filepath.Join(projectDir, ".opencode")); !os.IsNotExist(err) {
		t.Errorf("expected no .opencode dir to be created, stat err = %v", err)
	}
}

// TestRunInstall_RefuseExitsNonZeroAndPlacesNothing: a REFUSE must
// print the preflight report, exit 1, and place nothing: no plugin
// file, no install-state.json.
func TestRunInstall_RefuseExitsNonZeroAndPlacesNothing(t *testing.T) {
	bundleDir := t.TempDir()
	writeFile(t, bundleDir, "plugin.json", refusingTurnEndPluginJSON, 0o644)

	projectDir := t.TempDir()

	var stdout, stderr bytes.Buffer
	code := run([]string{
		"install", bundleDir,
		"--target", "opencode",
		"--project", projectDir,
		"--targets-dir", realTargetsDir(),
		"--assume-version", "1.20.0",
	}, &stdout, &stderr)

	if code != 1 {
		t.Fatalf("run(install) = %d, want 1 (stdout=%s stderr=%s)", code, stdout.String(), stderr.String())
	}
	if !strings.Contains(stdout.String(), "REFUSE") {
		t.Errorf("stdout = %q, want it to show the REFUSE verdict", stdout.String())
	}

	if _, err := os.Stat(filepath.Join(projectDir, ".opencode")); !os.IsNotExist(err) {
		t.Errorf("expected no .opencode dir to be created on REFUSE, stat err = %v", err)
	}
	if _, err := os.Stat(filepath.Join(bundleDir, ".whippletree", "install-state.json")); !os.IsNotExist(err) {
		t.Errorf("expected no install-state.json to be written on REFUSE, stat err = %v", err)
	}
}

// TestRunInstall_RefusesToOverwriteNonMarkerFile: a pre-existing
// destination file that wasn't generated by whippletree (no marker
// first line) must not be clobbered.
func TestRunInstall_RefusesToOverwriteNonMarkerFile(t *testing.T) {
	bundleDir := t.TempDir()
	writeFile(t, bundleDir, "plugin.json", tsPluginContractPluginJSON("acme-tool"), 0o644)
	writeFile(t, bundleDir, "bin/whippletree-hook", "#!/bin/sh\n", 0o755)
	writeFile(t, bundleDir, "hooks/opencode.ts", fakeCompiledOpencodeShim, 0o644)

	projectDir := t.TempDir()
	const handWritten = "hand-written, do not touch\n"
	writeFile(t, projectDir, filepath.Join(".opencode", "plugin", "whippletree-acme-tool.ts"), handWritten, 0o644)

	var stdout, stderr bytes.Buffer
	code := run([]string{
		"install", bundleDir,
		"--target", "opencode",
		"--project", projectDir,
		"--targets-dir", realTargetsDir(),
		"--assume-version", "1.20.0",
	}, &stdout, &stderr)

	if code != 1 {
		t.Fatalf("run(install) = %d, want 1 (stdout=%s stderr=%s)", code, stdout.String(), stderr.String())
	}
	if !strings.Contains(stderr.String(), "overwrite") {
		t.Errorf("stderr = %q, want it to mention the overwrite refusal", stderr.String())
	}

	dest := filepath.Join(projectDir, ".opencode", "plugin", "whippletree-acme-tool.ts")
	body, err := os.ReadFile(dest)
	if err != nil {
		t.Fatalf("read dest: %v", err)
	}
	if string(body) != handWritten {
		t.Errorf("dest content = %q, want it untouched (%q)", body, handWritten)
	}
}

// TestRunInstall_OverwritesOwnPriorPlacement: a pre-existing
// destination file whose first line IS the generated-by marker (i.e.
// a previous install's output) may be overwritten.
func TestRunInstall_OverwritesOwnPriorPlacement(t *testing.T) {
	bundleDir := t.TempDir()
	writeFile(t, bundleDir, "plugin.json", tsPluginContractPluginJSON("acme-tool"), 0o644)
	writeFile(t, bundleDir, "bin/whippletree-hook", "#!/bin/sh\n", 0o755)
	writeFile(t, bundleDir, "hooks/opencode.ts", fakeCompiledOpencodeShim, 0o644)

	projectDir := t.TempDir()
	stale := generatedByMarkerLine + "\n" + `const HOOK = "/old/stale/path"` + "\n"
	writeFile(t, projectDir, filepath.Join(".opencode", "plugin", "whippletree-acme-tool.ts"), stale, 0o644)

	var stdout, stderr bytes.Buffer
	code := run([]string{
		"install", bundleDir,
		"--target", "opencode",
		"--project", projectDir,
		"--targets-dir", realTargetsDir(),
		"--assume-version", "1.20.0",
	}, &stdout, &stderr)

	if code != 0 {
		t.Fatalf("run(install) = %d, want 0 (stdout=%s stderr=%s)", code, stdout.String(), stderr.String())
	}

	dest := filepath.Join(projectDir, ".opencode", "plugin", "whippletree-acme-tool.ts")
	body, err := os.ReadFile(dest)
	if err != nil {
		t.Fatalf("read dest: %v", err)
	}
	if strings.Contains(string(body), "/old/stale/path") {
		t.Errorf("dest = %q, want the stale path overwritten", body)
	}
}

// TestRunInstall_HooksJSONTargetPrintsGuidanceAndExitsZero: install
// against a hooks-json target (claude-code) is documentation, not
// placement: it prints the harness's own plugin-marketplace commands
// and exits 0 without writing any files.
func TestRunInstall_HooksJSONTargetPrintsGuidanceAndExitsZero(t *testing.T) {
	bundleDir := t.TempDir()
	writeFile(t, bundleDir, "plugin.json", tsPluginContractPluginJSON("acme-tool"), 0o644)

	projectDir := t.TempDir()

	var stdout, stderr bytes.Buffer
	code := run([]string{
		"install", bundleDir,
		"--target", "claude-code",
		"--project", projectDir,
		"--targets-dir", realTargetsDir(),
		"--assume-version", "2.5.0",
	}, &stdout, &stderr)

	if code != 0 {
		t.Fatalf("run(install) = %d, want 0 (stdout=%s stderr=%s)", code, stdout.String(), stderr.String())
	}
	if !strings.Contains(stdout.String(), "install for claude-code is the harness's own plugin mechanism") {
		t.Errorf("stdout = %q, want the hooks-json guidance banner", stdout.String())
	}
	if !strings.Contains(stdout.String(), "claude plugin marketplace add") || !strings.Contains(stdout.String(), "claude plugin install") {
		t.Errorf("stdout = %q, want both the marketplace-add and install commands", stdout.String())
	}
	if _, err := os.Stat(filepath.Join(projectDir, ".opencode")); !os.IsNotExist(err) {
		t.Errorf("expected no .opencode dir for a hooks-json install, stat err = %v", err)
	}
	if _, err := os.Stat(filepath.Join(bundleDir, ".whippletree", "install-state.json")); err != nil {
		t.Errorf("expected install-state.json to still be written: %v", err)
	}
}

// TestRunInstall_CodexGuidanceUsesPluginAdd confirms codex's guidance
// uses "plugin add" (its own CLI verb) rather than claude-code's
// "plugin install", matching README's documented commands.
func TestRunInstall_CodexGuidanceUsesPluginAdd(t *testing.T) {
	bundleDir := t.TempDir()
	writeFile(t, bundleDir, "plugin.json", tsPluginContractPluginJSON("acme-tool"), 0o644)

	projectDir := t.TempDir()

	var stdout, stderr bytes.Buffer
	code := run([]string{
		"install", bundleDir,
		"--target", "codex",
		"--project", projectDir,
		"--targets-dir", realTargetsDir(),
		"--assume-version", "0.150.0",
	}, &stdout, &stderr)

	if code != 0 {
		t.Fatalf("run(install) = %d, want 0 (stdout=%s stderr=%s)", code, stdout.String(), stderr.String())
	}
	if !strings.Contains(stdout.String(), "codex plugin add") {
		t.Errorf("stdout = %q, want the codex-specific \"plugin add\" command", stdout.String())
	}
}

// TestRunInstall_ProjectDefaultsToCWD: omitting --project places the
// plugin shim relative to the process's current working directory.
func TestRunInstall_ProjectDefaultsToCWD(t *testing.T) {
	bundleDir := t.TempDir()
	writeFile(t, bundleDir, "plugin.json", tsPluginContractPluginJSON("acme-tool"), 0o644)
	writeFile(t, bundleDir, "bin/whippletree-hook", "#!/bin/sh\n", 0o755)
	writeFile(t, bundleDir, "hooks/opencode.ts", fakeCompiledOpencodeShim, 0o644)

	// realTargetsDir() is relative to this package's directory, so it
	// must be resolved to an absolute path before the chdir below.
	absTargetsDir, err := filepath.Abs(realTargetsDir())
	if err != nil {
		t.Fatal(err)
	}

	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	projectDir := t.TempDir()
	if err := os.Chdir(projectDir); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := os.Chdir(cwd); err != nil {
			t.Fatal(err)
		}
	}()

	var stdout, stderr bytes.Buffer
	code := run([]string{
		"install", bundleDir,
		"--target", "opencode",
		"--targets-dir", absTargetsDir,
		"--assume-version", "1.20.0",
	}, &stdout, &stderr)

	if code != 0 {
		t.Fatalf("run(install) = %d, want 0 (stdout=%s stderr=%s)", code, stdout.String(), stderr.String())
	}
	if _, err := os.Stat(filepath.Join(projectDir, ".opencode", "plugin", "whippletree-acme-tool.ts")); err != nil {
		t.Errorf("expected shim placed under cwd's .opencode/plugin, stat err = %v", err)
	}
}

// TestStdinIsTTY_FalseForRegularFile confirms stdinIsTTY reports false
// for a non-terminal stdin.
func TestStdinIsTTY_FalseForRegularFile(t *testing.T) {
	f, err := os.CreateTemp(t.TempDir(), "not-a-tty")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	orig := os.Stdin
	os.Stdin = f
	defer func() { os.Stdin = orig }()

	if stdinIsTTY() {
		t.Error("stdinIsTTY() = true for a regular file, want false")
	}
}

// TestRunWith_PlumbsWithoutBehaviorChange verifies that runWith accepts
// injected stdin and TTY-ness without changing behavior: calling runWith
// with strings.NewReader("") and func() bool { return false } on an
// existing arg-error path produces identical exit code and stderr output
// to the same call through run.
func TestRunWith_PlumbsWithoutBehaviorChange(t *testing.T) {
	// Use preflight without the required --target flag as an arg-error case.
	args := []string{"preflight", "bundledir"}

	// Call through run to get the baseline.
	var runStdout, runStderr bytes.Buffer
	runCode := run(args, &runStdout, &runStderr)

	// Call through runWith with injected stdin and TTY function.
	var runWithStdout, runWithStderr bytes.Buffer
	runWithCode := runWith(args, strings.NewReader(""), func() bool { return false }, &runWithStdout, &runWithStderr)

	if runWithCode != runCode {
		t.Errorf("runWith exit code = %d, want %d (same as run)", runWithCode, runCode)
	}
	if runWithStderr.String() != runStderr.String() {
		t.Errorf("runWith stderr = %q, want %q (same as run)", runWithStderr.String(), runStderr.String())
	}
}
