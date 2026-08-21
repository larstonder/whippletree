package skillfile

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"whippletree.dev/internal/contract"
)

func expandFixture(t *testing.T, description string, exps []Expansion) (dst string) {
	t.Helper()
	dir := t.TempDir()
	src := filepath.Join(dir, "skills", "cap")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatal(err)
	}
	content := "---\nname: cap\ndescription: " + description + "\n---\nAuthored body.\n"
	if err := os.WriteFile(filepath.Join(src, "SKILL.md"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "helper.sh"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	dst = filepath.Join(dir, "out", "cap")
	if err := ExpandDir(src, dst, exps, "1.2.3"); err != nil {
		t.Fatal(err)
	}
	return dst
}

func readOut(t *testing.T, dst string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(dst, "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func TestExpandTurnEndGolden(t *testing.T) {
	dst := expandFixture(t, "Captures notes.", []Expansion{{
		Event: "turn-end", ReqID: "capture-gate", Kind: "blocking-gate",
		Handler: "./handlers/capture.sh", Target: "opencode",
	}})
	got := readOut(t, dst)

	want, err := os.ReadFile(filepath.Join("testdata", "expand-turn-end.golden.md"))
	if err != nil {
		t.Fatal(err)
	}
	if got != string(want) {
		t.Fatalf("golden mismatch.\n--- got ---\n%s\n--- want ---\n%s", got, string(want))
	}

	// The expanded template embeds the handler command, so the
	// placeholder is present by construction; install's replace-all
	// relies on this rather than on an occurrence-count error.
	if !strings.Contains(got, Placeholder) {
		t.Fatal("expanded SKILL.md must contain the bundle-root placeholder")
	}
	if _, err := os.Stat(filepath.Join(dst, "helper.sh")); err != nil {
		t.Fatalf("supporting file not copied: %v", err)
	}
	if !strings.Contains(got, contract.T3Fidelity) {
		t.Fatal("expanded SKILL.md must pin the same fidelity wording as contract.T3Fidelity")
	}
}

func TestExpandSessionStartGolden(t *testing.T) {
	dst := expandFixture(t, "Captures notes.", []Expansion{{
		Event: "session-start", ReqID: "pull-signal", Kind: "lifecycle-signal",
		Handler: "./handlers/pull.sh", Target: "cursor-x",
	}})
	got := readOut(t, dst)
	want, err := os.ReadFile(filepath.Join("testdata", "expand-session-start.golden.md"))
	if err != nil {
		t.Fatal(err)
	}
	if got != string(want) {
		t.Fatalf("golden mismatch.\n--- got ---\n%s", got)
	}
	if strings.Contains(got, "ADAPTER_STOP_ACTIVE=false") || strings.Contains(got, "ADAPTER_STOP_ACTIVE=true") {
		t.Fatal("session-start command must leave ADAPTER_STOP_ACTIVE empty")
	}
}

func TestExpandQuotedDescription(t *testing.T) {
	dst := expandFixture(t, `"Captures notes."`, []Expansion{{
		Event: "turn-end", ReqID: "g", Kind: "blocking-gate",
		Handler: "./handlers/capture.sh", Target: "opencode",
	}})
	got := readOut(t, dst)
	wantLine := `description: "Captures notes. Use this skill before writing any message that declares the task complete."`
	if !strings.Contains(got, wantLine) {
		t.Fatalf("clause must land inside the quotes.\nwant line: %s\ngot:\n%s", wantLine, got)
	}
}

func TestExpandUnexpandedVariantGetsMarkerOnly(t *testing.T) {
	dst := expandFixture(t, "Captures notes.", nil)
	got := readOut(t, dst)
	if !strings.Contains(got, "compiled-by: whippletree 1.2.3") {
		t.Fatalf("marker missing:\n%s", got)
	}
	if strings.Contains(got, "Manual step") || strings.Contains(got, Placeholder) {
		t.Fatalf("unexpanded variant must not carry expansion content:\n%s", got)
	}
	if !strings.Contains(got, "description: Captures notes.\n") {
		t.Fatalf("description must be untouched:\n%s", got)
	}
}

func TestExpandTwoHooks(t *testing.T) {
	dst := expandFixture(t, "Captures notes.", []Expansion{
		{Event: "turn-end", ReqID: "g", Kind: "blocking-gate", Handler: "./handlers/capture.sh", Target: "x"},
		{Event: "session-start", ReqID: "l", Kind: "lifecycle-signal", Handler: "./handlers/pull.sh", Target: "x"},
	})
	got := readOut(t, dst)
	first := strings.Index(got, "declares the task complete")
	second := strings.Index(got, "at the start of a session")
	if first == -1 || second == -1 || first > second {
		t.Fatalf("both clauses required, contract order:\n%s", got)
	}
	if strings.Count(got, "## Manual step on this harness") != 2 {
		t.Fatalf("want two body sections:\n%s", got)
	}
}
