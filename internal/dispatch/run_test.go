package dispatch_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/larstonder/whippletree/internal/contract"
	"github.com/larstonder/whippletree/internal/dispatch"
)

// newBundle builds a temp bundle directory with a vendored .whippletree/
// contract.json (from the kb-example fixture), a vendored
// .whippletree/targets/codex.yaml (copied from the repo's shared
// targets/), and one handler script per entry in handlers (keyed by
// filename under handlers/, written 0755).
func newBundle(t *testing.T, handlers map[string]string) string {
	t.Helper()
	dir := t.TempDir()

	raw, err := os.ReadFile("../tier/testdata/kb-example.json")
	if err != nil {
		t.Fatalf("read kb-example fixture: %v", err)
	}
	c, err := contract.Parse(raw)
	if err != nil {
		t.Fatalf("contract.Parse: %v", err)
	}
	body, err := json.Marshal(c)
	if err != nil {
		t.Fatalf("marshal vendored contract: %v", err)
	}

	vendorDir := filepath.Join(dir, ".whippletree")
	if err := os.MkdirAll(vendorDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(vendorDir, "contract.json"), body, 0o644); err != nil {
		t.Fatal(err)
	}

	targetsDir := filepath.Join(vendorDir, "targets")
	if err := os.MkdirAll(targetsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	targetSrc, err := os.ReadFile("../../targets/codex/target.yaml")
	if err != nil {
		t.Fatalf("read codex target.yaml: %v", err)
	}
	if err := os.WriteFile(filepath.Join(targetsDir, "codex.yaml"), targetSrc, 0o644); err != nil {
		t.Fatal(err)
	}

	handlersDir := filepath.Join(dir, "handlers")
	if err := os.MkdirAll(handlersDir, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, script := range handlers {
		if err := os.WriteFile(filepath.Join(handlersDir, name), []byte(script), 0o755); err != nil {
			t.Fatal(err)
		}
	}

	return dir
}

func TestRunExecutesMatchingHandlers(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "marker.json")
	script := fmt.Sprintf("#!/bin/bash\ncat > %q\nexit 0\n", marker)
	bundle := newBundle(t, map[string]string{"pull.sh": script})

	stdin := []byte(`{"session_id":"s1","transcript_path":"/tmp/t.jsonl","cwd":"/tmp/proj","hook_event_name":"SessionStart"}`)

	var stdout, stderr bytes.Buffer
	code := dispatch.Run(bundle, "session-start", "codex", stdin, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("Run = %d, want 0 (stderr: %s)", code, stderr.String())
	}

	got, err := os.ReadFile(marker)
	if err != nil {
		t.Fatalf("read marker: %v", err)
	}
	if !strings.Contains(string(got), `"event":"session-start"`) {
		t.Errorf("marker = %s, want to contain %q", got, `"event":"session-start"`)
	}
}

func TestRunBlockPropagatesExit2AndStderr(t *testing.T) {
	script := "#!/bin/bash\necho \"need capture\" >&2\nexit 2\n"
	bundle := newBundle(t, map[string]string{"capture.sh": script})

	stdin, err := os.ReadFile("testdata/codex-stop.json")
	if err != nil {
		t.Fatalf("read codex-stop fixture: %v", err)
	}

	var stdout, stderr bytes.Buffer
	code := dispatch.Run(bundle, "turn-end", "codex", stdin, &stdout, &stderr)
	if code != 2 {
		t.Fatalf("Run = %d, want 2 (stderr: %s)", code, stderr.String())
	}
	if !strings.Contains(stderr.String(), "need capture") {
		t.Errorf("stderr = %q, want to contain %q", stderr.String(), "need capture")
	}
}

func TestRunUnknownExitFailsOpen(t *testing.T) {
	script := "#!/bin/bash\nexit 7\n"
	bundle := newBundle(t, map[string]string{"capture.sh": script})

	stdin, err := os.ReadFile("testdata/codex-stop.json")
	if err != nil {
		t.Fatalf("read codex-stop fixture: %v", err)
	}

	var stdout, stderr bytes.Buffer
	code := dispatch.Run(bundle, "turn-end", "codex", stdin, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("Run = %d, want 0 (fail-open)", code)
	}
	if !strings.Contains(stderr.String(), "exited 7") {
		t.Errorf("stderr = %q, want to contain %q", stderr.String(), "exited 7")
	}
}

// TestRunForwardsStderrOnCleanExit: a handler's stderr must be
// forwarded even when it exits 0 (or any non-2 code), not just on the
// exit-2 block path.
func TestRunForwardsStderrOnCleanExit(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "marker.json")
	script := fmt.Sprintf("#!/bin/bash\ncat > %q\necho \"note: informational\" >&2\nexit 0\n", marker)
	bundle := newBundle(t, map[string]string{"pull.sh": script})

	stdin := []byte(`{"session_id":"s1","transcript_path":"/tmp/t.jsonl","cwd":"/tmp/proj","hook_event_name":"SessionStart"}`)

	var stdout, stderr bytes.Buffer
	code := dispatch.Run(bundle, "session-start", "codex", stdin, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("Run = %d, want 0 (stderr: %s)", code, stderr.String())
	}
	if !strings.Contains(stderr.String(), "note: informational") {
		t.Errorf("stderr = %q, want it to contain the handler's stderr output even on a clean exit", stderr.String())
	}
}

