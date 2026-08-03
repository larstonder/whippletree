package skillfile

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/larstonder/whippletree/internal/contract"
)

func writeSkill(t *testing.T, dir, name, frontmatter, body string) string {
	t.Helper()
	skillDir := filepath.Join(dir, "skills", name)
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	content := "---\n" + frontmatter + "---\n" + body
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return skillDir
}

func TestParseFile(t *testing.T) {
	dir := t.TempDir()
	writeSkill(t, dir, "cap", "name: cap\ndescription: Captures notes.\n", "Body text.\n")

	doc, err := ParseFile(filepath.Join(dir, "skills", "cap", "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	if doc.Name != "cap" || doc.Description != "Captures notes." {
		t.Fatalf("got name %q description %q", doc.Name, doc.Description)
	}
}

func TestParseFileErrors(t *testing.T) {
	dir := t.TempDir()
	cases := []struct {
		name, frontmatter, want string
	}{
		{"noname", "description: d\n", "missing name"},
		{"nodesc", "name: x\n", "missing description"},
		{"emptydesc", "name: x\ndescription: \"\"\n", "missing description"},
		{"multiline", "name: x\ndescription: |\n  two\n  lines\n", "single-line"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			writeSkill(t, dir, tc.name, tc.frontmatter, "b\n")
			_, err := ParseFile(filepath.Join(dir, "skills", tc.name, "SKILL.md"))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("want error containing %q, got %v", tc.want, err)
			}
		})
	}

	if _, err := ParseFile(filepath.Join(dir, "nope", "SKILL.md")); err == nil {
		t.Fatal("want error for missing file")
	}
}

func TestCheck(t *testing.T) {
	no := false
	dir := t.TempDir()
	writeSkill(t, dir, "cap", "name: cap\ndescription: d.\n", "b\n")
	writeSkill(t, dir, "mismatch", "name: other\ndescription: d.\n", "b\n")

	ok := contract.Requirement{ID: "s", Kind: "skill", Path: "./skills/cap", HardRequired: &no}
	if err := Check(dir, ok); err != nil {
		t.Fatalf("valid skill rejected: %v", err)
	}

	bad := contract.Requirement{ID: "s", Kind: "skill", Path: "./skills/mismatch", HardRequired: &no}
	err := Check(dir, bad)
	if err == nil || !strings.Contains(err.Error(), `frontmatter name "other" must equal its directory name "mismatch"`) {
		t.Fatalf("want dir-name mismatch error, got %v", err)
	}

	gone := contract.Requirement{ID: "s", Kind: "skill", Path: "./skills/gone", HardRequired: &no}
	if err := Check(dir, gone); err == nil {
		t.Fatal("want error for missing skill dir")
	}
}
