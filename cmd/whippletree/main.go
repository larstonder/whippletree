// Command whippletree is the developer-facing CLI: it compiles a
// bundle's per-target variants (build) and reports whether a target
// can satisfy a bundle's contract before install (preflight).
package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/larstonder/whippletree/internal/compile"
	"github.com/larstonder/whippletree/internal/contract"
	"github.com/larstonder/whippletree/internal/preflight"
	"github.com/larstonder/whippletree/internal/target"
	"github.com/larstonder/whippletree/targets"
)

// loadTargets resolves the target definitions every verb (build,
// preflight, install) compiles or checks against. An explicit
// --targets-dir always wins, loaded straight off disk exactly as
// before; with no flag, it falls back to the target definitions
// embedded into this binary at build time (targets.FS), which is what
// lets the CLI work from any working directory, not just the
// whippletree repo root.
func loadTargets(targetsDirArg string) (map[string]*target.Def, error) {
	if targetsDirArg != "" {
		return target.LoadDir(targetsDirArg)
	}
	return target.LoadFS(targets.FS)
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

// run is main's testable body.
func run(args []string, stdout, stderr io.Writer) int {
	return runWith(args, os.Stdin, stdinIsTTY, stdout, stderr)
}

// runWith is the dispatcher containing the real logic; it accepts injected
// stdin and TTY-ness for testing and future interactive verbs. Existing
// verbs read the global stdin/TTY state (preflight calls stdinIsTTY
// directly), so this plumbing creates no behavior change.
func runWith(args []string, stdin io.Reader, isTTY func() bool, stdout, stderr io.Writer) int {
	if len(args) < 1 {
		fmt.Fprintln(stderr, "usage: whippletree <build|preflight|install> ...")
		return 1
	}

	switch args[0] {
	case "build":
		return runBuild(args[1:], stdout, stderr)
	case "preflight":
		return runPreflight(args[1:], stdout, stderr)
	case "install":
		return runInstall(args[1:], stdout, stderr)
	default:
		fmt.Fprintf(stderr, "whippletree: unknown subcommand %q\n", args[0])
		return 1
	}
}

// buildArgs is the parsed form of `whippletree build`'s arguments.
type buildArgs struct {
	bundleDir              string
	targetsDir             string
	allowMissingDispatcher bool
	allowRefuse            bool
}

func parseBuildArgs(args []string) (buildArgs, error) {
	var parsed buildArgs

	var positional []string
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--allow-missing-dispatcher":
			parsed.allowMissingDispatcher = true
		case "--allow-refuse":
			parsed.allowRefuse = true
		case "--targets-dir":
			if i+1 >= len(args) {
				return buildArgs{}, fmt.Errorf("--targets-dir requires a value")
			}
			parsed.targetsDir = args[i+1]
			i++
		default:
			positional = append(positional, args[i])
		}
	}

	if len(positional) < 1 {
		return buildArgs{}, fmt.Errorf("missing <bundleDir>")
	}
	parsed.bundleDir = positional[0]

	return parsed, nil
}

func runBuild(args []string, stdout, stderr io.Writer) int {
	const usage = "usage: whippletree build <bundleDir> [--targets-dir dir] [--allow-missing-dispatcher] [--allow-refuse]"

	parsed, err := parseBuildArgs(args)
	if err != nil {
		fmt.Fprintf(stderr, "whippletree: %v\n", err)
		fmt.Fprintln(stderr, usage)
		return 1
	}

	targetDefs, err := loadTargets(parsed.targetsDir)
	if err != nil {
		fmt.Fprintf(stderr, "whippletree: %v\n", err)
		return 1
	}

	built, err := compile.Build(parsed.bundleDir, targetDefs)
	if err != nil {
		fmt.Fprintf(stderr, "whippletree: build: %v\n", err)
		return 1
	}

	if err := ensureDispatcher(parsed.bundleDir, parsed.allowMissingDispatcher, stderr); err != nil {
		fmt.Fprintf(stderr, "whippletree: %v\n", err)
		return 1
	}

	names := make([]string, 0, len(built.PerTarget))
	for name := range built.PerTarget {
		names = append(names, name)
	}
	sort.Strings(names)

	type refusal struct {
		target string
		reqID  string
	}
	var refusals []refusal

	for _, name := range names {
		satisfy, degrade, refuse, absent := 0, 0, 0, 0
		for _, a := range built.PerTarget[name] {
			// build has no probed version to fail closed on, so it
			// classifies straight from compile.Result rather than
			// going through preflight.Check.
			switch preflight.Classify(a) {
			case preflight.Satisfy:
				satisfy++
			case preflight.Degrade:
				degrade++
			case preflight.Refuse:
				refuse++
				refusals = append(refusals, refusal{target: name, reqID: a.Req.ID})
			case preflight.Absent:
				absent++
			}
		}
		fmt.Fprintf(stdout, "target %s: %d satisfy, %d degrade, %d refuse, %d absent\n", name, satisfy, degrade, refuse, absent)
	}

	if len(refusals) > 0 {
		for _, r := range refusals {
			fmt.Fprintf(stderr, "whippletree: target %s refuses requirement %s\n", r.target, r.reqID)
		}
		if !parsed.allowRefuse {
			return 1
		}
		fmt.Fprintln(stderr, "whippletree: continuing past refusals (--allow-refuse)")
	}

	return 0
}