// TestRunForwardsHandlerStdout: a handler's stdout is forwarded
// verbatim to the dispatcher's own stdout.
func TestRunForwardsHandlerStdout(t *testing.T) {
	script := "#!/bin/sh\necho \"CTX-LINE-1\"\nexit 0\n"
	bundle := newBundle(t, map[string]string{"pull.sh": script})

	stdin := []byte(`{"session_id":"s1","transcript_path":"/tmp/t.jsonl","cwd":"/tmp/proj","hook_event_name":"SessionStart"}`)

	var stdout, stderr bytes.Buffer
	code := dispatch.Run(bundle, "session-start", "codex", stdin, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("Run = %d, want 0 (stderr: %s)", code, stderr.String())
	}
	if got, want := stdout.String(), "CTX-LINE-1\n"; got != want {
		t.Errorf("stdout = %q, want %q", got, want)
	}
}

// TestRunForwardsStdoutOnBlock: stdout is forwarded even when the
// handler blocks (exit 2), the same as stderr already is.
func TestRunForwardsStdoutOnBlock(t *testing.T) {
	script := "#!/bin/sh\necho \"CTX-ON-BLOCK\"\necho \"reason\" >&2\nexit 2\n"
	bundle := newBundle(t, map[string]string{"capture.sh": script})

	stdin, err := os.ReadFile("testdata/codex-stop.json")
	if err != nil {
		t.Fatalf("read codex-stop fixture: %v", err)
	}

	var stdout, stderr bytes.Buffer
	code := dispatch.Run(bundle, "turn-end", "codex", stdin, &stdout, &stderr)
	if code != 2 {
		t.Fatalf("Run = %d, want 2 (stderr: %s)", code, stderr.String())
	}
	if got, want := stdout.String(), "CTX-ON-BLOCK\n"; got != want {
		t.Errorf("stdout = %q, want %q", got, want)
	}
	if !strings.Contains(stderr.String(), "reason") {
		t.Errorf("stderr = %q, want to contain %q", stderr.String(), "reason")
	}
}

