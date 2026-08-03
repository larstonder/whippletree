// Package skillfile reads, checks, and (in expand.go) transforms a
// bundle's SKILL.md files. Parsing is line-based on purpose: authored
// files are never round-tripped through a YAML encoder, so their
// formatting survives untouched.
package skillfile

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/larstonder/whippletree/internal/contract"
)

// Doc is a parsed SKILL.md: the identity fields build and install need,
// plus the raw line split expansion splices into.
type Doc struct {
	Name        string
	Description string

	// lines is the full file split on "\n"; fmEnd is the index of the
	// closing "---" line of the frontmatter block that starts at line 0.
	lines []string
	fmEnd int
}

// ParseFile reads and parses path as a SKILL.md with YAML frontmatter.
// It enforces the contract's authoring rules: non-empty name, non-empty
// single-line description.
func ParseFile(path string) (*Doc, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}

	lines := strings.Split(string(raw), "\n")
	if len(lines) == 0 || strings.TrimRight(lines[0], " \t") != "---" {
		return nil, fmt.Errorf("%s: must start with a --- frontmatter fence", path)
	}
	fmEnd := -1
	for i := 1; i < len(lines); i++ {
		if strings.TrimRight(lines[i], " \t") == "---" {
			fmEnd = i
			break
		}
	}
	if fmEnd == -1 {
		return nil, fmt.Errorf("%s: frontmatter fence is never closed", path)
	}

	doc := &Doc{lines: lines, fmEnd: fmEnd}
	for i := 1; i < fmEnd; i++ {
		key, value, found := strings.Cut(lines[i], ":")
		if !found {
			continue
		}
		switch strings.TrimSpace(key) {
		case "name":
			doc.Name = unquote(strings.TrimSpace(value))
		case "description":
			v := strings.TrimSpace(value)
			if v == "|" || v == ">" || strings.HasPrefix(v, "|") || strings.HasPrefix(v, ">") {
				return nil, fmt.Errorf("%s: description must be a single-line scalar", path)
			}
			doc.Description = unquote(v)
		}
	}

	if doc.Name == "" {
		return nil, fmt.Errorf("%s: frontmatter is missing name", path)
	}
	if doc.Description == "" {
		return nil, fmt.Errorf("%s: frontmatter is missing description", path)
	}
	return doc, nil
}

// unquote strips one layer of matching single or double quotes.
func unquote(s string) string {
	if len(s) >= 2 && (s[0] == '"' && s[len(s)-1] == '"' || s[0] == '\'' && s[len(s)-1] == '\'') {
		return s[1 : len(s)-1]
	}
	return s
}

// Check verifies a skill requirement's on-disk shape under bundleDir:
// the SKILL.md exists and parses, and its frontmatter name equals the
// skill's directory name, the identity the plugin-dir discovery
// convention keys on.
func Check(bundleDir string, req contract.Requirement) error {
	skillDir := filepath.Join(bundleDir, filepath.FromSlash(strings.TrimPrefix(req.Path, "./")))
	doc, err := ParseFile(filepath.Join(skillDir, "SKILL.md"))
	if err != nil {
		return fmt.Errorf("requirement %s: %w", req.ID, err)
	}
	dirName := filepath.Base(skillDir)
	if doc.Name != dirName {
		return fmt.Errorf("requirement %s: skill frontmatter name %q must equal its directory name %q", req.ID, doc.Name, dirName)
	}
	return nil
}