// ensureDispatcher makes sure <bundleDir>/bin/whippletree-hook exists,
// since every hooks-file command this build just wrote invokes it and
// compile.Build never provisions it itself. If it's missing, it tries
// copying the whippletree-hook binary sitting alongside whippletree's
// own executable (the layout a packaged release ships). If that's not
// available either, allowMissing turns the gap into a warning;
// otherwise it's a build error naming the exact command to run.
func ensureDispatcher(bundleDir string, allowMissing bool, stderr io.Writer) error {
	binPath := filepath.Join(bundleDir, "bin", "whippletree-hook")
	if _, err := os.Stat(binPath); err == nil {
		return nil
	}

	if err := copyFromSiblingExecutable(binPath); err == nil {
		return nil
	}

	msg := fmt.Sprintf("%s is missing; build it with `go build -o %s ./cmd/whippletree-hook`", binPath, binPath)
	if allowMissing {
		fmt.Fprintf(stderr, "whippletree: warning: %s\n", msg)
		return nil
	}
	return errors.New(msg)
}

// copyFromSiblingExecutable copies a "whippletree-hook" binary found next
// to whippletree's own executable (dirname(os.Executable())) to dst,
// the layout a packaged whippletree release ships both binaries in.
func copyFromSiblingExecutable(dst string) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	real, err := filepath.EvalSymlinks(exe)
	if err != nil {
		return err
	}

	src := filepath.Join(filepath.Dir(real), "whippletree-hook")
	info, err := os.Stat(src)
	if err != nil {
		return err
	}
	if info.IsDir() {
		return fmt.Errorf("%s is a directory, not the whippletree-hook binary", src)
	}

	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	return os.WriteFile(dst, data, 0o755)
}

// installState is the shape written to
// <bundleDir>/.whippletree/install-state.json on a successful (non-
// refused) preflight.
type installState struct {
	Harness   string            `json:"harness"`
	Version   string            `json:"version"`
	CheckedAt string            `json:"checkedAt"`
	Tiers     map[string]string `json:"tiers"`
}

func runPreflight(args []string, stdout, stderr io.Writer) int {
	bundleDir, targetName, assumeVersion, targetsDirArg, err := parsePreflightArgs(args)
	if err != nil {
		fmt.Fprintf(stderr, "whippletree: %v\n", err)
		fmt.Fprintln(stderr, "usage: whippletree preflight <bundleDir> --target <name> [--assume-version X] [--targets-dir dir]")
		return 1
	}

	_, report, probed, ok := checkAgainstTarget(bundleDir, targetName, assumeVersion, targetsDirArg, stdout, stderr)
	if !ok {
		return 1
	}

	if report.Refused {
		return 1
	}

	if err := writeInstallState(bundleDir, targetName, string(probed), report); err != nil {
		fmt.Fprintf(stderr, "whippletree: write install state: %v\n", err)
		return 1
	}

	return 0
}