// TestRunForwardsStdoutInHandlerOrder: two requirements bound to the
// same event forward their handlers' stdout in invocation (contract)
// order. kb-example.json's fixture has exactly one requirement per
// event, so this test vendors its own two-requirement contract rather
// than going through newBundle.
func TestRunForwardsStdoutInHandlerOrder(t *testing.T) {
	dir := t.TempDir()

	vendorDir := filepath.Join(dir, ".whippletree")
	if err := os.MkdirAll(vendorDir, 0o755); err != nil {
		t.Fatal(err)
	}
	contractJSON := `{"contractVersion":"1.0.0","requires":[
		{"id":"first","kind":"lifecycle-signal","event":"session-start","minTier":"T2","hardRequired":false,"handler":"./handlers/first.sh"},
		{"id":"second","kind":"lifecycle-signal","event":"session-start","minTier":"T2","hardRequired":false,"handler":"./handlers/second.sh"}
	]}`
	if err := os.WriteFile(filepath.Join(vendorDir, "contract.json"), []byte(contractJSON), 0o644); err != nil {
		t.Fatal(err)
	}

	targetsDir := filepath.Join(vendorDir, "targets")
	if err := os.MkdirAll(targetsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	targetSrc, err := os.ReadFile("../../targets/codex/target.yaml")
	if err != nil {
		t.Fatalf("read codex target.yaml: %v", err)
	}
	if err := os.WriteFile(filepath.Join(targetsDir, "codex.yaml"), targetSrc, 0o644); err != nil {
		t.Fatal(err)
	}

	handlersDir := filepath.Join(dir, "handlers")
	if err := os.MkdirAll(handlersDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(handlersDir, "first.sh"), []byte("#!/bin/sh\necho \"FIRST\"\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(handlersDir, "second.sh"), []byte("#!/bin/sh\necho \"SECOND\"\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	stdin := []byte(`{"session_id":"s1","transcript_path":"/tmp/t.jsonl","cwd":"/tmp/proj","hook_event_name":"SessionStart"}`)

	var stdout, stderr bytes.Buffer
	code := dispatch.Run(dir, "session-start", "codex", stdin, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("Run = %d, want 0 (stderr: %s)", code, stderr.String())
	}
	if got, want := stdout.String(), "FIRST\nSECOND\n"; got != want {
		t.Errorf("stdout = %q, want %q", got, want)
	}
}

// TestRunEmptyStdoutWritesNothing: a silent handler must not cause any
// bytes, not even a blank line, to land on the dispatcher's stdout.
func TestRunEmptyStdoutWritesNothing(t *testing.T) {
	script := "#!/bin/sh\nexit 0\n"
	bundle := newBundle(t, map[string]string{"pull.sh": script})

	stdin := []byte(`{"session_id":"s1","transcript_path":"/tmp/t.jsonl","cwd":"/tmp/proj","hook_event_name":"SessionStart"}`)

	var stdout, stderr bytes.Buffer
	code := dispatch.Run(bundle, "session-start", "codex", stdin, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("Run = %d, want 0 (stderr: %s)", code, stderr.String())
	}
	if got := stdout.String(); got != "" {
		t.Errorf("stdout = %q, want empty", got)
	}
}

func TestRunNoMatchingRequirementIsNoop(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "marker.json")
	script := fmt.Sprintf("#!/bin/bash\ncat > %q\nexit 0\n", marker)
	// A handler exists in the bundle (bound to session-start), but no
	// requirement matches "session-end" — it must never run.
	bundle := newBundle(t, map[string]string{"pull.sh": script})

	var stdout, stderr bytes.Buffer
	code := dispatch.Run(bundle, "session-end", "codex", []byte(`{}`), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("Run = %d, want 0 (stderr: %s)", code, stderr.String())
	}
	if _, err := os.Stat(marker); err == nil {
		t.Errorf("marker file exists, want no handler invoked")
	}
}

// envDumpScript writes a handler that dumps every ADAPTER_-prefixed
// environment variable it sees to marker, one KEY=VALUE line each,
// then exits 0.
func envDumpScript(marker string) string {
	return fmt.Sprintf("#!/bin/bash\nenv | grep '^ADAPTER_' > %q\nexit 0\n", marker)
}

// readEnvMarker parses a marker file written by envDumpScript into a
// map of env var name to value. A key present with an empty value
// (e.g. "ADAPTER_STOP_ACTIVE=") is recorded as "", distinct from a key
// that never appears at all.
func readEnvMarker(t *testing.T, marker string) map[string]string {
	t.Helper()
	raw, err := os.ReadFile(marker)
	if err != nil {
		t.Fatalf("read env marker: %v", err)
	}
	got := map[string]string{}
	for _, line := range strings.Split(strings.TrimRight(string(raw), "\n"), "\n") {
		if line == "" {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			t.Fatalf("env marker line %q: no '='", line)
		}
		got[k] = v
	}
	return got
}

// TestRunProjectsADAPTERVarsFromNormalizedEvent pins the additive
// ADAPTER_PRIMITIVE/ADAPTER_STOP_ACTIVE/ADAPTER_CWD/ADAPTER_PATH env
// vars runHandler must project from the normalized Event, alongside
// the existing ADAPTER_EVENT/ADAPTER_TARGET, always set (empty string
// when not applicable).
func TestRunProjectsADAPTERVarsFromNormalizedEvent(t *testing.T) {
	t.Run("turn-end with stop hook active", func(t *testing.T) {
		marker := filepath.Join(t.TempDir(), "marker.env")
		bundle := newBundle(t, map[string]string{"capture.sh": envDumpScript(marker)})
		stdin := []byte(`{"session_id":"s1","transcript_path":"/tmp/r.jsonl","cwd":"/tmp/proj","hook_event_name":"Stop","stop_hook_active":true}`)

		var stdout, stderr bytes.Buffer
		code := dispatch.Run(bundle, "turn-end", "codex", stdin, &stdout, &stderr)
		if code != 0 {
			t.Fatalf("Run = %d, want 0 (stderr: %s)", code, stderr.String())
		}

		env := readEnvMarker(t, marker)
		if got, want := env["ADAPTER_PRIMITIVE"], "turn-end"; got != want {
			t.Errorf("ADAPTER_PRIMITIVE = %q, want %q", got, want)
		}
		if got, want := env["ADAPTER_STOP_ACTIVE"], "true"; got != want {
			t.Errorf("ADAPTER_STOP_ACTIVE = %q, want %q", got, want)
		}
	})

	t.Run("session-start with no loop guard", func(t *testing.T) {
		marker := filepath.Join(t.TempDir(), "marker.env")
		bundle := newBundle(t, map[string]string{"pull.sh": envDumpScript(marker)})
		stdin := []byte(`{"session_id":"s1","transcript_path":"/tmp/t.jsonl","cwd":"/tmp/proj","hook_event_name":"SessionStart"}`)

		var stdout, stderr bytes.Buffer
		code := dispatch.Run(bundle, "session-start", "codex", stdin, &stdout, &stderr)
		if code != 0 {
			t.Fatalf("Run = %d, want 0 (stderr: %s)", code, stderr.String())
		}

		env := readEnvMarker(t, marker)
		if got, want := env["ADAPTER_PRIMITIVE"], "session-start"; got != want {
			t.Errorf("ADAPTER_PRIMITIVE = %q, want %q", got, want)
		}
		if got, ok := env["ADAPTER_STOP_ACTIVE"]; !ok || got != "" {
			t.Errorf("ADAPTER_STOP_ACTIVE = %q (present=%v), want empty", got, ok)
		}
		if got, want := env["ADAPTER_CWD"], "/tmp/proj"; got != want {
			t.Errorf("ADAPTER_CWD = %q, want %q", got, want)
		}
	})

	t.Run("file-read alias", func(t *testing.T) {
		marker := filepath.Join(t.TempDir(), "marker.env")
		bundle := newBundle(t, map[string]string{"log-read.sh": envDumpScript(marker)})
		stdin, err := os.ReadFile("testdata/codex-posttooluse-shell.json")
		if err != nil {
			t.Fatalf("read codex-posttooluse-shell fixture: %v", err)
		}

		var stdout, stderr bytes.Buffer
		code := dispatch.Run(bundle, "file-read", "codex", stdin, &stdout, &stderr)
		if code != 0 {
			t.Fatalf("Run = %d, want 0 (stderr: %s)", code, stderr.String())
		}

		env := readEnvMarker(t, marker)
		if got, want := env["ADAPTER_PRIMITIVE"], "tool-post"; got != want {
			t.Errorf("ADAPTER_PRIMITIVE = %q, want %q", got, want)
		}
		if got, want := env["ADAPTER_EVENT"], "file-read"; got != want {
			t.Errorf("ADAPTER_EVENT = %q, want %q (existing var unchanged)", got, want)
		}
		if got, want := env["ADAPTER_PATH"], "hello.txt"; got != want {
			t.Errorf("ADAPTER_PATH = %q, want %q", got, want)
		}
	})

	t.Run("event with no paths", func(t *testing.T) {
		marker := filepath.Join(t.TempDir(), "marker.env")
		bundle := newBundle(t, map[string]string{"capture.sh": envDumpScript(marker)})
		stdin, err := os.ReadFile("testdata/codex-stop.json")
		if err != nil {
			t.Fatalf("read codex-stop fixture: %v", err)
		}

		var stdout, stderr bytes.Buffer
		code := dispatch.Run(bundle, "turn-end", "codex", stdin, &stdout, &stderr)
		if code != 0 {
			t.Fatalf("Run = %d, want 0 (stderr: %s)", code, stderr.String())
		}

		env := readEnvMarker(t, marker)
		if got, want := env["ADAPTER_STOP_ACTIVE"], "false"; got != want {
			t.Errorf("ADAPTER_STOP_ACTIVE = %q, want %q", got, want)
		}
		if got, ok := env["ADAPTER_PATH"]; !ok || got != "" {
			t.Errorf("ADAPTER_PATH = %q (present=%v), want empty", got, ok)
		}
	})
}
