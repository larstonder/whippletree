// Package dispatch normalizes a target's raw hook stdin payload into the
// adapter-sdk's canonical Event shape, so downstream handlers work against
// one representation regardless of which target produced the payload.
package dispatch

import (
	"encoding/json"
	"fmt"
	"regexp"

	"github.com/larstonder/adapter-sdk/internal/contract"
	"github.com/larstonder/adapter-sdk/internal/target"
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
// string. This is a documented lossy heuristic (see research §3.5): it has
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

// Normalize decodes stdin (a target's raw hook payload) into the canonical
// Event shape. logicalEvent may be an alias (e.g. "file-read") or a
// primitive (e.g. "turn-end"); td supplies the target-specific mapping
// needed to read the loop-guard field on turn-end payloads.
func Normalize(logicalEvent string, td *target.Def, stdin []byte) (*Event, error) {
	primitive, toolClass, err := contract.ResolveEvent(logicalEvent)
	if err != nil {
		return nil, fmt.Errorf("normalize: %w", err)
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
