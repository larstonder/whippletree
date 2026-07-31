package dispatch_test

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/larstonder/whippletree/internal/dispatch"
	"github.com/larstonder/whippletree/internal/target"
)

// loadTarget loads the named target's target.yaml from the repo's shared
// targets/ directory.
func loadTarget(t *testing.T, name string) *target.Def {
	t.Helper()
	defs, err := target.LoadDir("../../targets")
	if err != nil {
		t.Fatalf("target.LoadDir: %v", err)
	}
	td, ok := defs[name]
	if !ok {
		t.Fatalf("target %q not found in ../../targets", name)
	}
	return td
}

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return b
}

func TestNormalizeCodexShellRead(t *testing.T) {
	td := loadTarget(t, "codex")
	stdin := fixture(t, "codex-posttooluse-shell.json")

	ev, err := dispatch.Normalize("file-read", td, stdin)
	if err != nil {
		t.Fatalf("Normalize: %v", err)
	}

	if ev.Event != "tool-post" {
		t.Errorf("Event = %q, want %q", ev.Event, "tool-post")
	}
	if ev.Alias != "file-read" {
		t.Errorf("Alias = %q, want %q", ev.Alias, "file-read")
	}
	if ev.ToolClass != "read" {
		t.Errorf("ToolClass = %q, want %q", ev.ToolClass, "read")
	}

	wantCommand := "rg --files -g 'hello.txt' && sed -n '1,120p' hello.txt"
	if ev.Command != wantCommand {
		t.Errorf("Command = %q, want %q", ev.Command, wantCommand)
	}

	wantPaths := []string{"hello.txt", "hello.txt"}
	if !reflect.DeepEqual(ev.Paths, wantPaths) {
		t.Errorf("Paths = %v, want %v", ev.Paths, wantPaths)
	}

	if ev.TranscriptPath != "/tmp/r.jsonl" {
		t.Errorf("TranscriptPath = %q, want %q", ev.TranscriptPath, "/tmp/r.jsonl")
	}
	if ev.CWD != "/tmp/proj" {
		t.Errorf("CWD = %q, want %q", ev.CWD, "/tmp/proj")
	}
	if ev.StopHookActive != nil {
		t.Errorf("StopHookActive = %v, want nil", ev.StopHookActive)
	}
	if string(ev.Raw) != string(stdin) {
		t.Errorf("Raw does not match stdin verbatim")
	}
}

func TestNormalizeClaudeDirectRead(t *testing.T) {
	td := loadTarget(t, "claude-code")
	stdin := fixture(t, "claude-posttooluse-read.json")

	ev, err := dispatch.Normalize("file-read", td, stdin)
	if err != nil {
		t.Fatalf("Normalize: %v", err)
	}

	if ev.Event != "tool-post" {
		t.Errorf("Event = %q, want %q", ev.Event, "tool-post")
	}
	if ev.Alias != "file-read" {
		t.Errorf("Alias = %q, want %q", ev.Alias, "file-read")
	}
	if ev.ToolClass != "read" {
		t.Errorf("ToolClass = %q, want %q", ev.ToolClass, "read")
	}

	if ev.Command != "" {
		t.Errorf("Command = %q, want empty", ev.Command)
	}

	wantPaths := []string{"/tmp/proj/notes.md"}
	if !reflect.DeepEqual(ev.Paths, wantPaths) {
		t.Errorf("Paths = %v, want %v", ev.Paths, wantPaths)
	}

	if ev.TranscriptPath != "/tmp/t.jsonl" {
		t.Errorf("TranscriptPath = %q, want %q", ev.TranscriptPath, "/tmp/t.jsonl")
	}
	if ev.CWD != "/tmp/proj" {
		t.Errorf("CWD = %q, want %q", ev.CWD, "/tmp/proj")
	}
}

// TestNormalizeClaudeSessionStart pins that a class-1 session-start
// payload is decoded as class-1 even though it carries a top-level
// "source" field. Claude Code's SessionStart hook input sets source to
// "startup", "resume", "clear" or "compact", which collides with the
// field the opencode envelope also uses; the backend on the target
// definition is what selects the dialect.
func TestNormalizeClaudeSessionStart(t *testing.T) {
	td := loadTarget(t, "claude-code")
	stdin := fixture(t, "claude-sessionstart.json")

	ev, err := dispatch.Normalize("session-start", td, stdin)
	if err != nil {
		t.Fatalf("Normalize: %v", err)
	}

	if ev.Event != "session-start" {
		t.Errorf("Event = %q, want %q", ev.Event, "session-start")
	}
	if ev.Alias != "" {
		t.Errorf("Alias = %q, want empty (session-start is a primitive)", ev.Alias)
	}
	if ev.TranscriptPath != "/tmp/t.jsonl" {
		t.Errorf("TranscriptPath = %q, want %q", ev.TranscriptPath, "/tmp/t.jsonl")
	}
	if ev.CWD != "/tmp/proj" {
		t.Errorf("CWD = %q, want %q", ev.CWD, "/tmp/proj")
	}
	if ev.StopHookActive != nil {
		t.Errorf("StopHookActive = %v, want nil", ev.StopHookActive)
	}
	if string(ev.Raw) != string(stdin) {
		t.Errorf("Raw does not match stdin verbatim")
	}
}

