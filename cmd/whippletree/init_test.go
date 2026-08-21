// SPDX-FileCopyrightText: 2026 Lars Tønder
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"whippletree.dev/internal/contract"
)

// readTree walks dir and returns every regular file's contents, keyed
// by its path relative to dir, so two scaffolded trees can be compared
// file-by-file.
func readTree(t *testing.T, dir string) map[string]string {
	t.Helper()
	out := make(map[string]string)
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel, relErr := filepath.Rel(dir, path)
		if relErr != nil {
			return relErr
		}
		body, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		out[rel] = string(body)
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", dir, err)
	}
	return out
}

// TestRunInit_YesDefaultsToLifecycleOnly confirms --yes in a temp dir
// creates exactly the lifecycle-only file set, and the scaffolded
// plugin.json round-trips through contract.Parse+Validate.
func TestRunInit_YesDefaultsToLifecycleOnly(t *testing.T) {
	dir := t.TempDir()

	var stdout, stderr bytes.Buffer
	code := run([]string{"init", dir, "--name", "acme-tool", "--yes"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("run(init) = %d, want 0 (stdout=%s stderr=%s)", code, stdout.String(), stderr.String())
	}

	wantFiles := []string{
		"plugin.json",
		filepath.Join(".claude-plugin", "marketplace.json"),
		filepath.Join("handlers", "lifecycle-signal.sh"),
		".gitignore",
		"README.md",
	}
	for _, rel := range wantFiles {
		if _, err := os.Stat(filepath.Join(dir, rel)); err != nil {
			t.Errorf("expected %s to exist: %v", rel, err)
		}
	}

	unwantedFiles := []string{
		filepath.Join("handlers", "observation-signal.sh"),
		filepath.Join("handlers", "blocking-gate.sh"),
		filepath.Join("bin", "acme-tool"),
	}
	for _, rel := range unwantedFiles {
		if _, err := os.Stat(filepath.Join(dir, rel)); !os.IsNotExist(err) {
			t.Errorf("expected %s to NOT exist, stat err = %v", rel, err)
		}
	}

	raw, err := os.ReadFile(filepath.Join(dir, "plugin.json"))
	if err != nil {
		t.Fatalf("read scaffolded plugin.json: %v", err)
	}
	c, err := contract.Parse(raw)
	if err != nil {
		t.Fatalf("contract.Parse(scaffolded plugin.json) = %v", err)
	}
	if err := contract.Validate(c); err != nil {
		t.Fatalf("contract.Validate(scaffolded plugin.json) = %v", err)
	}
	if len(c.Requires) != 1 || c.Requires[0].Kind != "lifecycle-signal" {
		t.Errorf("Requires = %+v, want exactly one lifecycle-signal requirement", c.Requires)
	}
	if c.Requires[0].HardRequired == nil || *c.Requires[0].HardRequired {
		t.Errorf("Requires[0].HardRequired = %v, want false (soft)", c.Requires[0].HardRequired)
	}
}

// TestRunInit_KindsVariants confirms each single-kind --kinds variant,
// and the all-four variant, produce a plugin.json that round-trips
// through contract.Parse+Validate.
func TestRunInit_KindsVariants(t *testing.T) {
	cases := []struct {
		name  string
		kinds string
		want  []string
	}{
		{"lifecycle-only", "lifecycle-signal", []string{"lifecycle-signal"}},
		{"observation-only", "observation-signal", []string{"observation-signal"}},
		{"blocking-only", "blocking-gate", []string{"blocking-gate"}},
		{"executable-only", "executable-path", []string{"executable-path"}},
		{"all-four", "blocking-gate,lifecycle-signal,observation-signal,executable-path",
			[]string{"lifecycle-signal", "observation-signal", "blocking-gate", "executable-path"}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			bundleDir := filepath.Join(dir, "bundle")

			var stdout, stderr bytes.Buffer
			code := run([]string{"init", bundleDir, "--name", "acme-tool", "--kinds", tc.kinds}, &stdout, &stderr)
			if code != 0 {
				t.Fatalf("run(init --kinds %s) = %d, want 0 (stdout=%s stderr=%s)", tc.kinds, code, stdout.String(), stderr.String())
			}

			raw, err := os.ReadFile(filepath.Join(bundleDir, "plugin.json"))
			if err != nil {
				t.Fatalf("read scaffolded plugin.json: %v", err)
			}
			c, err := contract.Parse(raw)
			if err != nil {
				t.Fatalf("contract.Parse = %v", err)
			}
			if err := contract.Validate(c); err != nil {
				t.Fatalf("contract.Validate = %v", err)
			}

			if len(c.Requires) != len(tc.want) {
				t.Fatalf("Requires = %+v, want %d requirements matching %v", c.Requires, len(tc.want), tc.want)
			}
			for i, k := range tc.want {
				if c.Requires[i].Kind != k {
					t.Errorf("Requires[%d].Kind = %q, want %q", i, c.Requires[i].Kind, k)
				}
			}
		})
	}
}