// checkAgainstTarget loads targetName from targetsDirArg, parses and
// validates bundleDir's plugin.json contract, resolves a probed (or
// assumed) version, and runs preflight.Check, printing the rendered
// report to stdout exactly as the preflight verb does. Both preflight
// and install share this one code path so a REFUSE, a probe failure,
// or any other error is reported identically by both.
//
// ok is false whenever a hard error occurred (already written to
// stderr with an appropriate message); the caller's exit code is 1 in
// that case. When ok is true, the caller must still check
// report.Refused before treating the run as successful: a REFUSE is
// not an error here, it prints the report and lets the caller decide
// what "nothing happens next" means for its own verb.
func checkAgainstTarget(bundleDir, targetName, assumeVersion, targetsDirArg string, stdout, stderr io.Writer) (td *target.Def, report *preflight.Report, probed preflight.Version, ok bool) {
	targetDefs, err := loadTargets(targetsDirArg)
	if err != nil {
		fmt.Fprintf(stderr, "whippletree: %v\n", err)
		return nil, nil, "", false
	}
	td, found := targetDefs[targetName]
	if !found {
		fmt.Fprintf(stderr, "whippletree: unknown target %q\n", targetName)
		return nil, nil, "", false
	}

	rawManifest, err := os.ReadFile(filepath.Join(bundleDir, "plugin.json"))
	if err != nil {
		fmt.Fprintf(stderr, "whippletree: read plugin manifest: %v\n", err)
		return nil, nil, "", false
	}
	c, err := contract.Parse(rawManifest)
	if err != nil {
		fmt.Fprintf(stderr, "whippletree: %v\n", err)
		return nil, nil, "", false
	}
	if err := contract.Validate(c); err != nil {
		fmt.Fprintf(stderr, "whippletree: validate contract: %v\n", err)
		return nil, nil, "", false
	}

	// A TTY'd stdin is the signal that this invocation could actually
	// prompt a human.
	interactive := stdinIsTTY()

	if assumeVersion != "" {
		probed = preflight.Version(assumeVersion)
	} else {
		v, err := preflight.Probe(td)
		if err != nil {
			fmt.Fprintf(stderr, "preflight: probe failed: %v\n", err)
			return nil, nil, "", false
		}
		probed = v
	}

	report, err = preflight.Check(c, td, probed, interactive)
	if err != nil {
		fmt.Fprintf(stderr, "preflight: %v\n", err)
		return nil, nil, "", false
	}

	fmt.Fprint(stdout, report.Render(targetName, probed))

	return td, report, probed, true
}

// writeInstallState writes <bundleDir>/.whippletree/install-state.json
// recording the harness, its probed version, when this preflight ran,
// and the achieved tier per requirement.
func writeInstallState(bundleDir, targetName, version string, report *preflight.Report) error {
	tiers := make(map[string]string, len(report.Lines))
	for _, l := range report.Lines {
		if l.Verdict == preflight.Absent {
			continue
		}
		tiers[l.ReqID] = l.Got.String()
	}

	state := installState{
		Harness:   targetName,
		Version:   version,
		CheckedAt: time.Now().UTC().Format(time.RFC3339),
		Tiers:     tiers,
	}

	body, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal install state: %w", err)
	}
	body = append(body, '\n')

	dir := filepath.Join(bundleDir, ".whippletree")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create .whippletree dir: %w", err)
	}
	return os.WriteFile(filepath.Join(dir, "install-state.json"), body, 0o644)
}

func parsePreflightArgs(args []string) (bundleDir, targetName, assumeVersion, targetsDirArg string, err error) {
	var positional []string
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--target":
			if i+1 >= len(args) {
				return "", "", "", "", fmt.Errorf("--target requires a value")
			}
			targetName = args[i+1]
			i++
		case "--assume-version":
			if i+1 >= len(args) {
				return "", "", "", "", fmt.Errorf("--assume-version requires a value")
			}
			assumeVersion = args[i+1]
			i++
		case "--targets-dir":
			if i+1 >= len(args) {
				return "", "", "", "", fmt.Errorf("--targets-dir requires a value")
			}
			targetsDirArg = args[i+1]
			i++
		default:
			positional = append(positional, args[i])
		}
	}

	if len(positional) < 1 {
		return "", "", "", "", fmt.Errorf("missing <bundleDir>")
	}
	bundleDir = positional[0]

	if targetName == "" {
		return "", "", "", "", fmt.Errorf("missing --target")
	}

	return bundleDir, targetName, assumeVersion, targetsDirArg, nil
}

// generatedByMarker is the exact first line of a ts-plugin shim
// compile.Build emits (see internal/compile/tsplugin.go's
// renderTSPlugin and its golden testdata). install's overwrite
// protection treats a destination file as "safe to replace" only when
// its first line matches this marker, i.e. it was itself placed by a
// previous whippletree install rather than hand-authored.
const generatedByMarker = "// Generated by whippletree. Do not hand-edit; regenerate with `whippletree build`."

// tsPluginHookPlaceholder is the literal placeholder the compiler
// leaves in a ts-plugin shim's HOOK constant; install replaces it with
// the bundle's absolute dispatcher path.
const tsPluginHookPlaceholder = "__WHIPPLETREE_HOOK__"

