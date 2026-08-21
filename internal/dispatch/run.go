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

// HandlerTimeout bounds a single handler run; outliving it is fail-open,
// because a wedged handler must never wedge the harness. 30s matches
// Copilot CLI's own hook timeoutSec default, which also fails open.
const HandlerTimeout = 30 * time.Second

// handlerTimeout is the value actually used, so tests can shorten it.
var handlerTimeout = HandlerTimeout

// Run executes every handler whose requirement Event matches
// logicalEvent verbatim, in contract order, against the normalized event
// JSON on stdin.
//
// This is the exit-2 refusal dialect: exit 2 is a hard block and returns
// immediately without running later handlers ("first block wins"), while
// any other non-zero exit, or a missing or non-executable handler, is
// logged and ignored. Handler stdout is forwarded verbatim whenever the
// handler produced a verdict, whatever that verdict was; what it means
// is the harness's decision. See docs/AUTHORING.md.
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

		// A failure here has nowhere left to be reported to.
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

// loadVendoredContract reads bundleRoot's vendored contract.json. It is
// not re-validated here; see resolveHandlerPath for why that matters.
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

// runHandler runs one handler with payload on stdin and the ADAPTER_*
// env vars set (the table is in docs/AUTHORING.md). ran is false when
// the handler could not be run at all, in which case it has already
// explained itself on stderr and the caller should continue.
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

	ctx, cancel := context.WithTimeout(context.Background(), handlerTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, handlerPath)
	// CommandContext kills only the handler itself. Any grandchild it
	// spawned inherits the stdout pipe, and Run waits for that pipe to
	// close, so without WaitDelay a handler running `sleep 600 &` would
	// hang the harness for ten minutes despite the timeout firing.
	cmd.WaitDelay = time.Second
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

	// A real exit code outranks the deadline. WaitDelay makes it ordinary
	// for Run to return after the deadline even though the handler
	// answered long before: a child holding the stdout pipe keeps Wait
	// blocked past it. Reporting that as a timeout would drop a genuine
	// exit-2 block. ExitCode is -1 when a signal killed the handler,
	// which is the case with no verdict to honour.
	var exitErr *exec.ExitError
	if errors.As(runErr, &exitErr) && exitErr.ExitCode() >= 0 {
		return exitErr.ExitCode(), stdoutBuf.Bytes(), captured.Bytes(), true
	}

	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		// Diagnostics still help, so stderr is forwarded. Stdout is not:
		// it is a payload channel, and half a payload is worse than none.
		if captured.Len() > 0 {
			_, _ = stderr.Write(captured.Bytes())
		}
		fmt.Fprintf(stderr, "whippletree-hook: handler %s: timed out after %s (ignored)\n", handlerRelPath, handlerTimeout)
		return 0, nil, nil, false
	}

	fmt.Fprintf(stderr, "whippletree-hook: handler %s: %v (ignored)\n", handlerRelPath, runErr)
	return 0, nil, nil, false
}

// resolveHandlerPath resolves a bundle-relative handler to an absolute
// path, refusing anything outside bundleRoot.
//
// This repeats contract.Validate's build-time check on purpose: the
// vendored contract.json is read here without re-validation, so in a
// third-party bundle it is untrusted and a handler of
// "../../../../bin/sh" would otherwise execute. Symlinks are resolved
// first so a link inside the bundle cannot step outside it either.
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

// isExecutable reports whether info is worth handing to exec.
//
// Go's os.Stat on Windows synthesizes a mode from the read-only
// attribute and never sets an execute bit, so a POSIX mode test there
// rejects every handler. Defer to the loader instead.
func isExecutable(info os.FileInfo) bool {
	if runtime.GOOS == "windows" {
		return true
	}
	return info.Mode()&0o111 != 0
}
