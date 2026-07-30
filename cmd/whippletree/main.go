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
	"time"

	"github.com/larstonder/whippletree/internal/compile"
	"github.com/larstonder/whippletree/internal/contract"
	"github.com/larstonder/whippletree/internal/preflight"
	"github.com/larstonder/whippletree/internal/target"
)

// defaultTargetsDir is the on-disk location of the class-1 target
// definitions, relative to the working directory the CLI is invoked
// from (the whippletree repo root), used unless --targets-dir
// overrides it.
const defaultTargetsDir = "targets"

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

// run implements the whippletree CLI. It is split out from main so
// tests can exercise it without touching process-global os.Args/Exit.
func run(args []string, stdout, stderr io.Writer) int {
	if len(args) < 1 {
		fmt.Fprintln(stderr, "usage: whippletree <build|preflight> ...")
		return 1
	}

	switch args[0] {
	case "build":
		return runBuild(args[1:], stdout, stderr)
	case "preflight":
		return runPreflight(args[1:], stdout, stderr)
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

// parseBuildArgs parses
// `<bundleDir> [--targets-dir dir] [--allow-missing-dispatcher] [--allow-refuse]`.
func parseBuildArgs(args []string) (buildArgs, error) {
	parsed := buildArgs{targetsDir: defaultTargetsDir}

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

// runBuild implements
// `whippletree build <bundleDir> [--targets-dir dir] [--allow-missing-dispatcher] [--allow-refuse]`.
func runBuild(args []string, stdout, stderr io.Writer) int {
	const usage = "usage: whippletree build <bundleDir> [--targets-dir dir] [--allow-missing-dispatcher] [--allow-refuse]"

	parsed, err := parseBuildArgs(args)
	if err != nil {
		fmt.Fprintf(stderr, "whippletree: %v\n", err)
		fmt.Fprintln(stderr, usage)
		return 1
	}

	targets, err := target.LoadDir(parsed.targetsDir)
	if err != nil {
		fmt.Fprintf(stderr, "whippletree: %v\n", err)
		return 1
	}

	result, err := compile.Build(parsed.bundleDir, targets)
	if err != nil {
		fmt.Fprintf(stderr, "whippletree: build: %v\n", err)
		return 1
	}

	if err := ensureDispatcher(parsed.bundleDir, parsed.allowMissingDispatcher, stderr); err != nil {
		fmt.Fprintf(stderr, "whippletree: %v\n", err)
		return 1
	}

	names := make([]string, 0, len(result.PerTarget))
	for name := range result.PerTarget {
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
		for _, a := range result.PerTarget[name] {
			// preflight.Classify is the single source of truth for the
			// tier-comparison/hard-requirement verdict rules; build has
			// no probed version to fail closed on (tier assignment
			// depends only on the target definition, not the installed
			// version), so it classifies straight from compile.Result
			// without calling preflight.Check.
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

// runPreflight implements
// `whippletree preflight <bundleDir> --target <name> [--assume-version X] [--targets-dir dir]`.
func runPreflight(args []string, stdout, stderr io.Writer) int {
	bundleDir, targetName, assumeVersion, targetsDirArg, err := parsePreflightArgs(args)
	if err != nil {
		fmt.Fprintf(stderr, "whippletree: %v\n", err)
		fmt.Fprintln(stderr, "usage: whippletree preflight <bundleDir> --target <name> [--assume-version X] [--targets-dir dir]")
		return 1
	}

	targets, err := target.LoadDir(targetsDirArg)
	if err != nil {
		fmt.Fprintf(stderr, "whippletree: %v\n", err)
		return 1
	}
	td, ok := targets[targetName]
	if !ok {
		fmt.Fprintf(stderr, "whippletree: unknown target %q\n", targetName)
		return 1
	}

	rawManifest, err := os.ReadFile(filepath.Join(bundleDir, "plugin.json"))
	if err != nil {
		fmt.Fprintf(stderr, "whippletree: read plugin manifest: %v\n", err)
		return 1
	}
	c, err := contract.Parse(rawManifest)
	if err != nil {
		fmt.Fprintf(stderr, "whippletree: parse contract: %v\n", err)
		return 1
	}
	if err := contract.Validate(c); err != nil {
		fmt.Fprintf(stderr, "whippletree: validate contract: %v\n", err)
		return 1
	}

	// interactive reflects whether this invocation can actually prompt
	// a human: previously hardcoded false, which meant Check always
	// failed closed on an unprobed version even when run at a real
	// terminal. A TTY'd stdin is the signal an interactive session is
	// possible.
	interactive := stdinIsTTY()

	var probed preflight.Version
	if assumeVersion != "" {
		probed = preflight.Version(assumeVersion)
	} else {
		v, err := preflight.Probe(td)
		if err != nil {
			// Probe failed to run/parse the harness's own version
			// output: a different failure mode than Check's fail-closed
			// error below, so it keeps its own distinct message.
			fmt.Fprintf(stderr, "preflight: probe failed: %v\n", err)
			return 1
		}
		probed = v
	}

	report, err := preflight.Check(c, td, probed, interactive)
	if err != nil {
		// Check's only error is the fail-closed "no probed version and
		// not interactive" case, not a probe failure; wrap it with its
		// own message so the two failure modes aren't conflated.
		fmt.Fprintf(stderr, "preflight: %v\n", err)
		return 1
	}

	fmt.Fprint(stdout, report.Render(targetName, probed))

	if report.Refused {
		return 1
	}

	if err := writeInstallState(bundleDir, targetName, string(probed), report); err != nil {
		fmt.Fprintf(stderr, "whippletree: write install state: %v\n", err)
		return 1
	}

	return 0
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

// parsePreflightArgs parses
// `<bundleDir> --target <name> [--assume-version X] [--targets-dir dir]`.
func parsePreflightArgs(args []string) (bundleDir, targetName, assumeVersion, targetsDirArg string, err error) {
	targetsDirArg = defaultTargetsDir

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
