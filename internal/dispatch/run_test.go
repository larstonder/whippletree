package dispatch_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/larstonder/adapter-sdk/internal/contract"
	"github.com/larstonder/adapter-sdk/internal/dispatch"
)

// newBundle builds a temp bundle directory with a vendored .adapter-sdk/
// contract.json (from the kb-example fixture), a vendored
// .adapter-sdk/targets/codex.yaml (copied from the repo's shared
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

	sdkDir := filepath.Join(dir, ".adapter-sdk")
	if err := os.MkdirAll(sdkDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sdkDir, "contract.json"), body, 0o644); err != nil {
		t.Fatal(err)
	}

	targetsDir := filepath.Join(sdkDir, "targets")
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

	var stderr bytes.Buffer
	code := dispatch.Run(bundle, "session-start", "codex", stdin, &stderr)
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

	var stderr bytes.Buffer
	code := dispatch.Run(bundle, "turn-end", "codex", stdin, &stderr)
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

	var stderr bytes.Buffer
	code := dispatch.Run(bundle, "turn-end", "codex", stdin, &stderr)
	if code != 0 {
		t.Fatalf("Run = %d, want 0 (fail-open)", code)
	}
	if !strings.Contains(stderr.String(), "exited 7") {
		t.Errorf("stderr = %q, want to contain %q", stderr.String(), "exited 7")
	}
}

// TestRunForwardsStderrOnCleanExit is the regression test for finding
// 12: a handler's stderr must be forwarded even when it exits 0 (or any
// non-2 code), not just on the exit-2 block path.
func TestRunForwardsStderrOnCleanExit(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "marker.json")
	script := fmt.Sprintf("#!/bin/bash\ncat > %q\necho \"note: informational\" >&2\nexit 0\n", marker)
	bundle := newBundle(t, map[string]string{"pull.sh": script})

	stdin := []byte(`{"session_id":"s1","transcript_path":"/tmp/t.jsonl","cwd":"/tmp/proj","hook_event_name":"SessionStart"}`)

	var stderr bytes.Buffer
	code := dispatch.Run(bundle, "session-start", "codex", stdin, &stderr)
	if code != 0 {
		t.Fatalf("Run = %d, want 0 (stderr: %s)", code, stderr.String())
	}
	if !strings.Contains(stderr.String(), "note: informational") {
		t.Errorf("stderr = %q, want it to contain the handler's stderr output even on a clean exit", stderr.String())
	}
}

func TestRunNoMatchingRequirementIsNoop(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "marker.json")
	script := fmt.Sprintf("#!/bin/bash\ncat > %q\nexit 0\n", marker)
	// A handler exists in the bundle (bound to session-start), but no
	// requirement matches "session-end" — it must never run.
	bundle := newBundle(t, map[string]string{"pull.sh": script})

	var stderr bytes.Buffer
	code := dispatch.Run(bundle, "session-end", "codex", []byte(`{}`), &stderr)
	if code != 0 {
		t.Fatalf("Run = %d, want 0 (stderr: %s)", code, stderr.String())
	}
	if _, err := os.Stat(marker); err == nil {
		t.Errorf("marker file exists, want no handler invoked")
	}
}
