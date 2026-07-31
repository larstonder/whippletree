// Package dispatch normalizes a target's raw hook stdin payload into the
// whippletree's canonical Event shape, so downstream handlers work against
// one representation regardless of which target produced the payload.
package dispatch

import (
	"encoding/json"
	"fmt"
	"regexp"

	"github.com/larstonder/whippletree/internal/contract"
	"github.com/larstonder/whippletree/internal/target"
)

// Event is the canonical, target-agnostic representation of a single hook
// invocation, built by Normalize from a target's raw stdin payload.
type Event struct {
	Event          string          `json:"event"`
	Alias          string          `json:"alias,omitempty"`
	ToolClass      string          `json:"toolClass,omitempty"`
	Command        string          `json:"command,omitempty"`
	Paths          []string        `json:"paths,omitempty"`
	StopHookActive *bool           `json:"stopHookActive,omitempty"`
	TranscriptPath string          `json:"transcriptPath,omitempty"`
	CWD            string          `json:"cwd,omitempty"`
	Raw            json.RawMessage `json:"raw"`
}

// pathLikeRE extracts filesystem-path-shaped tokens out of a shell command
// string. This is a deliberately lossy heuristic: it has
// no understanding of shell syntax, so it can double-count a path that
// appears more than once in the command, and it can miss paths embedded in
// pipelines, heredocs, or scripts. Deliberately not deduped; that decision
// belongs to the handler.
var pathLikeRE = regexp.MustCompile(`[A-Za-z0-9_./~-]+\.[A-Za-z0-9]{1,8}`)

// payload is the subset of hook stdin fields Normalize reads, common across
// all class-1 targets.
type payload struct {
	TranscriptPath string `json:"transcript_path"`
	CWD            string `json:"cwd"`
	ToolInput      struct {
		FilePath string `json:"file_path"`
		Command  string `json:"command"`
	} `json:"tool_input"`
}

// opencodeArgs is the subset of an opencode tool call's args Normalize
// reads across tool kinds.
type opencodeArgs struct {
	FilePath string `json:"filePath"`
}

// opencodeEnvelope is the shape whippletree's compiled ts-plugin shim wraps
// opencode's raw hook arguments in before piping them to the dispatcher.
type opencodeEnvelope struct {
	Hook      string `json:"hook"`
	Directory string `json:"directory"`
	Input     struct {
		Args opencodeArgs `json:"args"`
	} `json:"input"`
	Output struct {
		Args opencodeArgs `json:"args"`
	} `json:"output"`
}

// Normalize decodes stdin (a target's raw hook payload) into the canonical
// Event shape. logicalEvent may be an alias (e.g. "file-read") or a
// primitive (e.g. "turn-end"); td supplies the backend that selects the
// payload dialect and the target-specific mapping needed to read the
// loop-guard field on turn-end payloads.
func Normalize(logicalEvent string, td *target.Def, stdin []byte) (*Event, error) {
	primitive, toolClass, err := contract.ResolveEvent(logicalEvent)
	if err != nil {
		return nil, fmt.Errorf("normalize: %w", err)
	}

	// The target's backend, not the payload's shape, selects the
	// dialect. Payload shape is not a reliable discriminator: the
	// envelope's top-level "source" field also appears on Claude Code's
	// SessionStart input, where it carries "startup", "resume", "clear"
	// or "compact".
	if td.Backend == target.BackendTSPlugin {
		return normalizeOpencode(logicalEvent, primitive, toolClass, stdin)
	}

	var p payload
	if err := json.Unmarshal(stdin, &p); err != nil {
		return nil, fmt.Errorf("normalize: decode stdin: %w", err)
	}

	ev := &Event{
		Event:          primitive,
		ToolClass:      toolClass,
		TranscriptPath: p.TranscriptPath,
		CWD:            p.CWD,
		Raw:            json.RawMessage(stdin),
	}
	if primitive != logicalEvent {
		ev.Alias = logicalEvent
	}

	switch primitive {
	case "tool-post":
		switch {
		case p.ToolInput.FilePath != "":
			ev.Paths = []string{p.ToolInput.FilePath}
		case p.ToolInput.Command != "":
			ev.Command = p.ToolInput.Command
			ev.Paths = pathLikeRE.FindAllString(p.ToolInput.Command, -1)
		}
	case "turn-end":
		mapping, ok := td.Events["turn-end"]
		if ok && mapping.LoopGuardField != "" {
			var raw map[string]json.RawMessage
			if err := json.Unmarshal(stdin, &raw); err != nil {
				return nil, fmt.Errorf("normalize: decode stdin: %w", err)
			}
			if fieldRaw, present := raw[mapping.LoopGuardField]; present {
				var v bool
				if err := json.Unmarshal(fieldRaw, &v); err != nil {
					return nil, fmt.Errorf("normalize: decode loop guard field %q: %w", mapping.LoopGuardField, err)
				}
				ev.StopHookActive = &v
			}
		}
	}

	return ev, nil
}

// normalizeOpencode decodes an envelope-shaped stdin payload, the shape the
// opencode ts-plugin shim wraps its raw hook arguments in. opencode has no
// loop guard: a blocking tool-pre throw fails just that tool call and the
// agent loop continues, so StopHookActive is always nil here.
func normalizeOpencode(logicalEvent, primitive, toolClass string, stdin []byte) (*Event, error) {
	var env opencodeEnvelope
	if err := json.Unmarshal(stdin, &env); err != nil {
		return nil, fmt.Errorf("normalize: decode opencode envelope: %w", err)
	}

	ev := &Event{
		Event:     primitive,
		ToolClass: toolClass,
		CWD:       env.Directory,
		Raw:       json.RawMessage(stdin),
	}
	if primitive != logicalEvent {
		ev.Alias = logicalEvent
	}

	// opencode's before hook exposes args on output, not input: output is
	// the mutable slot a plugin can rewrite before the tool runs. The
	// after hook has already merged the (possibly rewritten) args onto
	// input.
	switch env.Hook {
	case "tool.execute.after":
		if fp := env.Input.Args.FilePath; fp != "" {
			ev.Paths = []string{fp}
		}
	case "tool.execute.before":
		if fp := env.Output.Args.FilePath; fp != "" {
			ev.Paths = []string{fp}
		}
	}

	return ev, nil
}