// TestGuarantee_InitBuildPreflightAllFourKindsDefaultsSoft confirms the
// all-four scaffold, with real sibling-dispatcher binaries built, must
// build with exit 0 and preflight against claude-code with no REFUSE
// (defaults are all soft, so a tier shortfall against a class-1 target
// would DEGRADE at worst, never REFUSE).
//
// This exercises the real CLI as built binaries (rather than in-process
// run() calls) because ensureDispatcher's sibling-copy path keys off
// os.Executable(), which under `go test` resolves to the test binary,
// not a "whippletree" binary with a "whippletree-hook" sibling.
func TestGuarantee_InitBuildPreflightAllFourKindsDefaultsSoft(t *testing.T) {
	repoRoot, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}

	binDir := t.TempDir()
	exe := ""
	if runtime.GOOS == "windows" {
		exe = ".exe"
	}
	whippletreeBin := filepath.Join(binDir, "whippletree"+exe)
	hookBin := filepath.Join(binDir, "whippletree-hook"+exe)

	buildBinary := func(out, pkg string) {
		t.Helper()
		cmd := exec.Command("go", "build", "-o", out, pkg)
		cmd.Dir = repoRoot
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("go build %s: %v\n%s", pkg, err, out)
		}
	}
	buildBinary(whippletreeBin, "./cmd/whippletree")
	buildBinary(hookBin, "./cmd/whippletree-hook")

	// A working directory outside the repo entirely: proves the built
	// whippletree binary falls back to its embedded targets rather than
	// relying on a cwd-relative "targets" directory.
	workDir := t.TempDir()
	bundleDir := filepath.Join(workDir, "acme-tool")

	runCLI := func(args ...string) (stdout, stderr string, code int) {
		t.Helper()
		cmd := exec.Command(whippletreeBin, args...)
		cmd.Dir = workDir
		var outBuf, errBuf bytes.Buffer
		cmd.Stdout = &outBuf
		cmd.Stderr = &errBuf
		runErr := cmd.Run()
		if runErr == nil {
			return outBuf.String(), errBuf.String(), 0
		}
		var exitErr *exec.ExitError
		if errors.As(runErr, &exitErr) {
			return outBuf.String(), errBuf.String(), exitErr.ExitCode()
		}
		t.Fatalf("run %v: %v", args, runErr)
		return "", "", -1
	}

	_, initStderr, code := runCLI("init", bundleDir,
		"--kinds", "blocking-gate,lifecycle-signal,observation-signal,executable-path",
		"--yes")
	if code != 0 {
		t.Fatalf("init exit = %d, want 0 (stderr=%s)", code, initStderr)
	}

	buildStdout, buildStderr, code := runCLI("build", bundleDir)
	if code != 0 {
		t.Fatalf("build exit = %d, want 0 (stdout=%s stderr=%s)", code, buildStdout, buildStderr)
	}

	if _, err := os.Stat(filepath.Join(bundleDir, "bin", dispatcherName())); err != nil {
		t.Errorf("expected build to have copied the dispatcher from the sibling binary: %v", err)
	}

	preStdout, preStderr, code := runCLI("preflight", bundleDir, "--target", "claude-code", "--assume-version", "2.1.220")
	if code != 0 {
		t.Fatalf("preflight exit = %d, want 0 (stdout=%s stderr=%s)", code, preStdout, preStderr)
	}
	if strings.Contains(preStdout, "REFUSE") {
		t.Errorf("preflight stdout = %q, want no REFUSE verdicts with all-soft defaults", preStdout)
	}
}

