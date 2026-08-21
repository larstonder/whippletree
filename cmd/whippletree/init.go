package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/larstonder/whippletree/internal/contract"
)

// validNamePattern is the closed character set a bundle name must
// match: lowercase letters, digits, and hyphens.
var validNamePattern = regexp.MustCompile(`^[a-z0-9-]+$`)

// kindOrder is the canonical, deterministic order requirements are
// emitted in regardless of the order --kinds names them. skill comes
// first so its requirement precedes the gate that references it in the
// generated plugin.json; the JSON order is cosmetic but reads causally.
var kindOrder = []string{"skill", "lifecycle-signal", "observation-signal", "blocking-gate", "executable-path"}

// validKinds is the closed set of requirement kinds init knows how to
// scaffold.
var validKinds = map[string]bool{
	"skill":              true,
	"blocking-gate":      true,
	"lifecycle-signal":   true,
	"observation-signal": true,
	"executable-path":    true,
}

// initArgs is the parsed form of `whippletree init`'s arguments.
type initArgs struct {
	dir string

	name       string
	nameGiven  bool
	kinds      []string
	kindsGiven bool
	hard       []string
	hardGiven  bool
	yes        bool
}

// hasScaffoldFlag reports whether any flag that selects the
// non-interactive scaffolding path was given.
func (a initArgs) hasScaffoldFlag() bool {
	return a.yes || a.nameGiven || a.kindsGiven || a.hardGiven
}

// parseInitArgs accepts only the space-separated "--flag value" form,
// not "--flag=value".
func parseInitArgs(args []string) (initArgs, error) {
	var parsed initArgs

	var positional []string
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch arg {
		case "--name":
			if i+1 >= len(args) {
				return initArgs{}, fmt.Errorf("--name requires a value")
			}
			parsed.name = args[i+1]
			parsed.nameGiven = true
			i++
		case "--kinds":
			if i+1 >= len(args) {
				return initArgs{}, fmt.Errorf("--kinds requires a value")
			}
			parsed.kinds = splitCSV(args[i+1])
			parsed.kindsGiven = true
			i++
		case "--hard":
			if i+1 >= len(args) {
				return initArgs{}, fmt.Errorf("--hard requires a value")
			}
			parsed.hard = splitCSV(args[i+1])
			parsed.hardGiven = true
			i++
		case "--yes":
			parsed.yes = true
		default:
			if strings.HasPrefix(arg, "-") {
				return initArgs{}, fmt.Errorf("unknown flag %q", arg)
			}
			positional = append(positional, arg)
		}
	}

	if len(positional) > 1 {
		return initArgs{}, fmt.Errorf("unexpected extra arguments: %s", strings.Join(positional[1:], " "))
	}

	parsed.dir = "."
	if len(positional) == 1 {
		parsed.dir = positional[0]
	}

	return parsed, nil
}