func TestNormalizeOpencodeSessionStart(t *testing.T) {
	td := loadTarget(t, "opencode")
	stdin := fixture(t, "opencode-event-session-created.json")

	ev, err := dispatch.Normalize("session-start", td, stdin)
	if err != nil {
		t.Fatalf("Normalize: %v", err)
	}

	if ev.Event != "session-start" {
		t.Errorf("Event = %q, want %q", ev.Event, "session-start")
	}
	if ev.Alias != "" {
		t.Errorf("Alias = %q, want empty (session-start is a primitive)", ev.Alias)
	}
	if ev.ToolClass != "" {
		t.Errorf("ToolClass = %q, want empty", ev.ToolClass)
	}
	if ev.CWD != "/tmp/proj" {
		t.Errorf("CWD = %q, want %q", ev.CWD, "/tmp/proj")
	}
	if ev.StopHookActive != nil {
		t.Errorf("StopHookActive = %v, want nil", ev.StopHookActive)
	}
	if string(ev.Raw) != string(stdin) {
		t.Errorf("Raw does not match stdin verbatim")
	}
}

func TestNormalizeOpencodeToolPostRead(t *testing.T) {
	td := loadTarget(t, "opencode")
	stdin := fixture(t, "opencode-tool-after-read.json")

	ev, err := dispatch.Normalize("file-read", td, stdin)
	if err != nil {
		t.Fatalf("Normalize: %v", err)
	}

	if ev.Event != "tool-post" {
		t.Errorf("Event = %q, want %q", ev.Event, "tool-post")
	}
	if ev.Alias != "file-read" {
		t.Errorf("Alias = %q, want %q", ev.Alias, "file-read")
	}
	if ev.ToolClass != "read" {
		t.Errorf("ToolClass = %q, want %q", ev.ToolClass, "read")
	}

	wantPaths := []string{"/tmp/ocprobe/proj/hello.txt"}
	if !reflect.DeepEqual(ev.Paths, wantPaths) {
		t.Errorf("Paths = %v, want %v", ev.Paths, wantPaths)
	}

	if ev.CWD != "/tmp/proj" {
		t.Errorf("CWD = %q, want %q", ev.CWD, "/tmp/proj")
	}
	if ev.StopHookActive != nil {
		t.Errorf("StopHookActive = %v, want nil", ev.StopHookActive)
	}
	if string(ev.Raw) != string(stdin) {
		t.Errorf("Raw does not match stdin verbatim")
	}
}

func TestNormalizeOpencodeToolPreRead(t *testing.T) {
	td := loadTarget(t, "opencode")
	stdin := fixture(t, "opencode-tool-before-read.json")

	ev, err := dispatch.Normalize("tool-pre", td, stdin)
	if err != nil {
		t.Fatalf("Normalize: %v", err)
	}

	if ev.Event != "tool-pre" {
		t.Errorf("Event = %q, want %q", ev.Event, "tool-pre")
	}
	if ev.Alias != "" {
		t.Errorf("Alias = %q, want empty (tool-pre is a primitive)", ev.Alias)
	}

	wantPaths := []string{"/tmp/ocprobe/proj/hello.txt"}
	if !reflect.DeepEqual(ev.Paths, wantPaths) {
		t.Errorf("Paths = %v, want %v", ev.Paths, wantPaths)
	}

	if ev.CWD != "/tmp/proj" {
		t.Errorf("CWD = %q, want %q", ev.CWD, "/tmp/proj")
	}
	if ev.StopHookActive != nil {
		t.Errorf("StopHookActive = %v, want nil", ev.StopHookActive)
	}
}

func TestNormalizeStopCarriesLoopGuard(t *testing.T) {
	td := loadTarget(t, "codex")
	stdin := fixture(t, "codex-stop.json")

	ev, err := dispatch.Normalize("turn-end", td, stdin)
	if err != nil {
		t.Fatalf("Normalize: %v", err)
	}

	if ev.Event != "turn-end" {
		t.Errorf("Event = %q, want %q", ev.Event, "turn-end")
	}
	if ev.Alias != "" {
		t.Errorf("Alias = %q, want empty (turn-end is a primitive)", ev.Alias)
	}
	if ev.ToolClass != "" {
		t.Errorf("ToolClass = %q, want empty", ev.ToolClass)
	}

	if ev.StopHookActive == nil {
		t.Fatal("StopHookActive = nil, want non-nil")
	}
	if *ev.StopHookActive != false {
		t.Errorf("*StopHookActive = %v, want false", *ev.StopHookActive)
	}
}