// TestRunInit_RefusesToOverwriteExistingFile confirms overwrite refusal
// on a pre-existing README.md must error before writing anything else
// (e.g. plugin.json, written earlier in the generation order, must
// also be absent).
func TestRunInit_RefusesToOverwriteExistingFile(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "README.md", "hand-written, do not touch\n", 0o644)

	var stdout, stderr bytes.Buffer
	code := run([]string{"init", dir, "--name", "acme-tool", "--yes"}, &stdout, &stderr)
	if code != 1 {
		t.Fatalf("run(init) = %d, want 1 (stdout=%s stderr=%s)", code, stdout.String(), stderr.String())
	}
	if !strings.Contains(stderr.String(), "README.md") {
		t.Errorf("stderr = %q, want it to name README.md", stderr.String())
	}

	body, err := os.ReadFile(filepath.Join(dir, "README.md"))
	if err != nil {
		t.Fatalf("read README.md: %v", err)
	}
	if string(body) != "hand-written, do not touch\n" {
		t.Errorf("README.md = %q, want it untouched", body)
	}

	if _, err := os.Stat(filepath.Join(dir, "plugin.json")); !os.IsNotExist(err) {
		t.Errorf("expected no plugin.json to have been written, stat err = %v", err)
	}
}

// TestRunInit_UnknownFlagErrorsAndCreatesNothing confirms an unknown
// flag is a strict parse error, and nothing is created.
func TestRunInit_UnknownFlagErrorsAndCreatesNothing(t *testing.T) {
	dir := t.TempDir()

	var stdout, stderr bytes.Buffer
	code := run([]string{"init", dir, "--kins", "x"}, &stdout, &stderr)
	if code != 1 {
		t.Fatalf("run(init --kins x) = %d, want 1 (stdout=%s stderr=%s)", code, stdout.String(), stderr.String())
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read dir: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("dir entries = %v, want none created", entries)
	}
}

// TestRunInit_BadNameErrors confirms a name that doesn't match
// ^[a-z0-9-]+$ is a clear error.
func TestRunInit_BadNameErrors(t *testing.T) {
	dir := t.TempDir()

	var stdout, stderr bytes.Buffer
	code := run([]string{"init", dir, "--name", "My Tool", "--yes"}, &stdout, &stderr)
	if code != 1 {
		t.Fatalf("run(init --name \"My Tool\") = %d, want 1 (stdout=%s stderr=%s)", code, stdout.String(), stderr.String())
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read dir: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("dir entries = %v, want none created", entries)
	}
}