// installArgs is the parsed form of `whippletree install`'s arguments.
type installArgs struct {
	bundleDir     string
	targetName    string
	project       string
	assumeVersion string
	targetsDir    string
}

func parseInstallArgs(args []string) (installArgs, error) {
	var parsed installArgs

	var positional []string
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--target":
			if i+1 >= len(args) {
				return installArgs{}, fmt.Errorf("--target requires a value")
			}
			parsed.targetName = args[i+1]
			i++
		case "--project":
			if i+1 >= len(args) {
				return installArgs{}, fmt.Errorf("--project requires a value")
			}
			parsed.project = args[i+1]
			i++
		case "--assume-version":
			if i+1 >= len(args) {
				return installArgs{}, fmt.Errorf("--assume-version requires a value")
			}
			parsed.assumeVersion = args[i+1]
			i++
		case "--targets-dir":
			if i+1 >= len(args) {
				return installArgs{}, fmt.Errorf("--targets-dir requires a value")
			}
			parsed.targetsDir = args[i+1]
			i++
		default:
			positional = append(positional, args[i])
		}
	}

	if len(positional) < 1 {
		return installArgs{}, fmt.Errorf("missing <bundleDir>")
	}
	parsed.bundleDir = positional[0]

	if parsed.targetName == "" {
		return installArgs{}, fmt.Errorf("missing --target")
	}

	if parsed.project == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return installArgs{}, fmt.Errorf("determine current directory: %w", err)
		}
		parsed.project = cwd
	}

	return parsed, nil
}

// runInstall runs the same preflight check the preflight verb does
// (see checkAgainstTarget), then, on anything short of a REFUSE,
// performs the backend-specific install action: ts-plugin targets get
// the compiled shim placed into the project's plugin directory with
// its dispatcher placeholder resolved; hooks-json targets get printed
// guidance pointing at the harness's own plugin-marketplace commands,
// since installing those is that harness's job, not whippletree's.
func runInstall(args []string, stdout, stderr io.Writer) int {
	const usage = "usage: whippletree install <bundleDir> --target <name> [--project dir] [--assume-version X] [--targets-dir dir]"

	parsed, err := parseInstallArgs(args)
	if err != nil {
		fmt.Fprintf(stderr, "whippletree: %v\n", err)
		fmt.Fprintln(stderr, usage)
		return 1
	}

	td, report, probed, ok := checkAgainstTarget(parsed.bundleDir, parsed.targetName, parsed.assumeVersion, parsed.targetsDir, stdout, stderr)
	if !ok {
		return 1
	}

	// REFUSE places nothing: the report has already been printed by
	// checkAgainstTarget, so there's nothing left to do but stop.
	if report.Refused {
		return 1
	}

	switch td.Backend {
	case target.BackendTSPlugin:
		if err := placeTSPlugin(parsed.bundleDir, parsed.project, parsed.targetName, stderr); err != nil {
			fmt.Fprintf(stderr, "whippletree: %v\n", err)
			return 1
		}
	default:
		printHooksJSONGuidance(stdout, parsed.targetName, parsed.bundleDir, td)
	}

	if err := writeInstallState(parsed.bundleDir, parsed.targetName, string(probed), report); err != nil {
		fmt.Fprintf(stderr, "whippletree: write install state: %v\n", err)
		return 1
	}

	return 0
}

