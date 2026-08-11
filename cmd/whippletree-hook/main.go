// Command whippletree-hook is the process a harness's native hooks file
// invokes. It is not meant to be run by humans: harnesses call
// `whippletree-hook run <event> --target <name>` with the raw hook payload
// on stdin, and whippletree-hook dispatches it to the bundle's declared
// handlers.
package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/larstonder/whippletree/internal/dispatch"
	"github.com/larstonder/whippletree/internal/target"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdin, os.Stderr))
}

// run is main's testable body.
func run(args []string, stdin io.Reader, stderr io.Writer) int {
	event, targetName, err := parseRunArgs(args)
	if err != nil {
		fmt.Fprintln(stderr, err)
		fmt.Fprintln(stderr, "usage: whippletree-hook run <event> --target <name>")
		return 1
	}

	bundleRoot, err := resolveBundleRoot(targetName)
	if err != nil {
		fmt.Fprintf(stderr, "whippletree-hook: %v\n", err)
		return 1
	}

	in, err := io.ReadAll(stdin)
	if err != nil {
		fmt.Fprintf(stderr, "whippletree-hook: read stdin: %v\n", err)
		return 1
	}

	return dispatch.Run(bundleRoot, event, targetName, in, os.Stdout, stderr)
}

// parseRunArgs parses `run <event> --target <name>`. <event> and
// --target's value may appear in either order after "run".
func parseRunArgs(args []string) (event, targetName string, err error) {
	if len(args) < 1 || args[0] != "run" {
		return "", "", fmt.Errorf("whippletree-hook: expected subcommand \"run\"")
	}

	rest := args[1:]
	for i := 0; i < len(rest); i++ {
		if rest[i] == "--target" {
			if i+1 >= len(rest) {
				return "", "", fmt.Errorf("whippletree-hook: --target requires a value")
			}
			targetName = rest[i+1]
			i++
			continue
		}
		if event == "" {
			event = rest[i]
		}
	}

	if event == "" {
		return "", "", fmt.Errorf("whippletree-hook: missing <event>")
	}
	if targetName == "" {
		return "", "", fmt.Errorf("whippletree-hook: missing --target")
	}
	return event, targetName, nil
}

// resolveBundleRoot finds the bundle root this invocation is running
// inside of. It prefers the first SET env var among the target's
// PluginRootVars (the harness-provided plugin-root convention, e.g.
// CLAUDE_PLUGIN_ROOT); those vars are learned from the vendored target
// definition at the binary's self-located candidate root. When none of
// them is set, it falls back to that candidate root outright.
func resolveBundleRoot(targetName string) (string, error) {
	selfRoot, err := selfBundleRoot()
	if err != nil {
		return "", err
	}

	vendoredPath := filepath.Join(selfRoot, ".whippletree", "targets", targetName+".yaml")
	if td, err := target.Load(vendoredPath); err == nil {
		for _, v := range td.PluginRootVars {
			if val, ok := os.LookupEnv(v); ok && val != "" {
				return val, nil
			}
		}
	}

	return selfRoot, nil
}

// selfBundleRoot resolves the binary's own grandparent directory. A
// bundle-installed whippletree-hook always lives at
// <bundleRoot>/bin/whippletree-hook, so dirname(dirname(argv0)) recovers
// bundleRoot. Symlinks are resolved first: os.Executable can return a
// symlinked path (e.g. through a plugin marketplace's shared bin/
// directory), and only the real, installed location sits under the
// actual bundle root.
func selfBundleRoot() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("resolve executable path: %w", err)
	}
	real, err := filepath.EvalSymlinks(exe)
	if err != nil {
		return "", fmt.Errorf("resolve executable symlinks: %w", err)
	}
	return filepath.Dir(filepath.Dir(real)), nil
}