// TestRunInitWizard_AllDefaultsMatchesYes confirms answering every
// wizard question with a bare newline (taking every default) produces
// exactly the same files --yes would, byte for byte.
func TestRunInitWizard_AllDefaultsMatchesYes(t *testing.T) {
	wizardDir := filepath.Join(t.TempDir(), "acme-tool")
	yesDir := filepath.Join(t.TempDir(), "acme-tool")

	var wizardStdout, wizardStderr bytes.Buffer
	code := runWith([]string{"init", wizardDir}, strings.NewReader("\n\n\n"), func() bool { return true }, &wizardStdout, &wizardStderr)
	if code != 0 {
		t.Fatalf("runWith(init wizard) = %d, want 0 (stdout=%s stderr=%s)", code, wizardStdout.String(), wizardStderr.String())
	}

	var yesStdout, yesStderr bytes.Buffer
	yesCode := runWith([]string{"init", yesDir, "--yes"}, strings.NewReader(""), func() bool { return false }, &yesStdout, &yesStderr)
	if yesCode != 0 {
		t.Fatalf("runWith(init --yes) = %d, want 0 (stdout=%s stderr=%s)", yesCode, yesStdout.String(), yesStderr.String())
	}

	wizardFiles := readTree(t, wizardDir)
	yesFiles := readTree(t, yesDir)
	if len(wizardFiles) != len(yesFiles) {
		t.Fatalf("wizard produced %d files, --yes produced %d: wizard=%v yes=%v", len(wizardFiles), len(yesFiles), wizardFiles, yesFiles)
	}
	for rel, body := range yesFiles {
		wb, ok := wizardFiles[rel]
		if !ok {
			t.Errorf("wizard is missing %s", rel)
			continue
		}
		if wb != body {
			t.Errorf("%s differs between wizard and --yes:\nwizard=%q\nyes=%q", rel, wb, body)
		}
	}

	if !strings.Contains(wizardStdout.String(), "Bundle name [acme-tool]: ") {
		t.Errorf("stdout = %q, want it to contain the bundle name prompt", wizardStdout.String())
	}
	if !strings.Contains(wizardStdout.String(), "Kinds to include (comma-separated numbers) [2]: ") {
		t.Errorf("stdout = %q, want it to contain the kinds prompt", wizardStdout.String())
	}
}

// TestRunInitWizard_AllFourKindsWithHardGate confirms choosing all four
// kinds and answering yes to blocking-gate's hard-required question
// (while leaving executable-path's blank) produces a plugin.json
// matching those choices, and the hard-required prompts appear on
// stdout with the expected wording.
func TestRunInitWizard_AllFourKindsWithHardGate(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "acme-tool")

	answers := "\n1,2,3,4\ny\n\n"
	var stdout, stderr bytes.Buffer
	code := runWith([]string{"init", dir}, strings.NewReader(answers), func() bool { return true }, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("runWith(init wizard) = %d, want 0 (stdout=%s stderr=%s)", code, stdout.String(), stderr.String())
	}

	wantPrompts := []string{
		"Bundle name [acme-tool]: ",
		"Kinds to include (comma-separated numbers) [2]: ",
		"Make blocking-gate hard-required? A hard requirement refuses install when the harness cannot meet it. [y/N]: ",
		"Make executable-path hard-required? A hard requirement refuses install when the harness cannot meet it. [y/N]: ",
	}
	for _, p := range wantPrompts {
		if !strings.Contains(stdout.String(), p) {
			t.Errorf("stdout = %q, want it to contain %q", stdout.String(), p)
		}
	}

	raw, err := os.ReadFile(filepath.Join(dir, "plugin.json"))
	if err != nil {
		t.Fatalf("read plugin.json: %v", err)
	}
	c, err := contract.Parse(raw)
	if err != nil {
		t.Fatalf("contract.Parse = %v", err)
	}
	if err := contract.Validate(c); err != nil {
		t.Fatalf("contract.Validate = %v", err)
	}
	if len(c.Requires) != 4 {
		t.Fatalf("Requires = %+v, want all four kinds", c.Requires)
	}

	byKind := make(map[string]contract.Requirement, len(c.Requires))
	for _, r := range c.Requires {
		byKind[r.Kind] = r
	}
	if hr := byKind["blocking-gate"].HardRequired; hr == nil || !*hr {
		t.Errorf("blocking-gate hardRequired = %v, want true", hr)
	}
	if hr := byKind["executable-path"].HardRequired; hr == nil || *hr {
		t.Errorf("executable-path hardRequired = %v, want false (blank answer keeps default)", hr)
	}
	if hr := byKind["lifecycle-signal"].HardRequired; hr == nil || *hr {
		t.Errorf("lifecycle-signal hardRequired = %v, want false (always soft)", hr)
	}
	if hr := byKind["observation-signal"].HardRequired; hr == nil || *hr {
		t.Errorf("observation-signal hardRequired = %v, want false (always soft)", hr)
	}
}