// splitCSV splits a comma-separated flag value, trimming whitespace
// around each entry and dropping empty entries (so a trailing comma
// isn't treated as naming a blank kind).
func splitCSV(s string) []string {
	var out []string
	for _, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

// normalizeKinds validates raw against the closed kind set and returns
// the chosen kinds in kindOrder's deterministic order.
func normalizeKinds(raw []string) ([]string, error) {
	chosen := make(map[string]bool, len(raw))
	for _, k := range raw {
		if !validKinds[k] {
			return nil, fmt.Errorf("unknown kind %q; must be one of skill, blocking-gate, lifecycle-signal, observation-signal, executable-path", k)
		}
		chosen[k] = true
	}
	if len(chosen) == 0 {
		return nil, fmt.Errorf("--kinds requires at least one kind")
	}

	out := make([]string, 0, len(chosen))
	for _, k := range kindOrder {
		if chosen[k] {
			out = append(out, k)
		}
	}
	return out, nil
}

// normalizeHard validates raw against the closed kind set and requires
// it to be a subset of kinds, returning it as a set.
func normalizeHard(raw []string, kinds []string) (map[string]bool, error) {
	chosenKinds := make(map[string]bool, len(kinds))
	for _, k := range kinds {
		chosenKinds[k] = true
	}

	hardSet := make(map[string]bool, len(raw))
	for _, k := range raw {
		if !validKinds[k] {
			return nil, fmt.Errorf("unknown kind %q in --hard; must be one of skill, blocking-gate, lifecycle-signal, observation-signal, executable-path", k)
		}
		if !chosenKinds[k] {
			return nil, fmt.Errorf("--hard names kind %q, which is not among the chosen --kinds", k)
		}
		hardSet[k] = true
	}
	return hardSet, nil
}

// runInit implements `whippletree init`: the non-interactive path
// driven by flags, and the interactive wizard that runs when init is
// invoked bare on a TTY. Both paths converge on the same scaffold
// generator, so a wizard session and the equivalent flags produce
// identical output.
func runInit(args []string, stdin io.Reader, isTTY func() bool, stdout, stderr io.Writer) int {
	const usage = "usage: whippletree init [<dir>] [--name <s>] [--kinds <csv>] [--hard <csv>] [--yes]"

	parsed, err := parseInitArgs(args)
	if err != nil {
		fmt.Fprintf(stderr, "whippletree: %v\n", err)
		fmt.Fprintln(stderr, usage)
		return 1
	}

	absDir, err := filepath.Abs(parsed.dir)
	if err != nil {
		fmt.Fprintf(stderr, "whippletree: %v\n", err)
		return 1
	}

	var name string
	var kinds []string
	var hardSet map[string]bool

	if !parsed.hasScaffoldFlag() && isTTY() {
		name, kinds, hardSet, err = runInitWizard(stdin, stdout, filepath.Base(absDir))
		if err != nil {
			fmt.Fprintf(stderr, "whippletree: %v\n", err)
			return 1
		}
	} else {
		name = parsed.name
		if name == "" {
			name = filepath.Base(absDir)
		}

		kinds = []string{"lifecycle-signal"}
		if parsed.kindsGiven {
			kinds, err = normalizeKinds(parsed.kinds)
			if err != nil {
				fmt.Fprintf(stderr, "whippletree: %v\n", err)
				return 1
			}
		}

		hardSet, err = normalizeHard(parsed.hard, kinds)
		if err != nil {
			fmt.Fprintf(stderr, "whippletree: %v\n", err)
			return 1
		}
	}

	if !validNamePattern.MatchString(name) {
		fmt.Fprintf(stderr, "whippletree: invalid name %q: must match ^[a-z0-9-]+$\n", name)
		return 1
	}

	files, err := scaffoldFiles(name, kinds, hardSet)
	if err != nil {
		fmt.Fprintf(stderr, "whippletree: %v\n", err)
		return 1
	}

	if err := checkNoExisting(absDir, files); err != nil {
		fmt.Fprintf(stderr, "whippletree: %v\n", err)
		return 1
	}

	if err := writeScaffold(absDir, files); err != nil {
		fmt.Fprintf(stderr, "whippletree: %v\n", err)
		return 1
	}

	fmt.Fprintf(stdout, "whippletree: scaffolded %s in %s\n", name, absDir)
	return 0
}

// wizardKindMenu is the order the wizard numbers kinds in. It does not
// match the order --kinds documents them in its own error messages
// (that order is kindOrder, with skill first); the two orders are
// independent and free to diverge.
var wizardKindMenu = []string{"blocking-gate", "lifecycle-signal", "observation-signal", "executable-path", "skill"}

// runInitWizard collects a name, kinds, and hard-required choices, then
// hands them to the same generator the flag path uses. An empty line
// takes the default; the stream ending early (Ctrl-D) cancels with an
// error rather than silently accepting defaults. Bad answers re-prompt
// once. Writes nothing to disk.
func runInitWizard(r io.Reader, stdout io.Writer, defaultName string) (name string, kinds []string, hardSet map[string]bool, err error) {
	scanner := bufio.NewScanner(r)
	readLine := func() (string, bool) {
		if !scanner.Scan() {
			return "", false
		}
		return strings.TrimSpace(scanner.Text()), true
	}

	fmt.Fprintf(stdout, "Bundle name [%s]: ", defaultName)
	nameLine, ok := readLine()
	if !ok {
		return "", nil, nil, fmt.Errorf("input ended before the bundle name was answered; wizard cancelled")
	}
	name = nameLine
	if name == "" {
		name = defaultName
	}

	fmt.Fprintln(stdout, "Kinds:")
	for i, k := range wizardKindMenu {
		marker := ""
		if k == "lifecycle-signal" {
			marker = " (default)"
		}
		fmt.Fprintf(stdout, "  %d. %s%s\n", i+1, k, marker)
	}
	fmt.Fprint(stdout, "Kinds to include (comma-separated numbers) [2]: ")

	kindsLine, ok := readLine()
	if !ok {
		return "", nil, nil, fmt.Errorf("input ended before the kinds question was answered; wizard cancelled")
	}
	kinds, kindsErr := parseWizardKinds(kindsLine)
	if kindsErr != nil {
		fmt.Fprintf(stdout, "%v; enter numbers between 1 and %d, comma-separated\n", kindsErr, len(wizardKindMenu))
		fmt.Fprint(stdout, "Kinds to include (comma-separated numbers) [2]: ")
		kindsLine, ok = readLine()
		if !ok {
			return "", nil, nil, fmt.Errorf("input ended before the kinds question was answered; wizard cancelled")
		}
		kinds, kindsErr = parseWizardKinds(kindsLine)
		if kindsErr != nil {
			return "", nil, nil, kindsErr
		}
	}

	hardSet = make(map[string]bool)
	for _, k := range kinds {
		if k != "blocking-gate" && k != "executable-path" {
			continue
		}
		prompt := fmt.Sprintf("Make %s hard-required? A hard requirement refuses install when the harness cannot meet it. [y/N]: ", k)
		fmt.Fprint(stdout, prompt)

		hardLine, ok := readLine()
		if !ok {
			return "", nil, nil, fmt.Errorf("input ended before %s hard-required was answered; wizard cancelled", k)
		}
		hard, hardErr := parseWizardYesNo(hardLine)
		if hardErr != nil {
			fmt.Fprintf(stdout, "%v; enter y or n\n", hardErr)
			fmt.Fprint(stdout, prompt)
			hardLine, ok = readLine()
			if !ok {
				return "", nil, nil, fmt.Errorf("input ended before %s hard-required was answered; wizard cancelled", k)
			}
			hard, hardErr = parseWizardYesNo(hardLine)
			if hardErr != nil {
				return "", nil, nil, hardErr
			}
		}
		hardSet[k] = hard
	}

	return name, kinds, hardSet, nil
}

// parseWizardKinds parses raw as comma-separated 1-based indices into
// wizardKindMenu, defaulting to lifecycle-signal alone when raw is
// empty, and returns the chosen kinds in scaffoldFiles' canonical
// order.
func parseWizardKinds(raw string) ([]string, error) {
	if raw == "" {
		return []string{"lifecycle-signal"}, nil
	}

	var chosen []string
	for _, part := range strings.Split(raw, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		n, convErr := strconv.Atoi(part)
		if convErr != nil || n < 1 || n > len(wizardKindMenu) {
			return nil, fmt.Errorf("invalid kind number %q", part)
		}
		chosen = append(chosen, wizardKindMenu[n-1])
	}
	if len(chosen) == 0 {
		return nil, fmt.Errorf("no kinds selected")
	}

	return normalizeKinds(chosen)
}

// parseWizardYesNo parses a y/n answer, treating an empty answer as
// the printed default of "no".
func parseWizardYesNo(raw string) (bool, error) {
	switch strings.ToLower(raw) {
	case "", "n", "no":
		return false, nil
	case "y", "yes":
		return true, nil
	default:
		return false, fmt.Errorf("invalid answer %q", raw)
	}
}

// scaffoldFile is one file init will write, relative to the bundle
// directory.
type scaffoldFile struct {
	relPath string
	body    []byte
	perm    os.FileMode
}

// scaffoldManifest is the shape of a scaffolded plugin.json.
type scaffoldManifest struct {
	Name        string                      `json:"name"`
	Version     string                      `json:"version"`
	Description string                      `json:"description"`
	Extensions  map[string]scaffoldContract `json:"extensions"`
}

// scaffoldContract is the dev.whippletree.v1 extension block of a
// scaffolded plugin.json.
type scaffoldContract struct {
	ContractVersion string                 `json:"contractVersion"`
	Requires        []contract.Requirement `json:"requires"`
}

// scaffoldRequirement builds the exact-values requirement the scaffold
// defaults table specifies for kind. hard is only consulted for
// blocking-gate and executable-path: lifecycle-signal and
// observation-signal are always soft, matching the table.
func scaffoldRequirement(kind, name string, hard bool) (contract.Requirement, error) {
	no, yes := false, true
	switch kind {
	case "skill":
		return contract.Requirement{
			ID: "skill", Kind: kind, Path: "./skills/" + name,
			MinTierRaw: "T1", HardRequired: &no,
		}, nil
	case "lifecycle-signal":
		return contract.Requirement{
			ID: "lifecycle-signal", Kind: kind, Event: "session-start",
			MinTierRaw: "T2", HardRequired: &no,
			Handler: "./handlers/lifecycle-signal.sh",
		}, nil
	case "observation-signal":
		return contract.Requirement{
			ID: "observation-signal", Kind: kind, Event: "file-read",
			MinTierRaw: "T4", HardRequired: &no,
			Handler: "./handlers/observation-signal.sh",
		}, nil
	case "blocking-gate":
		hr := &no
		if hard {
			hr = &yes
		}
		return contract.Requirement{
			ID: "blocking-gate", Kind: kind, Event: "turn-end",
			MinTierRaw: "T1", HardRequired: hr, LoopGuardRequired: true,
			Handler: "./handlers/blocking-gate.sh",
		}, nil
	case "executable-path":
		hr := &no
		if hard {
			hr = &yes
		}
		return contract.Requirement{
			ID: "executable-path", Kind: kind,
			MinTierRaw: "T1", HardRequired: hr,
			Path: "./bin/" + name,
		}, nil
	default:
		// Callers validate against validKinds before reaching here, so
		// this is unreachable today. It returns rather than panics so a
		// future caller that forgets gets an error like every other
		// failure in this package, not a stack trace.
		return contract.Requirement{}, fmt.Errorf("unknown requirement kind %q", kind)
	}
}

const gitignoreBody = `/hooks/
/.claude-plugin/plugin.json
/.codex-plugin/
/.whippletree/
/bin/whippletree-hook
`

const lifecycleSignalScript = `#!/usr/bin/env bash
set -euo pipefail
# Runs on session-start. Payload JSON is on stdin; common fields are
# already in the environment: ADAPTER_EVENT, ADAPTER_PRIMITIVE,
# ADAPTER_TARGET, ADAPTER_CWD.
exit 0
`

const observationSignalScript = `#!/usr/bin/env bash
set -euo pipefail
# Runs on file-read. Payload JSON is on stdin; common fields are
# already in the environment: ADAPTER_EVENT, ADAPTER_PRIMITIVE,
# ADAPTER_TARGET, ADAPTER_CWD, ADAPTER_PATH (the observed file).
exit 0
`

const blockingGateScript = `#!/usr/bin/env bash
set -euo pipefail
# Runs on turn-end. Exit 0 to allow the turn to finish; exit 2 with a
# reason on stderr to block it. ADAPTER_STOP_ACTIVE is "true" when
# this hook already blocked once this turn: always allow then, or the
# harness loops forever.
if [ "${ADAPTER_STOP_ACTIVE:-}" = "true" ]; then
  exit 0
fi
# Replace this with your real gate condition.
exit 0
`

func binScript(name string) string {
	return fmt.Sprintf("#!/bin/sh\necho \"replace bin/%s with your real executable\" >&2\nexit 1\n", name)
}

// skillStub is the authored SKILL.md scaffolded for the skill kind: its
// frontmatter name matches the bundle name per the dir==frontmatter-name
// rule (the skill's own directory is named after the bundle, same as
// its --kinds/plugin.json id, never independently chosen).
func skillStub(name string) string {
	return fmt.Sprintf(`---
name: %s
description: Replace this with one sentence saying when the agent should use this skill.
---
Replace this body with the knowledge or workflow the skill teaches.
`, name)
}

// scaffoldRequirements builds the full requirement list for kinds, in
// kinds' own order, then applies the one piece of cross-requirement
// wiring a scaffold ever does: when a skill kind is present alongside a
// blocking-gate, the gate gets fallbackSkill wired to it and its
// minTier bumped to T3 (an instruction fallback is only worth minTier
// T3, never T1, since it's best-effort with no harness enforcement).
// buildPluginJSON and buildReadme both call this so the manifest and
// the README's defaults table can never drift apart.
func scaffoldRequirements(name string, kinds []string, hardSet map[string]bool) ([]contract.Requirement, error) {
	reqs := make([]contract.Requirement, 0, len(kinds))
	for _, k := range kinds {
		req, err := scaffoldRequirement(k, name, hardSet[k])
		if err != nil {
			return nil, err
		}
		reqs = append(reqs, req)
	}

	hasSkill := false
	for _, r := range reqs {
		if r.Kind == "skill" {
			hasSkill = true
		}
	}
	if hasSkill {
		for i := range reqs {
			if reqs[i].Kind == "blocking-gate" {
				reqs[i].FallbackSkill = "skill"
				reqs[i].MinTierRaw = "T3"
			}
		}
	}
	return reqs, nil
}

// buildPluginJSON renders the scaffolded plugin.json for name, one
// requirement per kind in kinds, with hardRequired driven by hardSet
// for the kinds that honor it.
func buildPluginJSON(name string, kinds []string, hardSet map[string]bool) ([]byte, error) {
	reqs, err := scaffoldRequirements(name, kinds, hardSet)
	if err != nil {
		return nil, err
	}

	manifest := scaffoldManifest{
		Name:        name,
		Version:     "0.1.0",
		Description: name + " whippletree bundle",
		Extensions: map[string]scaffoldContract{
			contract.Namespace: {ContractVersion: "1.0.0", Requires: reqs},
		},
	}

	body, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal plugin.json: %w", err)
	}
	return append(body, '\n'), nil
}

