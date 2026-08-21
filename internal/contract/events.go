// SPDX-FileCopyrightText: 2026 Lars Tønder
// SPDX-License-Identifier: Apache-2.0

package contract

import "fmt"

// primitives is the exact event vocabulary a Requirement.Event may name
// directly, with no alias expansion.
var primitives = map[string]bool{
	"session-start":  true,
	"session-end":    true,
	"turn-end":       true,
	"tool-pre":       true,
	"tool-post":      true,
	"subagent-start": true,
	"subagent-stop":  true,
	"compact-pre":    true,
	"compact-post":   true,
}

// aliases expands a convenience event name to [primitive, toolClass].
var aliases = map[string][2]string{
	"file-read":  {"tool-post", "read"},
	"file-write": {"tool-post", "write"},
	"shell-exec": {"tool-post", "shell"},
}

// ResolveEvent resolves an event name to its underlying primitive and,
// for aliases, the associated tool class. Primitives resolve to
// themselves with an empty tool class. Anything else is an error.
func ResolveEvent(event string) (primitive string, toolClass string, err error) {
	if primitives[event] {
		return event, "", nil
	}
	if a, ok := aliases[event]; ok {
		return a[0], a[1], nil
	}
	return "", "", fmt.Errorf("unknown event %q", event)
}
