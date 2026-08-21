package skillfile

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"

	"github.com/larstonder/whippletree/internal/contract"
)

// Placeholder is the literal install replaces (replace-all) with the
// bundle's absolute root path in a generated SKILL.md. Zero occurrences
// is legal: an unexpanded variant has no baked command at all.
const Placeholder = "__WHIPPLETREE_BUNDLE_ROOT__"

// Expansion is one hook requirement expanding into a skill on one target.
type Expansion struct {
	Event   string // "turn-end" or "session-start"
	ReqID   string
	Kind    string // "blocking-gate" or "lifecycle-signal"
	Handler string // "./handlers/capture.sh"
	Target  string
}

// triggerClauses maps a fallback-eligible event to the sentence spliced
// onto the skill's description: the standing trigger the always-loaded
// skill listing carries.
var triggerClauses = map[string]string{
	"turn-end":      "Use this skill before writing any message that declares the task complete.",
	"session-start": "Use this skill at the start of a session, before other work.",
}

// Version returns the module's build-info version, or "dev" when built
// from a working tree.
func Version() string {
	if bi, ok := debug.ReadBuildInfo(); ok && bi.Main.Version != "" && bi.Main.Version != "(devel)" {
		return bi.Main.Version
	}
	return "dev"
}

// ExpandDir copies the skill directory srcDir to dstDir, transforming
// only SKILL.md: every variant gains the compiled-by ownership marker
// in its frontmatter; when exps is non-empty it also gains one
// description clause and one body section per expansion, in order.
// Every other file is copied byte-for-byte. The authored srcDir is
// never written to.
func ExpandDir(srcDir, dstDir string, exps []Expansion, version string) error {
	doc, err := ParseFile(filepath.Join(srcDir, "SKILL.md"))
	if err != nil {
		return err
	}

	if err := copyTree(srcDir, dstDir); err != nil {
		return err
	}

	out := transform(doc, exps, version)
	return os.WriteFile(filepath.Join(dstDir, "SKILL.md"), []byte(out), 0o644)
}

// transform renders the variant SKILL.md from a parsed authored doc.
func transform(doc *Doc, exps []Expansion, version string) string {
	lines := make([]string, len(doc.lines))
	copy(lines, doc.lines)

	if len(exps) > 0 {
		var clauses []string
		for _, e := range exps {
			clauses = append(clauses, triggerClauses[e.Event])
		}
		for i := 1; i < doc.fmEnd; i++ {
			key, value, found := strings.Cut(lines[i], ":")
			if found && strings.TrimSpace(key) == "description" {
				lines[i] = "description: " + spliceClause(strings.TrimSpace(value), strings.Join(clauses, " "))
				break
			}
		}
	}

	marker := "compiled-by: whippletree " + version
	lines = append(lines[:doc.fmEnd], append([]string{marker}, lines[doc.fmEnd:]...)...)

	var b strings.Builder
	b.WriteString(strings.Join(lines, "\n"))
	for _, e := range exps {
		b.WriteString("\n")
		b.WriteString(bodySection(e, version))
	}
	return b.String()
}

// spliceClause appends clause to a single-line description scalar,
// quote-aware: for a quoted scalar the clause lands inside the closing
// quote, for a plain scalar it is appended with a space.
func spliceClause(value, clause string) string {
	if len(value) >= 2 {
		last := value[len(value)-1]
		if (value[0] == '"' && last == '"') || (value[0] == '\'' && last == '\'') {
			return value[:len(value)-1] + " " + clause + string(last)
		}
	}
	return value + " " + clause
}

// bodySection renders the appended instruction section for one
// expansion: unhedged imperative instruction, with the fidelity claim
// in a structurally separate provenance comment per the honesty
// contract (fidelity wording is contract.T3Fidelity verbatim).
func bodySection(e Expansion, version string) string {
	handlerPath := Placeholder + "/" + strings.TrimPrefix(e.Handler, "./")
	comment := fmt.Sprintf(`<!-- compiled-tier: T3
     source-requirement: %s (%s, %s)
     fidelity: %s
     compiled-by: whippletree %s (https://whippletree.dev), do not hand-edit (edit the bundle contract instead) -->
`, e.ReqID, e.Kind, e.Event, contract.T3Fidelity, version)

	if e.Event == "turn-end" {
		return comment + fmt.Sprintf(`## Manual step on this harness (turn-end)

This harness has no enforced turn-end hook. Before writing any message that
declares the task complete, run:

    echo '{}' | ADAPTER_EVENT=turn-end ADAPTER_PRIMITIVE=turn-end \
      ADAPTER_TARGET=%s ADAPTER_CWD="$PWD" ADAPTER_STOP_ACTIVE=false ADAPTER_PATH= \
      %s

If it exits 2, read its stderr and do what it says. Then run the same command
once more with ADAPTER_STOP_ACTIVE=true and continue; a second exit 2 means the
step still failed and you should tell the user rather than silently finish.
`, e.Target, handlerPath)
	}
	return comment + fmt.Sprintf(`## Manual step on this harness (session-start)

This harness has no session-start hook seam. At the start of a session, before
other work, run:

    echo '{}' | ADAPTER_EVENT=session-start ADAPTER_PRIMITIVE=session-start \
      ADAPTER_TARGET=%s ADAPTER_CWD="$PWD" ADAPTER_STOP_ACTIVE= ADAPTER_PATH= \
      %s

If it fails, tell the user what went wrong rather than silently continuing.
`, e.Target, handlerPath)
}

func copyTree(src, dst string) error {
	return filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		out := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(out, 0o755)
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(out, data, info.Mode().Perm())
	})
}
