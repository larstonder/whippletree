package compile

import (
	"bytes"
	"encoding/json"
)

// primitiveOrder is the fixed canonical ordering used to make hooks-file
// emission deterministic. Go map iteration is randomized and a plain
// alphabetical sort of native event names would not match the order
// authors expect (e.g. SessionStart before PostToolUse), so instead we
// order by the adapter-sdk primitive each native event maps from.
var primitiveOrder = []string{
	"session-start",
	"session-end",
	"tool-pre",
	"tool-post",
	"turn-end",
	"subagent-start",
	"subagent-stop",
	"compact-pre",
	"compact-post",
}

// hookCommand is a single {"type": "command", "command": "..."} entry.
type hookCommand struct {
	Type    string `json:"type"`
	Command string `json:"command"`
}

// hookEntry is one element of a native event's hooks array: an optional
// matcher (observation-signal only) plus the commands to run.
type hookEntry struct {
	Matcher string        `json:"matcher,omitempty"`
	Hooks   []hookCommand `json:"hooks"`
}

// eventGroup collects every hookEntry destined for a single native event
// name, along with the primitive it was reached through (used only for
// ordering during marshaling).
type eventGroup struct {
	native  string
	entries []hookEntry
}

// hooksFile is the top-level {"hooks": {...}} document written to
// hooks/<target>.json. It marshals its native-event keys in a fixed
// deterministic order (see primitiveOrder) rather than Go's randomized
// map order, so golden-file comparisons are byte-stable.
type hooksFile struct {
	byPrimitive map[string]*eventGroup
}

func newHooksFile() *hooksFile {
	return &hooksFile{byPrimitive: make(map[string]*eventGroup)}
}

// add appends entry to the group for primitive, creating the group (with
// native name native) if this is the first entry seen for it.
func (h *hooksFile) add(primitive, native string, entry hookEntry) {
	g, ok := h.byPrimitive[primitive]
	if !ok {
		g = &eventGroup{native: native}
		h.byPrimitive[primitive] = g
	}
	g.entries = append(g.entries, entry)
}

// MarshalJSON implements a fixed key order for the top-level "hooks" map,
// per primitiveOrder, so repeated builds of the same contract produce
// byte-identical output.
func (h *hooksFile) MarshalJSON() ([]byte, error) {
	var buf bytes.Buffer
	buf.WriteString(`{"hooks":{`)

	first := true
	for _, primitive := range primitiveOrder {
		g, ok := h.byPrimitive[primitive]
		if !ok {
			continue
		}
		if !first {
			buf.WriteByte(',')
		}
		first = false

		keyJSON, err := json.Marshal(g.native)
		if err != nil {
			return nil, err
		}
		buf.Write(keyJSON)
		buf.WriteByte(':')

		valJSON, err := json.Marshal(g.entries)
		if err != nil {
			return nil, err
		}
		buf.Write(valJSON)
	}

	buf.WriteString("}}")
	return buf.Bytes(), nil
}
