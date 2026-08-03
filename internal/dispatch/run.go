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
	"strconv"

	"github.com/larstonder/whippletree/internal/contract"
	"github.com/larstonder/whippletree/internal/target"
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
// going.
//
// When no requirement matches logicalEvent, Run returns 0 without ever
// loading the target definition or calling Normalize: the cheap no-op
// path for events the bundle doesn't care about.
func Run(bundleRoot, logicalEvent, targetName string, stdin []byte, stderr io.Writer) int {
	c, err := loadVendoredContract(bundleRoot)
	if err != nil {
		fmt.Fprintf(stderr, "whippletree-hook: %v\n", err)
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
		fmt.Fprintf(stderr, "whippletree-hook: %v\n", err)
		return 1
	}

	ev, err := Normalize(logicalEvent, td, stdin)
	if err != nil {
		fmt.Fprintf(stderr, "whippletree-hook: %v\n", err)
		return 1
	}
	payload, err := json.Marshal(ev)
	if err != nil {
		fmt.Fprintf(stderr, "whippletree-hook: marshal event: %v\n", err)
		return 1
	}

	for _, req := range matching {
		exitCode, handlerStderr, ran := runHandler(bundleRoot, req.Handler, logicalEvent, targetName, ev, payload, stderr)
		if !ran {
			continue
		}

		// Handler diagnostics are useful whether or not it blocked, so
		// forward stderr on every exit path.
		if len(handlerStderr) > 0 {
			stderr.Write(handlerStderr)
		}

		switch {
		case exitCode == 2:
			return 2
		case exitCode != 0:
			fmt.Fprintf(stderr, "whippletree-hook: handler %s exited %d (ignored)\n", req.Handler, exitCode)
		}
	}

	return 0
}

// loadVendoredContract reads and parses bundleRoot's vendored
// .whippletree/contract.json (already the parsed, normalized form
// compile.Build wrote; no re-validation needed here).
func loadVendoredContract(bundleRoot string) (*contract.Contract, error) {
	path := filepath.Join(bundleRoot, ".whippletree", "contract.json")
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

func loadVendoredTarget(bundleRoot, targetName string) (*target.Def, error) {
	path := filepath.Join(bundleRoot, ".whippletree", "targets", targetName+".yaml")
	td, err := target.Load(path)
	if err != nil {
		return nil, fmt.Errorf("load vendored target: %w", err)
	}
	return td, nil
}

// runHandler executes handlerRelPath (resolved relative to bundleRoot)
// with payload on stdin and the handler env vars added to the parent
// environment: ADAPTER_EVENT (logicalEvent, verbatim as the contract
// author wrote it) and ADAPTER_TARGET, plus a projection of ev's
// normalized fields, always set (empty string means not applicable to
// this event): ADAPTER_PRIMITIVE (ev.Event, never empty),
// ADAPTER_STOP_ACTIVE ("true"/"false" when ev.StopHookActive is
// non-nil, else empty), ADAPTER_CWD (ev.CWD), and ADAPTER_PATH
// (ev.Paths[0] when present, else empty). ran is false when the
// handler file is missing or not executable; runHandler has already
// written a clear, fail-open message to stderr in that case, and the
// caller should just continue. When ran is true, exitCode and
// handlerStderr reflect the handler's own exit status and captured
// stderr, for the caller to interpret (exit 2 is the block dialect;
// anything else is fail-open logging the caller performs itself).
func runHandler(bundleRoot, handlerRelPath, logicalEvent, targetName string, ev *Event, payload []byte, stderr io.Writer) (exitCode int, handlerStderr []byte, ran bool) {
	handlerPath := filepath.Join(bundleRoot, handlerRelPath)

	info, err := os.Stat(handlerPath)
	if err != nil {
		fmt.Fprintf(stderr, "whippletree-hook: handler %s: missing (%v) (ignored)\n", handlerRelPath, err)
		return 0, nil, false
	}
	if info.IsDir() || info.Mode()&0o111 == 0 {
		fmt.Fprintf(stderr, "whippletree-hook: handler %s: not executable (ignored)\n", handlerRelPath)
		return 0, nil, false
	}

	stopActive := ""
	if ev.StopHookActive != nil {
		stopActive = strconv.FormatBool(*ev.StopHookActive)
	}
	path := ""
	if len(ev.Paths) > 0 {
		path = ev.Paths[0]
	}

	cmd := exec.Command(handlerPath)
	cmd.Stdin = bytes.NewReader(payload)
	var captured bytes.Buffer
	cmd.Stderr = &captured
	cmd.Env = append(os.Environ(),
		"ADAPTER_EVENT="+logicalEvent,
		"ADAPTER_TARGET="+targetName,
		"ADAPTER_PRIMITIVE="+ev.Event,
		"ADAPTER_STOP_ACTIVE="+stopActive,
		"ADAPTER_CWD="+ev.CWD,
		"ADAPTER_PATH="+path,
	)

	runErr := cmd.Run()
	if runErr == nil {
		return 0, captured.Bytes(), true
	}

	var exitErr *exec.ExitError
	if errors.As(runErr, &exitErr) {
		return exitErr.ExitCode(), captured.Bytes(), true
	}

	fmt.Fprintf(stderr, "whippletree-hook: handler %s: %v (ignored)\n", handlerRelPath, runErr)
	return 0, nil, false
}
