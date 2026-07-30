package dispatch

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/larstonder/adapter-sdk/internal/contract"
	"github.com/larstonder/adapter-sdk/internal/target"
)

// Run loads bundleRoot's vendored contract and target definition, selects
// the requirements whose Event field equals logicalEvent (matched
// verbatim, alias or primitive, exactly as the contract author wrote
// it), and executes each matching requirement's handler in process
// order against the normalized event JSON on stdin.
//
// This is the exit-2 refusal dialect: a handler exiting 2 is a hard
// block. Its stderr is copied through to stderr and its exit code (2)
// is returned immediately, without running any later handler ("first
// block wins"). Any other non-zero exit, or a missing/non-executable
// handler file, is logged to stderr and ignored (fail-open); Run keeps
// going. Requirements with an empty Handler (the executable-path kind)
// are never executed.
//
// When no requirement matches logicalEvent, Run returns 0 without ever
// loading the target definition or calling Normalize: the cheap no-op
// path for events the bundle doesn't care about.
func Run(bundleRoot, logicalEvent, targetName string, stdin []byte, stderr io.Writer) int {
	c, err := loadVendoredContract(bundleRoot)
	if err != nil {
		fmt.Fprintf(stderr, "adapter-hook: %v\n", err)
		return 1
	}

	var matching []contract.Requirement
	for _, req := range c.Requires {
		if req.Event == logicalEvent {
			matching = append(matching, req)
		}
	}
	if len(matching) == 0 {
		return 0
	}

	td, err := loadVendoredTarget(bundleRoot, targetName)
	if err != nil {
		fmt.Fprintf(stderr, "adapter-hook: %v\n", err)
		return 1
	}

	ev, err := Normalize(logicalEvent, td, stdin)
	if err != nil {
		fmt.Fprintf(stderr, "adapter-hook: %v\n", err)
		return 1
	}
	payload, err := json.Marshal(ev)
	if err != nil {
		fmt.Fprintf(stderr, "adapter-hook: marshal event: %v\n", err)
		return 1
	}

	for _, req := range matching {
		if req.Handler == "" {
			continue
		}

		exitCode, handlerStderr, ran := runHandler(bundleRoot, req.Handler, logicalEvent, targetName, payload, stderr)
		if !ran {
			continue
		}

		switch {
		case exitCode == 2:
			stderr.Write(handlerStderr)
			return 2
		case exitCode != 0:
			fmt.Fprintf(stderr, "adapter-hook: handler %s exited %d (ignored)\n", req.Handler, exitCode)
		}
	}

	return 0
}

// loadVendoredContract reads and parses bundleRoot's vendored
// .adapter-sdk/contract.json (already the parsed, normalized form
// compile.Build wrote; no re-validation needed here).
func loadVendoredContract(bundleRoot string) (*contract.Contract, error) {
	path := filepath.Join(bundleRoot, ".adapter-sdk", "contract.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read vendored contract: %w", err)
	}
	var c contract.Contract
	if err := json.Unmarshal(raw, &c); err != nil {
		return nil, fmt.Errorf("parse vendored contract: %w", err)
	}
	return &c, nil
}

// loadVendoredTarget loads bundleRoot's vendored
// .adapter-sdk/targets/<targetName>.yaml.
func loadVendoredTarget(bundleRoot, targetName string) (*target.Def, error) {
	path := filepath.Join(bundleRoot, ".adapter-sdk", "targets", targetName+".yaml")
	td, err := target.Load(path)
	if err != nil {
		return nil, fmt.Errorf("load vendored target %q: %w", targetName, err)
	}
	return td, nil
}

// runHandler executes handlerRelPath (resolved relative to bundleRoot)
// with payload on stdin and ADAPTER_EVENT/ADAPTER_TARGET added to the
// parent environment. ran is false when the handler file is missing or
// not executable; runHandler has already written a clear, fail-open
// message to stderr in that case, and the caller should just continue.
// When ran is true, exitCode and handlerStderr reflect the handler's
// own exit status and captured stderr, for the caller to interpret
// (exit 2 is the block dialect; anything else is fail-open logging the
// caller performs itself, since only the exit-2 case forwards the
// handler's stderr).
func runHandler(bundleRoot, handlerRelPath, logicalEvent, targetName string, payload []byte, stderr io.Writer) (exitCode int, handlerStderr []byte, ran bool) {
	handlerPath := filepath.Join(bundleRoot, handlerRelPath)

	info, err := os.Stat(handlerPath)
	if err != nil {
		fmt.Fprintf(stderr, "adapter-hook: handler %s: missing (%v) (ignored)\n", handlerRelPath, err)
		return 0, nil, false
	}
	if info.IsDir() || info.Mode()&0o111 == 0 {
		fmt.Fprintf(stderr, "adapter-hook: handler %s: not executable (ignored)\n", handlerRelPath)
		return 0, nil, false
	}

	cmd := exec.Command(handlerPath)
	cmd.Stdin = bytes.NewReader(payload)
	var captured bytes.Buffer
	cmd.Stderr = &captured
	cmd.Env = append(os.Environ(),
		"ADAPTER_EVENT="+logicalEvent,
		"ADAPTER_TARGET="+targetName,
	)

	runErr := cmd.Run()
	if runErr == nil {
		return 0, captured.Bytes(), true
	}

	var exitErr *exec.ExitError
	if errors.As(runErr, &exitErr) {
		return exitErr.ExitCode(), captured.Bytes(), true
	}

	fmt.Fprintf(stderr, "adapter-hook: handler %s: %v (ignored)\n", handlerRelPath, runErr)
	return 0, nil, false
}