// TestRunInitWizard_InvalidKindThenValidSucceeds confirms an
// out-of-range kind number re-prompts once with the valid range, and a
// valid answer on that reprompt succeeds.
func TestRunInitWizard_InvalidKindThenValidSucceeds(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "acme-tool")

	answers := "\n9\n2\n"
	var stdout, stderr bytes.Buffer
	code := runWith([]string{"init", dir}, strings.NewReader(answers), func() bool { return true }, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("runWith(init wizard) = %d, want 0 (stdout=%s stderr=%s)", code, stdout.String(), stderr.String())
	}
	if !strings.Contains(stdout.String(), "1 and 5") {
		t.Errorf("stdout = %q, want it to reprompt naming the valid range", stdout.String())
	}

	raw, err := os.ReadFile(filepath.Join(dir, "plugin.json"))
	if err != nil {
		t.Fatalf("read plugin.json: %v", err)
	}
	c, err := contract.Parse(raw)
	if err != nil {
		t.Fatalf("contract.Parse = %v", err)
	}
	if len(c.Requires) != 1 || c.Requires[0].Kind != "lifecycle-signal" {
		t.Errorf("Requires = %+v, want exactly lifecycle-signal (recovered choice 2)", c.Requires)
	}
}

// TestRunInitWizard_InvalidKindTwiceErrors confirms an out-of-range
// kind number that is still out of range on the reprompt is a clear
// error, and nothing is created.
func TestRunInitWizard_InvalidKindTwiceErrors(t *testing.T) {
	dir := t.TempDir()

	answers := "\n9\n9\n"
	var stdout, stderr bytes.Buffer
	code := runWith([]string{"init", dir}, strings.NewReader(answers), func() bool { return true }, &stdout, &stderr)
	if code != 1 {
		t.Fatalf("runWith(init wizard) = %d, want 1 (stdout=%s stderr=%s)", code, stdout.String(), stderr.String())
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read dir: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("dir entries = %v, want none created", entries)
	}
}

// TestRunInitWizard_InvalidHardAnswerTwiceErrors mirrors the kinds
// reprompt/error test for the hard-required question: an unrecognized
// y/n answer that is still unrecognized on the reprompt is a clear
// error, and nothing is created.
func TestRunInitWizard_InvalidHardAnswerTwiceErrors(t *testing.T) {
	dir := t.TempDir()

	answers := "\n1\nmaybe\nmaybe\n"
	var stdout, stderr bytes.Buffer
	code := runWith([]string{"init", dir}, strings.NewReader(answers), func() bool { return true }, &stdout, &stderr)
	if code != 1 {
		t.Fatalf("runWith(init wizard) = %d, want 1 (stdout=%s stderr=%s)", code, stdout.String(), stderr.String())
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read dir: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("dir entries = %v, want none created", entries)
	}
}

// TestRunInitWizard_EOFBeforeFirstQuestionErrors confirms an input
// stream that ends before the bundle name question is answered (e.g.
// Ctrl-D on a real terminal) errors cleanly instead of silently
// completing the wizard with every default, and creates nothing.
func TestRunInitWizard_EOFBeforeFirstQuestionErrors(t *testing.T) {
	dir := t.TempDir()

	var stdout, stderr bytes.Buffer
	code := runWith([]string{"init", dir}, strings.NewReader(""), func() bool { return true }, &stdout, &stderr)
	if code != 1 {
		t.Fatalf("runWith(init wizard) = %d, want 1 (stdout=%s stderr=%s)", code, stdout.String(), stderr.String())
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read dir: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("dir entries = %v, want none created", entries)
	}
}