// marketplacePlugin, marketplaceOwner, and marketplaceDoc mirror the
// exact field structure of examples/kb-shaped/.claude-plugin/marketplace.json.
type marketplacePlugin struct {
	Name        string `json:"name"`
	Source      string `json:"source"`
	Description string `json:"description"`
}

type marketplaceOwner struct {
	Name string `json:"name"`
}

type marketplaceDoc struct {
	Name    string              `json:"name"`
	Owner   marketplaceOwner    `json:"owner"`
	Plugins []marketplacePlugin `json:"plugins"`
}

// buildMarketplaceJSON renders the scaffolded .claude-plugin/marketplace.json
// for name, mirroring examples/kb-shaped's field structure with name and
// <name>-mkt substituted.
func buildMarketplaceJSON(name string) ([]byte, error) {
	doc := marketplaceDoc{
		Name:  name + "-mkt",
		Owner: marketplaceOwner{Name: "whippletree"},
		Plugins: []marketplacePlugin{
			{Name: name, Source: "./", Description: name + " whippletree bundle"},
		},
	}

	body, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal marketplace.json: %w", err)
	}
	return append(body, '\n'), nil
}

// generatedFileList returns the relative paths scaffoldFiles will
// create for kinds, in the same order scaffoldFiles writes them; used
// by buildReadme to list what init generated.
func generatedFileList(kinds []string) []string {
	hasKind := make(map[string]bool, len(kinds))
	for _, k := range kinds {
		hasKind[k] = true
	}

	files := []string{
		"plugin.json",
		filepath.Join(".claude-plugin", "marketplace.json"),
	}
	if hasKind["skill"] {
		files = append(files, filepath.Join("skills", "<name>", "SKILL.md"))
	}
	if hasKind["lifecycle-signal"] {
		files = append(files, filepath.Join("handlers", "lifecycle-signal.sh"))
	}
	if hasKind["observation-signal"] {
		files = append(files, filepath.Join("handlers", "observation-signal.sh"))
	}
	if hasKind["blocking-gate"] {
		files = append(files, filepath.Join("handlers", "blocking-gate.sh"))
	}
	if hasKind["executable-path"] {
		files = append(files, filepath.Join("bin", "<name>"))
	}
	files = append(files, ".gitignore", "README.md")
	return files
}

