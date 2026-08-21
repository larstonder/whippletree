package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/larstonder/whippletree/internal/contract"

	"github.com/larstonder/whippletree/internal/skillfile"
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

	var runStdout, runStderr bytes.Buffer
	runCode := run(args, &runStdout, &runStderr)

	var runWithStdout, runWithStderr bytes.Buffer
	runWithCode := runWith(args, strings.NewReader(""), func() bool { return false }, &runWithStdout, &runWithStderr)

	if runWithCode != runCode {
		t.Errorf("runWith exit code = %d, want %d (same as run)", runWithCode, runCode)
	}
	if runWithStderr.String() != runStderr.String() {
		t.Errorf("runWith stderr = %q, want %q (same as run)", runWithStderr.String(), runStderr.String())
	}
}

// escapeYAMLDoubleQuoted makes a path safe inside a double-quoted YAML
// scalar. Windows temp paths are full of backslashes, and "\U" there is
// a YAML escape rather than a literal.
func escapeYAMLDoubleQuoted(s string) string {
	return strings.ReplaceAll(s, `\`, `\\`)
}

// writeSkillTestTarget writes a minimal copy-dir target def whose dest
// is destValue, returning the targets dir for --targets-dir.
func writeSkillTestTarget(t *testing.T, destValue string) string {
	t.Helper()
	dir := t.TempDir()
	td := filepath.Join(dir, "skilltest")
	if err := os.MkdirAll(td, 0o755); err != nil {
		t.Fatal(err)
	}
	yaml := `apiVersion: whippletree.dev/v1
kind: TargetDefinition
metadata:
  name: skilltest
  class: 3
  schemaVersion: "1.0.0"
spec:
  backend: ts-plugin
  probe:
    command: ["true"]
    versionPattern: '(\d+)'
  events:
    tool-pre: { native: "tool.execute.before", blocking: true }
  capabilities:
    installerPath: true
  skillChannel:
    kind: copy-dir
    dest: "` + escapeYAMLDoubleQuoted(destValue) + `"
`
	if err := os.WriteFile(filepath.Join(td, "target.yaml"), []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

// scaffoldSkillBundle writes a bundle with one skill and one hard T3
// turn-end gate falling back to it, plus its handler and a dispatcher
// stub, and runs build against targetsDir.
func scaffoldSkillBundle(t *testing.T, targetsDir string) string {
	t.Helper()
	dir := t.TempDir()
	write := func(rel, body string, perm os.FileMode) {
		full := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(body), perm); err != nil {
			t.Fatal(err)
		}
	}
	write("plugin.json", `{
  "name": "sk", "version": "0.1.0", "description": "d",
  "extensions": {"dev.whippletree.v1": {"contractVersion": "1.0.0", "requires": [
    {"id":"cap","kind":"skill","path":"./skills/cap","minTier":"T1","hardRequired":false},
    {"id":"gate","kind":"blocking-gate","event":"turn-end","minTier":"T3",
     "hardRequired":true,"loopGuardRequired":true,"handler":"./handlers/g.sh",
     "fallbackSkill":"cap"}
  ]}}}`, 0o644)
	write("handlers/g.sh", "#!/bin/sh\nexit 0\n", 0o755)
	write("skills/cap/SKILL.md", "---\nname: cap\ndescription: d.\n---\nb\n", 0o644)
	write("bin/whippletree-hook", "#!/bin/sh\nexit 0\n", 0o755)

	var out, errb bytes.Buffer
	if code := run([]string{"build", dir, "--targets-dir", targetsDir}, &out, &errb); code != 0 {
		t.Fatalf("build failed: %s", errb.String())
	}
	return dir
}

func TestInstallPlacesSkillCopyDir(t *testing.T) {
	destRoot := t.TempDir()
	targetsDir := writeSkillTestTarget(t, filepath.Join(destRoot, "skills"))
	bundle := scaffoldSkillBundle(t, targetsDir)
	projectDir := t.TempDir()

	var out, errb bytes.Buffer
	code := run([]string{"install", bundle, "--target", "skilltest",
		"--project", projectDir,
		"--assume-version", "1", "--targets-dir", targetsDir}, &out, &errb)
	if code != 0 {
		t.Fatalf("install failed: %s", errb.String())
	}

	placed := filepath.Join(destRoot, "skills", "sk-cap", "SKILL.md")
	raw, err := os.ReadFile(placed)
	if err != nil {
		t.Fatalf("skill not placed: %v", err)
	}
	if strings.Contains(string(raw), skillfile.Placeholder) {
		t.Fatal("placeholder not baked")
	}
	absBundle, _ := filepath.Abs(bundle)
	if !strings.Contains(string(raw), absBundle+"/handlers/g.sh") {
		t.Fatalf("baked handler path missing:\n%s", raw)
	}

	// Idempotent re-install: our own marker allows replacement.
	if code := run([]string{"install", bundle, "--target", "skilltest",
		"--project", projectDir,
		"--assume-version", "1", "--targets-dir", targetsDir}, &out, &errb); code != 0 {
		t.Fatalf("re-install refused its own artifact: %s", errb.String())
	}
}

func TestInstallRefusesUserOwnedSkillDir(t *testing.T) {
	destRoot := t.TempDir()
	targetsDir := writeSkillTestTarget(t, filepath.Join(destRoot, "skills"))
	bundle := scaffoldSkillBundle(t, targetsDir)
	projectDir := t.TempDir()

	userDir := filepath.Join(destRoot, "skills", "sk-cap")
	if err := os.MkdirAll(userDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(userDir, "SKILL.md"),
		[]byte("---\nname: sk-cap\ndescription: mine.\n---\nhand-authored\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	var out, errb bytes.Buffer
	code := run([]string{"install", bundle, "--target", "skilltest",
		"--project", projectDir,
		"--assume-version", "1", "--targets-dir", targetsDir}, &out, &errb)
	if code == 0 {
		t.Fatal("install must refuse a marker-less destination")
	}
	if !strings.Contains(errb.String(), "not placed by whippletree") {
		t.Fatalf("refusal must say why: %s", errb.String())
	}
	raw, _ := os.ReadFile(filepath.Join(userDir, "SKILL.md"))
	if !strings.Contains(string(raw), "hand-authored") {
		t.Fatal("user-owned file was touched")
	}
}

// TestInstallRefusesSkillDirMissingSkillMD covers the half-authored/
// name-collision case: a destination directory exists but has no
// SKILL.md at all (not merely one missing the marker). This must be
// refused exactly like a marker-less SKILL.md would, and must not be
// cleared: a naive IsNotExist-on-SKILL.md check would misread this as
// "destination absent" and let placeSkills RemoveAll the directory.
func TestInstallRefusesSkillDirMissingSkillMD(t *testing.T) {
	destRoot := t.TempDir()
	targetsDir := writeSkillTestTarget(t, filepath.Join(destRoot, "skills"))
	bundle := scaffoldSkillBundle(t, targetsDir)
	projectDir := t.TempDir()

	userDir := filepath.Join(destRoot, "skills", "sk-cap")
	if err := os.MkdirAll(userDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(userDir, "notes.txt"), []byte("wip\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	var out, errb bytes.Buffer
	code := run([]string{"install", bundle, "--target", "skilltest",
		"--project", projectDir,
		"--assume-version", "1", "--targets-dir", targetsDir}, &out, &errb)
	if code == 0 {
		t.Fatal("install must refuse a destination dir that exists but has no SKILL.md")
	}
	if !strings.Contains(errb.String(), "not placed by whippletree") {
		t.Fatalf("refusal must say why: %s", errb.String())
	}
	if _, err := os.Stat(filepath.Join(userDir, "notes.txt")); err != nil {
		t.Fatalf("user-owned directory contents were touched: %v", err)
	}
	if _, err := os.Stat(filepath.Join(userDir, "SKILL.md")); !os.IsNotExist(err) {
		t.Fatalf("SKILL.md must not have been created, stat err = %v", err)
	}
}

func TestInstallExpandsTildeAgainstHOME(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home) // os.UserHomeDir reads this on Windows
	targetsDir := writeSkillTestTarget(t, "~/.agents/skills")
	bundle := scaffoldSkillBundle(t, targetsDir)
	projectDir := t.TempDir()

	var out, errb bytes.Buffer
	if code := run([]string{"install", bundle, "--target", "skilltest",
		"--project", projectDir,
		"--assume-version", "1", "--targets-dir", targetsDir}, &out, &errb); code != 0 {
		t.Fatalf("install failed: %s", errb.String())
	}
	if _, err := os.Stat(filepath.Join(home, ".agents", "skills", "sk-cap", "SKILL.md")); err != nil {
		t.Fatalf("tilde dest did not land under overridden HOME: %v", err)
	}
}

// TestBuildSessionStartSkillExpansionEndToEnd drives the session-start
// instruction-fallback path end to end at the build level: a
// lifecycle-signal requirement on session-start, against the
// writeSkillTestTarget synthetic target (which maps only tool-pre, no
// session-start), falls back to its paired skill, and the built
// variant must carry the session-start manual-step section with
// ADAPTER_STOP_ACTIVE left empty (only the turn-end manual step toggles
// it).
func TestBuildSessionStartSkillExpansionEndToEnd(t *testing.T) {
	targetsDir := writeSkillTestTarget(t, t.TempDir())

	dir := t.TempDir()
	writeFile(t, dir, "plugin.json", `{
  "name": "sk2", "version": "0.1.0", "description": "d",
  "extensions": {"dev.whippletree.v1": {"contractVersion": "1.0.0", "requires": [
    {"id":"cap","kind":"skill","path":"./skills/cap","minTier":"T1","hardRequired":false},
    {"id":"pull","kind":"lifecycle-signal","event":"session-start","minTier":"T3",
     "hardRequired":false,"handler":"./handlers/pull.sh","fallbackSkill":"cap"}
  ]}}}`, 0o644)
	writeFile(t, dir, "handlers/pull.sh", "#!/bin/sh\nexit 0\n", 0o755)
	writeFile(t, dir, "skills/cap/SKILL.md", "---\nname: cap\ndescription: d.\n---\nb\n", 0o644)
	writeFile(t, dir, "bin/whippletree-hook", "#!/bin/sh\nexit 0\n", 0o755)

	var out, errb bytes.Buffer
	if code := run([]string{"build", dir, "--targets-dir", targetsDir}, &out, &errb); code != 0 {
		t.Fatalf("build failed: %s", errb.String())
	}

	variant := filepath.Join(dir, ".whippletree", "skills", "skilltest", "cap", "SKILL.md")
	raw, err := os.ReadFile(variant)
	if err != nil {
		t.Fatalf("built skill variant missing: %v", err)
	}
	got := string(raw)
	if !strings.Contains(got, "Manual step on this harness (session-start)") {
		t.Fatalf("variant missing session-start manual step:\n%s", got)
	}
	if strings.Contains(got, "ADAPTER_STOP_ACTIVE=false") || strings.Contains(got, "ADAPTER_STOP_ACTIVE=true") {
		t.Fatalf("session-start command must leave ADAPTER_STOP_ACTIVE empty:\n%s", got)
	}
}

func TestPreflightRejectsBrokenSkillFile(t *testing.T) {
	targetsDir := writeSkillTestTarget(t, t.TempDir())
	bundle := scaffoldSkillBundle(t, targetsDir)

	broken := filepath.Join(bundle, "skills", "cap", "SKILL.md")
	if err := os.WriteFile(broken, []byte("no frontmatter here\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	var out, errb bytes.Buffer
	code := run([]string{"preflight", bundle, "--target", "skilltest",
		"--assume-version", "1", "--targets-dir", targetsDir}, &out, &errb)
	if code == 0 {
		t.Fatal("preflight must reject a SKILL.md without frontmatter")
	}
	if !strings.Contains(errb.String(), "frontmatter") {
		t.Fatalf("error must name the frontmatter problem: %s", errb.String())
	}
}

// TestParsersRejectUnknownFlags: an unrecognized token is an error in
// all four parsers, never folded into the positionals.
func TestParsersRejectUnknownFlags(t *testing.T) {
	cases := []struct {
		name string
		run  func([]string) error
	}{
		{"build", func(a []string) error { _, err := parseBuildArgs(a); return err }},
		{"preflight", func(a []string) error { _, _, _, _, err := parsePreflightArgs(a); return err }},
		{"install", func(a []string) error { _, err := parseInstallArgs(a); return err }},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.run([]string{"./bundle", "--not-a-real-flag"})
			if err == nil {
				t.Fatal("parser accepted an unknown flag, want an error")
			}
			if !strings.Contains(err.Error(), "unknown flag") {
				t.Errorf("error = %q, want it to mention the unknown flag", err)
			}
		})
	}
}

// TestBuildArgsDoesNotSwallowFlagAsBundleDir is the concrete regression:
// a lone flag must not become the bundle directory.
func TestBuildArgsDoesNotSwallowFlagAsBundleDir(t *testing.T) {
	parsed, err := parseBuildArgs([]string{"--allow-refuse"})
	if err == nil {
		t.Fatalf("parseBuildArgs = %+v, want an error rather than bundleDir=%q", parsed, parsed.bundleDir)
	}
}

func TestResolveSkillDest(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skipf("no home directory: %v", err)
	}
	project := t.TempDir()

	cases := []struct {
		name, dest, want, wantErr string
	}{
		{name: "bare tilde is home, not a directory named ~", dest: "~", want: home},
		{name: "tilde path", dest: "~/.agents/skills", want: filepath.Join(home, ".agents", "skills")},
		{name: "relative resolves against the project", dest: ".opencode/skills", want: filepath.Join(project, ".opencode", "skills")},
		{name: "absolute is itself", dest: filepath.Join(project, "abs"), want: filepath.Join(project, "abs")},
		{name: "tilde-user is refused, not guessed", dest: "~someone/skills", wantErr: "not supported"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := resolveSkillDest(tc.dest, project)
			if tc.wantErr != "" {
				if err == nil {
					t.Fatalf("resolveSkillDest(%q) = %q, want an error", tc.dest, got)
				}
				if !strings.Contains(err.Error(), tc.wantErr) {
					t.Errorf("error = %q, want it to mention %q", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("resolveSkillDest(%q) = %v", tc.dest, err)
			}
			if got != tc.want {
				t.Errorf("resolveSkillDest(%q) = %q, want %q", tc.dest, got, tc.want)
			}
		})
	}
}

// TestVersionReportsTargetCorpus: `whippletree version` must name every
// compiled-in target with its schema version and the harness range it
// was actually probed against. That corpus is the substantive half of
// the output; the build stamp alone would not be worth a subcommand.
func TestVersionReportsTargetCorpus(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := run([]string{"version"}, &stdout, &stderr); code != 0 {
		t.Fatalf("version = %d, want 0 (stderr: %s)", code, stderr.String())
	}

	out := stdout.String()
	for _, want := range []string{
		"whippletree ",
		"contract: " + contract.SupportedContractVersion,
		"targets (3):",
		"claude-code", "codex", "opencode",
		">=2.1.0", ">=0.144.0", ">=1.18.10",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("version output missing %q\n---\n%s", want, out)
		}
	}
}

func TestVersionRejectsUnknownArgs(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := run([]string{"version", "--nope"}, &stdout, &stderr); code != 1 {
		t.Fatalf("version --nope = %d, want 1", code)
	}
}

func TestHelpAndUnknownSubcommand(t *testing.T) {
	for _, arg := range []string{"help", "--help", "-h"} {
		var stdout, stderr bytes.Buffer
		if code := run([]string{arg}, &stdout, &stderr); code != 0 {
			t.Errorf("%s = %d, want 0", arg, code)
		}
		if !strings.Contains(stdout.String(), "usage: whippletree") {
			t.Errorf("%s did not print usage on stdout", arg)
		}
	}

	var stdout, stderr bytes.Buffer
	if code := run([]string{"definitely-not-a-verb"}, &stdout, &stderr); code != 1 {
		t.Errorf("unknown subcommand = %d, want 1", code)
	}
	if !strings.Contains(stderr.String(), "usage: whippletree") {
		t.Error("an unknown subcommand should print usage, not just an error")
	}
}