// TestRunInitWizard_EOFMidWizardErrors confirms an input stream that
// ends after the bundle name answer but before the kinds question
// errors cleanly and creates nothing, rather than silently completing
// the wizard with defaults for every remaining question.
func TestRunInitWizard_EOFMidWizardErrors(t *testing.T) {
	dir := t.TempDir()

	var stdout, stderr bytes.Buffer
	code := runWith([]string{"init", dir}, strings.NewReader("acme-tool\n"), func() bool { return true }, &stdout, &stderr)
	if code != 1 {
		t.Fatalf("runWith(init wizard) = %d, want 1 (stdout=%s stderr=%s)", code, stdout.String(), stderr.String())
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read dir: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("dir entries = %v, want none created", entries)
	}
}

// TestRunInit_BareOnNonTTYUsesDefaults confirms bare init (no flags at
// all) on a non-TTY still scaffolds using defaults, since isTTY()==false
// alone is sufficient to select the non-interactive path.
func TestRunInit_BareOnNonTTYUsesDefaults(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "acme-tool")

	var stdout, stderr bytes.Buffer
	code := runWith([]string{"init", dir}, strings.NewReader(""), func() bool { return false }, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("runWith(init, isTTY=false) = %d, want 0 (stdout=%s stderr=%s)", code, stdout.String(), stderr.String())
	}
	if _, err := os.Stat(filepath.Join(dir, "plugin.json")); err != nil {
		t.Errorf("expected plugin.json to have been scaffolded: %v", err)
	}
}

// TestRunInit_HardOnlyAffectsGateAndPathKinds confirms --hard's scope
// matches the scaffold defaults table exactly: lifecycle-signal and
// observation-signal are always soft, regardless of --hard; only
// blocking-gate and executable-path have a hardRequired driven by
// --hard.
func TestRunInit_HardOnlyAffectsGateAndPathKinds(t *testing.T) {
	dir := t.TempDir()

	var stdout, stderr bytes.Buffer
	code := run([]string{
		"init", dir, "--name", "acme-tool",
		"--kinds", "lifecycle-signal,blocking-gate,executable-path",
		"--hard", "lifecycle-signal,blocking-gate,executable-path",
	}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("run(init) = %d, want 0 (stdout=%s stderr=%s)", code, stdout.String(), stderr.String())
	}

	raw, err := os.ReadFile(filepath.Join(dir, "plugin.json"))
	if err != nil {
		t.Fatalf("read plugin.json: %v", err)
	}
	c, err := contract.Parse(raw)
	if err != nil {
		t.Fatalf("contract.Parse = %v", err)
	}

	byKind := make(map[string]contract.Requirement, len(c.Requires))
	for _, r := range c.Requires {
		byKind[r.Kind] = r
	}

	if hr := byKind["lifecycle-signal"].HardRequired; hr == nil || *hr {
		t.Errorf("lifecycle-signal hardRequired = %v, want false (always soft per scaffold table)", hr)
	}
	if hr := byKind["blocking-gate"].HardRequired; hr == nil || !*hr {
		t.Errorf("blocking-gate hardRequired = %v, want true (--hard applies)", hr)
	}
	if hr := byKind["executable-path"].HardRequired; hr == nil || !*hr {
		t.Errorf("executable-path hardRequired = %v, want true (--hard applies)", hr)
	}
}

