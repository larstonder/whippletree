// Command adapter-sdk is the developer-facing CLI: it compiles a
// bundle's per-target variants (build) and reports whether a target
// can satisfy a bundle's contract before install (preflight).
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/larstonder/adapter-sdk/internal/compile"
	"github.com/larstonder/adapter-sdk/internal/contract"
	"github.com/larstonder/adapter-sdk/internal/preflight"
	"github.com/larstonder/adapter-sdk/internal/target"
	"github.com/larstonder/adapter-sdk/internal/tier"
)

// targetsDir is the on-disk location of the class-1 target definitions,
// relative to the working directory the CLI is invoked from (the
// adapter-sdk repo root).
const targetsDir = "targets"

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

// run implements the adapter-sdk CLI. It is split out from main so
// tests can exercise it without touching process-global os.Args/Exit.
func run(args []string, stdout, stderr io.Writer) int {
	if len(args) < 1 {
		fmt.Fprintln(stderr, "usage: adapter-sdk <build|preflight> ...")
		return 1
	}

	switch args[0] {
	case "build":
		return runBuild(args[1:], stdout, stderr)
	case "preflight":
		return runPreflight(args[1:], stdout, stderr)
	default:
		fmt.Fprintf(stderr, "adapter-sdk: unknown subcommand %q\n", args[0])
		return 1
	}
}

// runBuild implements `adapter-sdk build <bundleDir>`.
func runBuild(args []string, stdout, stderr io.Writer) int {
	if len(args) < 1 {
		fmt.Fprintln(stderr, "usage: adapter-sdk build <bundleDir>")
		return 1
	}
	bundleDir := args[0]

	targets, err := target.LoadDir(targetsDir)
	if err != nil {
		fmt.Fprintf(stderr, "adapter-sdk: %v\n", err)
		return 1
	}

	result, err := compile.Build(bundleDir, targets)
	if err != nil {
		fmt.Fprintf(stderr, "adapter-sdk: build: %v\n", err)
		return 1
	}

	names := make([]string, 0, len(result.PerTarget))
	for name := range result.PerTarget {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		satisfy, degrade, refuse, absent := 0, 0, 0, 0
		for _, a := range result.PerTarget[name] {
			switch classifyAssignment(a) {
			case preflight.Satisfy:
				satisfy++
			case preflight.Degrade:
				degrade++
			case preflight.Refuse:
				refuse++
			case preflight.Absent:
				absent++
			}
		}
		fmt.Fprintf(stdout, "target %s: %d satisfy, %d degrade, %d refuse, %d absent\n", name, satisfy, degrade, refuse, absent)
	}

	return 0
}

// classifyAssignment mirrors preflight.Check's per-requirement verdict
// logic, applied directly to an already-computed tier.Assignment. build
// has no probed version to fail closed on (tier assignment depends only
// on the target definition, not the installed version), so it can
// classify straight from compile.Result without calling preflight.Check.
func classifyAssignment(a tier.Assignment) string {
	hard := a.Req.HardRequired != nil && *a.Req.HardRequired
	switch {
	case a.Absent && hard:
		return preflight.Refuse
	case a.Absent:
		return preflight.Absent
	case a.Tier <= a.Req.MinTier:
		return preflight.Satisfy
	case hard:
		return preflight.Refuse
	default:
		return preflight.Degrade
	}
}

// installState is the shape written to
// <bundleDir>/.adapter-sdk/install-state.json on a successful (non-
// refused) preflight.
type installState struct {
	Harness   string            `json:"harness"`
	Version   string            `json:"version"`
	CheckedAt string            `json:"checkedAt"`
	Tiers     map[string]string `json:"tiers"`
}

// runPreflight implements
// `adapter-sdk preflight <bundleDir> --target <name> [--assume-version X]`.
func runPreflight(args []string, stdout, stderr io.Writer) int {
	bundleDir, targetName, assumeVersion, err := parsePreflightArgs(args)
	if err != nil {
		fmt.Fprintf(stderr, "adapter-sdk: %v\n", err)
		fmt.Fprintln(stderr, "usage: adapter-sdk preflight <bundleDir> --target <name> [--assume-version X]")
		return 1
	}

	targets, err := target.LoadDir(targetsDir)
	if err != nil {
		fmt.Fprintf(stderr, "adapter-sdk: %v\n", err)
		return 1
	}
	td, ok := targets[targetName]
	if !ok {
		fmt.Fprintf(stderr, "adapter-sdk: unknown target %q\n", targetName)
		return 1
	}

	rawManifest, err := os.ReadFile(filepath.Join(bundleDir, "plugin.json"))
	if err != nil {
		fmt.Fprintf(stderr, "adapter-sdk: read plugin manifest: %v\n", err)
		return 1
	}
	c, err := contract.Parse(rawManifest)
	if err != nil {
		fmt.Fprintf(stderr, "adapter-sdk: parse contract: %v\n", err)
		return 1
	}
	if err := contract.Validate(c); err != nil {
		fmt.Fprintf(stderr, "adapter-sdk: validate contract: %v\n", err)
		return 1
	}

	var probed preflight.Version
	interactive := false
	if assumeVersion != "" {
		probed = preflight.Version(assumeVersion)
	} else {
		v, err := preflight.Probe(td)
		if err != nil {
			fmt.Fprintf(stderr, "preflight: probe failed: %v\n", err)
			return 1
		}
		probed = v
	}

	report, err := preflight.Check(c, td, probed, interactive)
	if err != nil {
		fmt.Fprintf(stderr, "preflight: probe failed: %v\n", err)
		return 1
	}

	fmt.Fprint(stdout, report.Render(targetName, probed))

	if report.Refused {
		return 1
	}

	if err := writeInstallState(bundleDir, targetName, string(probed), report); err != nil {
		fmt.Fprintf(stderr, "adapter-sdk: write install state: %v\n", err)
		return 1
	}

	return 0
}

// writeInstallState writes <bundleDir>/.adapter-sdk/install-state.json
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

	dir := filepath.Join(bundleDir, ".adapter-sdk")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create .adapter-sdk dir: %w", err)
	}
	return os.WriteFile(filepath.Join(dir, "install-state.json"), body, 0o644)
}

// parsePreflightArgs parses `<bundleDir> --target <name> [--assume-version X]`.
func parsePreflightArgs(args []string) (bundleDir, targetName, assumeVersion string, err error) {
	var positional []string
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--target":
			if i+1 >= len(args) {
				return "", "", "", fmt.Errorf("--target requires a value")
			}
			targetName = args[i+1]
			i++
		case "--assume-version":
			if i+1 >= len(args) {
				return "", "", "", fmt.Errorf("--assume-version requires a value")
			}
			assumeVersion = args[i+1]
			i++
		default:
			positional = append(positional, args[i])
		}
	}

	if len(positional) < 1 {
		return "", "", "", fmt.Errorf("missing <bundleDir>")
	}
	bundleDir = positional[0]

	if targetName == "" {
		return "", "", "", fmt.Errorf("missing --target")
	}

	return bundleDir, targetName, assumeVersion, nil
}