// buildReadme renders the scaffolded README.md: the generated-file
// list, the defaults table for the chosen kinds, and next steps.
func buildReadme(name string, kinds []string, hardSet map[string]bool) ([]byte, error) {
	var b strings.Builder

	fmt.Fprintf(&b, "# %s\n\n", name)
	b.WriteString("Generated by `whippletree init`:\n\n")
	for _, f := range generatedFileList(kinds) {
		fmt.Fprintf(&b, "- %s\n", strings.ReplaceAll(f, "<name>", name))
	}

	hasKind := make(map[string]bool, len(kinds))
	for _, k := range kinds {
		hasKind[k] = true
	}

	b.WriteString("\nDefaults (edit plugin.json to change these):\n\n")
	b.WriteString("| kind | id | event | minTier | hardRequired |\n")
	b.WriteString("|---|---|---|---|---|\n")
	reqs, err := scaffoldRequirements(name, kinds, hardSet)
	if err != nil {
		return nil, err
	}
	for _, req := range reqs {
		event := req.Event
		if event == "" {
			event = "(none)"
		}
		fmt.Fprintf(&b, "| %s | %s | %s | %s | %t |\n", req.Kind, req.ID, event, req.MinTierRaw, *req.HardRequired)
	}

	b.WriteString("\nNext steps:\n\n")
	b.WriteString("```\n")
	b.WriteString("whippletree build .\n")
	b.WriteString("whippletree preflight . --target claude-code\n")
	b.WriteString("```\n\n")
	b.WriteString("build auto-copies whippletree-hook when it sits next to the whippletree binary; otherwise it prints the go build command to run yourself.\n")
	if hardSet["blocking-gate"] {
		if hasKind["skill"] {
			b.WriteString("\nblocking-gate is hard-required with minTier T3 and an instruction fallback: on\ntargets with no native stop gate (opencode) the skill carries the step as\ninstructions instead of refusing to install.\n")
		} else {
			b.WriteString("\nblocking-gate is hard-required here, so build will refuse on any target with no\nnative stop gate (opencode); pass --allow-refuse to build anyway.\n")
		}
	}

	return []byte(b.String()), nil
}