// TestRunInit_HardMustBeSubsetOfKinds confirms --hard naming a kind
// outside the chosen --kinds set is a clear error, creating nothing.
func TestRunInit_HardMustBeSubsetOfKinds(t *testing.T) {
	dir := t.TempDir()

	var stdout, stderr bytes.Buffer
	code := run([]string{
		"init", dir, "--name", "acme-tool",
		"--kinds", "lifecycle-signal",
		"--hard", "blocking-gate",
	}, &stdout, &stderr)
	if code != 1 {
		t.Fatalf("run(init) = %d, want 1 (stdout=%s stderr=%s)", code, stdout.String(), stderr.String())
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read dir: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("dir entries = %v, want none created", entries)
	}
}

// TestRunInit_NameDefaultsToDirBasenameAndCreatesIntermediateDirs
// confirms the name defaults to the target directory's basename, and
// that intermediate directories are created as needed.
func TestRunInit_NameDefaultsToDirBasenameAndCreatesIntermediateDirs(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "nested", "acme-tool")

	var stdout, stderr bytes.Buffer
	code := run([]string{"init", dir, "--yes"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("run(init) = %d, want 0 (stdout=%s stderr=%s)", code, stdout.String(), stderr.String())
	}

	raw, err := os.ReadFile(filepath.Join(dir, "plugin.json"))
	if err != nil {
		t.Fatalf("read plugin.json: %v", err)
	}
	var m struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("unmarshal plugin.json: %v", err)
	}
	if m.Name != "acme-tool" {
		t.Errorf("plugin.json name = %q, want %q (dir basename)", m.Name, "acme-tool")
	}
	if _, err := os.Stat(filepath.Join(dir, "bin")); !os.IsNotExist(err) {
		t.Errorf("no executable-path kind chosen, expected no bin/ dir, stat err = %v", err)
	}
}

// TestRunInit_HardBlockingGateReadmeNotesAllowRefuse confirms the
// scaffolded README's "Next steps" section warns that `build` will
// refuse on opencode (no native stop gate) when blocking-gate is
// hard-required, naming --allow-refuse as the way past it.
func TestRunInit_HardBlockingGateReadmeNotesAllowRefuse(t *testing.T) {
	dir := t.TempDir()

	var stdout, stderr bytes.Buffer
	code := run([]string{
		"init", dir, "--name", "acme-tool",
		"--kinds", "lifecycle-signal,blocking-gate",
		"--hard", "blocking-gate",
	}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("run(init) = %d, want 0 (stdout=%s stderr=%s)", code, stdout.String(), stderr.String())
	}

	body, err := os.ReadFile(filepath.Join(dir, "README.md"))
	if err != nil {
		t.Fatalf("read README.md: %v", err)
	}
	if !strings.Contains(string(body), "--allow-refuse") {
		t.Errorf("README.md = %q, want a note about --allow-refuse when blocking-gate is hard-required", body)
	}
}

// TestRunInit_SoftBlockingGateReadmeOmitsAllowRefuseNote confirms the
// same note is absent when blocking-gate is left soft (the default):
// build succeeds everywhere, so there's nothing to warn about.
func TestRunInit_SoftBlockingGateReadmeOmitsAllowRefuseNote(t *testing.T) {
	dir := t.TempDir()

	var stdout, stderr bytes.Buffer
	code := run([]string{
		"init", dir, "--name", "acme-tool",
		"--kinds", "lifecycle-signal,blocking-gate",
	}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("run(init) = %d, want 0 (stdout=%s stderr=%s)", code, stdout.String(), stderr.String())
	}

	body, err := os.ReadFile(filepath.Join(dir, "README.md"))
	if err != nil {
		t.Fatalf("read README.md: %v", err)
	}
	if strings.Contains(string(body), "--allow-refuse") {
		t.Errorf("README.md = %q, want no --allow-refuse note when blocking-gate is soft", body)
	}
}

