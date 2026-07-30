package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeFile writes body to dir/rel, creating parent directories as
// needed.
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

// TestRunBuild_RefusesAndExitsNonZero is the regression test for
// finding 4: a build whose contract has a hard-required requirement
// the target can't satisfy (here, a loopGuardRequired blocking-gate
// against a target with no loop-guard field) must exit 1 and name the
// refusing target/requirement, not silently exit 0.
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

// TestRunBuild_TargetsDirWithZeroTargetsErrors is the regression test
// for finding 8's other half: --targets-dir pointing at a directory
// with no target.yaml files must be a build error, not a silent
// "compiled for zero targets."
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

// TestEnsureDispatcher_MissingFailsWithActionableMessage is the
// regression test for finding 2: a build whose bundle has no
// bin/whippletree-hook, and no sibling binary to copy from (the normal
// case under `go test`), must fail with the exact command to fix it,
// naming the bundle's bin path.
func TestEnsureDispatcher_MissingFailsWithActionableMessage(t *testing.T) {
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

// TestRunPreflight_ProbeFailureMessageIsDistinct is the regression
// test for finding 13: a Probe failure (the harness's version command
// itself failing) must be reported with its own "probe failed"
// message, not conflated with Check's fail-closed error.
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

// TestStdinIsTTY_FalseForRegularFile confirms stdinIsTTY reports false
// for a non-terminal stdin (the common case under `go test`, and
// exactly what makes preflight's old hardcoded-false behavior correct
// for every test and CI invocation while still letting a real
// terminal opt into interactive mode).
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