// placeTSPlugin places a ts-plugin target's compiled shim
// (<bundleDir>/hooks/<targetName>.ts, written by a prior
// `whippletree build`) into <projectDir>/.opencode/plugin/whippletree-<bundle
// name>.ts, with its dispatcher placeholder resolved to the bundle's
// absolute bin/whippletree-hook path.
func placeTSPlugin(bundleDir, projectDir, targetName string, stderr io.Writer) error {
	// The dispatcher must already be provisioned: every dispatch() call
	// the shim makes invokes it directly (there's no plugin-root env
	// var to resolve it from, unlike a hooks-json target). Reuse
	// build's own check rather than duplicating it.
	if err := ensureDispatcher(bundleDir, false, stderr); err != nil {
		return err
	}

	hooksSrc := filepath.Join(bundleDir, "hooks", targetName+".ts")
	body, err := os.ReadFile(hooksSrc)
	if err != nil {
		return fmt.Errorf("read compiled shim %s (run `whippletree build` first): %w", hooksSrc, err)
	}

	absBundleDir, err := filepath.Abs(bundleDir)
	if err != nil {
		return fmt.Errorf("resolve absolute path for %s: %w", bundleDir, err)
	}
	dispatcherPath := filepath.Join(absBundleDir, "bin", "whippletree-hook")

	// The compiler contract guarantees exactly one HOOK placeholder
	// occurrence. A stale or hand-corrupted hooks file could have zero
	// (nothing to resolve, install would otherwise silently ship an
	// already-broken shim), and a future compiler bug or tampering
	// could produce two or more (a plain Replace(..., 1) would resolve
	// only the first, silently shipping the literal placeholder
	// alongside it). Either way this is not something install should
	// paper over with a fail-open success.
	if n := strings.Count(string(body), tsPluginHookPlaceholder); n != 1 {
		return fmt.Errorf("%s: expected exactly one %s placeholder, found %d; regenerate it with `whippletree build`", hooksSrc, tsPluginHookPlaceholder, n)
	}
	resolved := strings.Replace(string(body), tsPluginHookPlaceholder, dispatcherPath, 1)

	bundleName, err := readBundleName(bundleDir)
	if err != nil {
		return err
	}

	destDir := filepath.Join(projectDir, ".opencode", "plugin")
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return fmt.Errorf("create plugin dir %s: %w", destDir, err)
	}
	dest := filepath.Join(destDir, "whippletree-"+bundleName+".ts")

	if err := checkOverwriteAllowed(dest); err != nil {
		return err
	}

	if err := os.WriteFile(dest, []byte(resolved), 0o644); err != nil {
		return fmt.Errorf("write plugin shim %s: %w", dest, err)
	}
	return nil
}

// readBundleName reads the "name" field straight off <bundleDir>/plugin.json.
// This is the bundle name install uses to derive its destination
// filename and, for a hooks-json target, the guidance's install
// command.
func readBundleName(bundleDir string) (string, error) {
	raw, err := os.ReadFile(filepath.Join(bundleDir, "plugin.json"))
	if err != nil {
		return "", fmt.Errorf("read plugin manifest: %w", err)
	}
	var m struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(raw, &m); err != nil {
		return "", fmt.Errorf("parse plugin manifest: %w", err)
	}
	if m.Name == "" {
		return "", fmt.Errorf("plugin manifest %s is missing \"name\"", filepath.Join(bundleDir, "plugin.json"))
	}
	return m.Name, nil
}

// checkOverwriteAllowed refuses to let install clobber a destination
// file it didn't write itself: a pre-existing file is only safe to
// overwrite when its first line is the generated-by marker, i.e. a
// previous install's own output. A missing destination is always fine
// (there's nothing to protect).
func checkOverwriteAllowed(dest string) error {
	existing, err := os.ReadFile(dest)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("check existing %s: %w", dest, err)
	}

	firstLine, _, _ := strings.Cut(string(existing), "\n")
	if firstLine != generatedByMarker {
		return fmt.Errorf("%s already exists and was not generated by whippletree; refusing to overwrite it", dest)
	}
	return nil
}

// hooksJSONInstallVerb is the CLI verb a hooks-json target's own
// harness uses for "install a plugin from a marketplace": claude-code
// uses "install", codex uses "add" (see README.md's install section).
func hooksJSONInstallVerb(targetName string) string {
	if targetName == "codex" {
		return "add"
	}
	return "install"
}

// printHooksJSONGuidance prints the guidance install shows for a
// hooks-json target: installing it is that harness's own
// plugin-marketplace mechanism, not something whippletree places
// itself. The two commands mirror README.md's install section: point
// the harness's marketplace at the bundle directory, then install the
// bundle's plugin from it (by convention, marketplace name
// "<bundle name>-mkt").
func printHooksJSONGuidance(stdout io.Writer, targetName, bundleDir string, td *target.Def) {
	cli := targetName
	if len(td.Probe.Command) > 0 {
		cli = td.Probe.Command[0]
	}

	bundleName, err := readBundleName(bundleDir)
	if err != nil {
		bundleName = "<bundle-name>"
	}

	fmt.Fprintf(stdout, "install for %s is the harness's own plugin mechanism:\n\n", targetName)
	fmt.Fprintf(stdout, "  %s plugin marketplace add %s\n", cli, bundleDir)
	fmt.Fprintf(stdout, "  %s plugin %s %s@%s-mkt\n", cli, hooksJSONInstallVerb(targetName), bundleName, bundleName)
}

// stdinIsTTY reports whether the process's stdin is a character
// device (a real terminal), the signal that an interactive session
// could actually prompt a human rather than reading from a pipe/file.
func stdinIsTTY() bool {
	info, err := os.Stdin.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}