func TestInitSkillKind(t *testing.T) {
	dir := t.TempDir()
	var out, errb bytes.Buffer
	code := run([]string{"init", filepath.Join(dir, "my-tool"), "--kinds", "skill", "--yes"}, &out, &errb)
	if code != 0 {
		t.Fatalf("init failed: %s", errb.String())
	}

	raw, err := os.ReadFile(filepath.Join(dir, "my-tool", "skills", "my-tool", "SKILL.md"))
	if err != nil {
		t.Fatalf("SKILL.md not scaffolded: %v", err)
	}
	if !strings.Contains(string(raw), "name: my-tool") {
		t.Fatalf("frontmatter name must equal bundle name:\n%s", raw)
	}

	manifest, _ := os.ReadFile(filepath.Join(dir, "my-tool", "plugin.json"))
	if !strings.Contains(string(manifest), `"kind": "skill"`) {
		t.Fatalf("plugin.json missing skill requirement:\n%s", manifest)
	}
}

func TestInitSkillPlusGateWiresFallback(t *testing.T) {
	dir := t.TempDir()
	var out, errb bytes.Buffer
	code := run([]string{"init", filepath.Join(dir, "b"), "--kinds", "skill,blocking-gate",
		"--hard", "blocking-gate", "--yes"}, &out, &errb)
	if code != 0 {
		t.Fatalf("init failed: %s", errb.String())
	}

	manifest, _ := os.ReadFile(filepath.Join(dir, "b", "plugin.json"))
	if !strings.Contains(string(manifest), `"fallbackSkill": "skill"`) {
		t.Fatalf("gate must reference the skill:\n%s", manifest)
	}
	if !strings.Contains(string(manifest), `"minTier": "T3"`) {
		t.Fatalf("gate minTier must be T3 when a skill is present:\n%s", manifest)
	}

	readme, _ := os.ReadFile(filepath.Join(dir, "b", "README.md"))
	if strings.Contains(string(readme), "--allow-refuse") {
		t.Fatalf("fallback-wired hard gate must not carry the refusal warning:\n%s", readme)
	}
	if !strings.Contains(string(readme), "instruction fallback") {
		t.Fatalf("README must explain the T3 fallback:\n%s", readme)
	}
}

func TestInitGateWithoutSkillKeepsT1(t *testing.T) {
	dir := t.TempDir()
	var out, errb bytes.Buffer
	if code := run([]string{"init", filepath.Join(dir, "b"), "--kinds", "blocking-gate",
		"--hard", "blocking-gate", "--yes"}, &out, &errb); code != 0 {
		t.Fatalf("init failed: %s", errb.String())
	}
	manifest, _ := os.ReadFile(filepath.Join(dir, "b", "plugin.json"))
	if !strings.Contains(string(manifest), `"minTier": "T1"`) {
		t.Fatalf("lone gate keeps T1:\n%s", manifest)
	}
	readme, _ := os.ReadFile(filepath.Join(dir, "b", "README.md"))
	if !strings.Contains(string(readme), "--allow-refuse") {
		t.Fatalf("lone hard gate keeps the refusal warning:\n%s", readme)
	}
}

func TestInitScaffoldWithSkillBuilds(t *testing.T) {
	dir := t.TempDir()
	bundle := filepath.Join(dir, "b")
	var out, errb bytes.Buffer
	if code := run([]string{"init", bundle, "--kinds", "skill,blocking-gate",
		"--hard", "blocking-gate", "--yes"}, &out, &errb); code != 0 {
		t.Fatalf("init failed: %s", errb.String())
	}

	// Provision a dispatcher stub so build's ensureDispatcher passes,
	// then build against the embedded target defs. With minTier T3 and
	// the fallback wired, the hard gate must NOT refuse on opencode, so
	// no --allow-refuse is passed.
	hook := filepath.Join(bundle, "bin", dispatcherName())
	if err := os.MkdirAll(filepath.Dir(hook), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(hook, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	out.Reset()
	errb.Reset()
	if code := run([]string{"build", bundle}, &out, &errb); code != 0 {
		t.Fatalf("scaffold with skill must build refusal-free: %s", errb.String())
	}
}
