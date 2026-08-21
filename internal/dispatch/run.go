package dispatch

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/larstonder/whippletree/internal/contract"
	"github.com/larstonder/whippletree/internal/target"
)

// HandlerTimeout bounds a single handler run. A handler that outlives it
// is killed and treated as fail-open, matching every other non-blocking
// failure in this dialect: a wedged handler must never wedge the harness.
//
// 30s is deliberately the same default GitHub Copilot CLI uses for its
// own hook timeoutSec, and Copilot likewise fails open on expiry, so the
// two agree by construction rather than by coincidence.
const HandlerTimeout = 30 * time.Second

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
//
// Each handler's stdout is forwarded verbatim to stdout after the
// handler exits, in invocation order and regardless of exit code; what
// stdout means is the harness's decision (see docs/AUTHORING.md).
func Run(bundleRoot, logicalEvent, targetName string, stdin []byte, stdout, stderr io.Writer) int {
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
		exitCode, handlerStdout, handlerStderr, ran := runHandler(bundleRoot, req.Handler, logicalEvent, targetName, ev, payload, stderr)
		if !ran {
			continue
		}

		// The handler's stdout is the payload channel: forward it
		// verbatim on every exit path and let the harness assign it
		// meaning (on claude-code, SessionStart stdout becomes context).
		if len(handlerStdout) > 0 {
			if _, err := stdout.Write(handlerStdout); err != nil {
				fmt.Fprintf(stderr, "whippletree-hook: handler %s: forward stdout: %v (ignored)\n", req.Handler, err)
			}
		}

		// Handler diagnostics are useful whether or not it blocked, so
		// forward stderr on every exit path. A failure here has nowhere
		// left to be reported to, so it is dropped rather than logged.
		if len(handlerStderr) > 0 {
			_, _ = stderr.Write(handlerStderr)
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
// caller should just continue. When ran is true, exitCode,
// handlerStdout, and handlerStderr reflect the handler's own exit
// status and captured stdout/stderr, for the caller to interpret (exit
// 2 is the block dialect; anything else is fail-open logging the
// caller performs itself).
func runHandler(bundleRoot, handlerRelPath, logicalEvent, targetName string, ev *Event, payload []byte, stderr io.Writer) (exitCode int, handlerStdout, handlerStderr []byte, ran bool) {
	handlerPath, err := resolveHandlerPath(bundleRoot, handlerRelPath)
	if err != nil {
		fmt.Fprintf(stderr, "whippletree-hook: handler %s: refused (%v)\n", handlerRelPath, err)
		return 0, nil, nil, false
	}

	info, err := os.Stat(handlerPath)
	if err != nil {
		fmt.Fprintf(stderr, "whippletree-hook: handler %s: missing (%v) (ignored)\n", handlerRelPath, err)
		return 0, nil, nil, false
	}
	if info.IsDir() || !isExecutable(info) {
		fmt.Fprintf(stderr, "whippletree-hook: handler %s: not executable (ignored)\n", handlerRelPath)
		return 0, nil, nil, false
	}

	stopActive := ""
	if ev.StopHookActive != nil {
		stopActive = strconv.FormatBool(*ev.StopHookActive)
	}
	path := ""
	if len(ev.Paths) > 0 {
		path = ev.Paths[0]
	}

	ctx, cancel := context.WithTimeout(context.Background(), HandlerTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, handlerPath)
	cmd.Stdin = bytes.NewReader(payload)
	var stdoutBuf, captured bytes.Buffer
	cmd.Stdout = &stdoutBuf
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
		return 0, stdoutBuf.Bytes(), captured.Bytes(), true
	}

	// A timeout is fail-open, and deliberately not reported as an exit
	// code: the handler was killed, so whatever it exited with says
	// nothing about whether it meant to block.
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		fmt.Fprintf(stderr, "whippletree-hook: handler %s: timed out after %s (ignored)\n", handlerRelPath, HandlerTimeout)
		return 0, stdoutBuf.Bytes(), captured.Bytes(), false
	}

	var exitErr *exec.ExitError
	if errors.As(runErr, &exitErr) {
		return exitErr.ExitCode(), stdoutBuf.Bytes(), captured.Bytes(), true
	}

	fmt.Fprintf(stderr, "whippletree-hook: handler %s: %v (ignored)\n", handlerRelPath, runErr)
	return 0, nil, nil, false
}

// resolveHandlerPath turns a bundle-relative handler path into an
// absolute one, refusing anything that would address a file outside
// bundleRoot.
//
// This repeats the check contract.Validate already performs at build
// time, on purpose. The dispatcher's input is the vendored
// .whippletree/contract.json, which loadVendoredContract reads with a
// plain json.Unmarshal and does not re-validate; in a bundle published
// by a third party that file is untrusted, and a handler of
// "../../../../bin/sh" would otherwise resolve and execute. Symlinks are
// resolved before the containment test so a link inside the bundle
// cannot be used to step outside it either.
func resolveHandlerPath(bundleRoot, handlerRelPath string) (string, error) {
	if err := contract.ValidateBundleRelPath(handlerRelPath); err != nil {
		return "", err
	}

	root, err := filepath.Abs(bundleRoot)
	if err != nil {
		return "", fmt.Errorf("resolve bundle root: %w", err)
	}
	if real, err := filepath.EvalSymlinks(root); err == nil {
		root = real
	}

	full := filepath.Join(root, filepath.FromSlash(handlerRelPath))
	// A handler that does not exist keeps its joined path, so the
	// caller's os.Stat reports the missing-handler case as before.
	if real, err := filepath.EvalSymlinks(full); err == nil {
		full = real
	}

	if full != root && !strings.HasPrefix(full, root+string(os.PathSeparator)) {
		return "", fmt.Errorf("path %q escapes the bundle root", handlerRelPath)
	}
	return full, nil
}

// isExecutable reports whether info describes a file this process can
// try to execute.
//
// Windows needs its own answer: Go's os.Stat there synthesizes a mode
// from the read-only attribute and never sets an execute bit, so a POSIX
// mode test rejects every handler and no handler ever runs. Windows
// decides executability by extension and by the loader instead, so defer
// to exec and let a genuine failure surface through the fail-open path.
func isExecutable(info os.FileInfo) bool {
	if runtime.GOOS == "windows" {
		return true
	}
	return info.Mode()&0o111 != 0
}
