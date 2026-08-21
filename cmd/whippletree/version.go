package main

import (
	"fmt"
	"io"
	"runtime"
	"runtime/debug"
	"sort"

	"github.com/larstonder/whippletree/internal/contract"
)

// Build metadata set via -ldflags at release time, e.g.
// -X main.version=v0.1.0. Unset, these fall back to Go build info.
var (
	version   = ""
	commit    = ""
	buildDate = ""
)

// runVersion prints build provenance and the target definitions compiled
// into this binary. The target block is the point: which harness
// versions a binary was tested against is a question about that binary.
func runVersion(args []string, stdout, stderr io.Writer) int {
	parsed, err := parseVersionArgs(args)
	if err != nil {
		fmt.Fprintf(stderr, "whippletree: %v\n", err)
		fmt.Fprintln(stderr, "usage: whippletree version [--targets-dir dir]")
		return 1
	}

	v, c, d := buildInfo()
	fmt.Fprintf(stdout, "whippletree %s\n", v)
	if c != "" {
		fmt.Fprintf(stdout, "  commit:  %s\n", c)
	}
	if d != "" {
		fmt.Fprintf(stdout, "  built:   %s\n", d)
	}
	fmt.Fprintf(stdout, "  go:      %s %s/%s\n", runtime.Version(), runtime.GOOS, runtime.GOARCH)
	fmt.Fprintf(stdout, "  contract: %s\n", contract.SupportedContractVersion)

	defs, err := loadTargets(parsed.targetsDir)
	if err != nil {
		fmt.Fprintf(stderr, "whippletree: %v\n", err)
		return 1
	}

	names := make([]string, 0, len(defs))
	for name := range defs {
		names = append(names, name)
	}
	sort.Strings(names)

	fmt.Fprintf(stdout, "\ntargets (%d):\n", len(names))
	width := 0
	for _, n := range names {
		if len(n) > width {
			width = len(n)
		}
	}
	for _, n := range names {
		td := defs[n]
		schema := td.SchemaVersion
		if schema == "" {
			schema = "—"
		}
		tested := td.TestedVersions
		if tested == "" {
			tested = "untested"
		}
		fmt.Fprintf(stdout, "  %-*s  schema %-7s  tested %s\n", width, n, schema, tested)
	}

	return 0
}

type versionArgs struct{ targetsDir string }

func parseVersionArgs(args []string) (versionArgs, error) {
	var parsed versionArgs
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--targets-dir":
			if i+1 >= len(args) {
				return versionArgs{}, fmt.Errorf("--targets-dir requires a value")
			}
			parsed.targetsDir = args[i+1]
			i++
		default:
			return versionArgs{}, fmt.Errorf("unexpected argument %q", args[i])
		}
	}
	return parsed, nil
}

// buildInfo prefers ldflags-injected values, falling back to Go build info.
func buildInfo() (v, c, d string) {
	v, c, d = version, commit, buildDate

	info, ok := debug.ReadBuildInfo()
	if !ok {
		if v == "" {
			v = "(devel)"
		}
		return v, c, d
	}

	if v == "" {
		v = info.Main.Version
		if v == "" {
			v = "(devel)"
		}
	}
	// Only annotate dirtiness when the revision came from build info; a
	// release binary's ldflags commit should not inherit it.
	fromBuildInfo := c == ""
	var modified bool
	for _, s := range info.Settings {
		switch s.Key {
		case "vcs.revision":
			if c == "" {
				c = s.Value
			}
		case "vcs.time":
			if d == "" {
				d = s.Value
			}
		case "vcs.modified":
			modified = s.Value == "true"
		}
	}
	if fromBuildInfo && modified && c != "" {
		c += " (dirty)"
	}
	return v, c, d
}

const usageText = `whippletree compiles one declared contract onto several AI coding harnesses.

usage: whippletree <command> [args]

commands:
  init       scaffold a new bundle
  build      compile a bundle's per-target artifacts
  preflight  report what a target will satisfy, degrade, or refuse
  install    preflight, then install into a harness
  version    print build provenance and the compiled-in target definitions

run a command with no arguments for its own usage line.
`