// scaffoldFiles assembles the full, ordered set of files init will
// write for name, kinds, and hardSet.
func scaffoldFiles(name string, kinds []string, hardSet map[string]bool) ([]scaffoldFile, error) {
	pluginBody, err := buildPluginJSON(name, kinds, hardSet)
	if err != nil {
		return nil, err
	}
	mktBody, err := buildMarketplaceJSON(name)
	if err != nil {
		return nil, err
	}

	hasKind := make(map[string]bool, len(kinds))
	for _, k := range kinds {
		hasKind[k] = true
	}

	files := []scaffoldFile{
		{"plugin.json", pluginBody, 0o644},
		{filepath.Join(".claude-plugin", "marketplace.json"), mktBody, 0o644},
	}

	if hasKind["skill"] {
		files = append(files, scaffoldFile{filepath.Join("skills", name, "SKILL.md"), []byte(skillStub(name)), 0o644})
	}
	if hasKind["lifecycle-signal"] {
		files = append(files, scaffoldFile{filepath.Join("handlers", "lifecycle-signal.sh"), []byte(lifecycleSignalScript), 0o755})
	}
	if hasKind["observation-signal"] {
		files = append(files, scaffoldFile{filepath.Join("handlers", "observation-signal.sh"), []byte(observationSignalScript), 0o755})
	}
	if hasKind["blocking-gate"] {
		files = append(files, scaffoldFile{filepath.Join("handlers", "blocking-gate.sh"), []byte(blockingGateScript), 0o755})
	}
	if hasKind["executable-path"] {
		files = append(files, scaffoldFile{filepath.Join("bin", name), []byte(binScript(name)), 0o755})
	}

	readme, err := buildReadme(name, kinds, hardSet)
	if err != nil {
		return nil, err
	}
	files = append(files,
		scaffoldFile{".gitignore", []byte(gitignoreBody), 0o644},
		scaffoldFile{"README.md", readme, 0o644},
	)

	return files, nil
}

// checkNoExisting errors, naming the first offender, if any file init
// would create under dir already exists. It writes nothing: callers
// must run this before writeScaffold so a clash is caught before any
// file is placed.
func checkNoExisting(dir string, files []scaffoldFile) error {
	for _, f := range files {
		full := filepath.Join(dir, f.relPath)
		if _, err := os.Stat(full); err == nil {
			return fmt.Errorf("%s already exists; refusing to overwrite it", full)
		} else if !os.IsNotExist(err) {
			return fmt.Errorf("check existing %s: %w", full, err)
		}
	}
	return nil
}

// writeScaffold creates dir (and any intermediate directories) and
// writes every file in files under it.
func writeScaffold(dir string, files []scaffoldFile) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create %s: %w", dir, err)
	}
	for _, f := range files {
		full := filepath.Join(dir, f.relPath)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			return fmt.Errorf("create directory for %s: %w", full, err)
		}
		if err := os.WriteFile(full, f.body, f.perm); err != nil {
			return fmt.Errorf("write %s: %w", full, err)
		}
	}
	return nil
}
