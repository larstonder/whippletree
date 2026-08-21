// SPDX-FileCopyrightText: 2026 Lars Tønder
// SPDX-License-Identifier: Apache-2.0

package compile

import (
	"bytes"
	"encoding/json"
	"strings"
)

// primitiveOrder is the fixed canonical ordering used to make hooks-file
// emission deterministic. Go map iteration is randomized and a plain
// alphabetical sort of native event names would not match the order
// authors expect (e.g. SessionStart before PostToolUse), so instead we
// order by the whippletree primitive each native event maps from.
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

type hookCommand struct {
	Type    string `json:"type"`
	Command string `json:"command"`
}

// hookEntry is one element of a native event's hooks array. Only
// observation-signal requirements ever set Matcher.
type hookEntry struct {
	Matcher string        `json:"matcher,omitempty"`
	Hooks   []hookCommand `json:"hooks"`
}

// eventGroup is every hookEntry destined for one native event name.
type eventGroup struct {
	native  string
	entries []hookEntry
}

// hooksFile is the top-level {"hooks": {...}} document written to
// hooks/<target>.json.
type hooksFile struct {
	byPrimitive map[string]*eventGroup
	seen        map[string]bool
}

func newHooksFile() *hooksFile {
	return &hooksFile{byPrimitive: make(map[string]*eventGroup), seen: make(map[string]bool)}
}

// add appends entry to primitive's group, dropping an exact repeat of
// (primitive, matcher, commands): two requirements can resolve to the
// same entry, and emitting both runs the handler twice per firing.
func (h *hooksFile) add(primitive, native string, entry hookEntry) {
	key := dedupeKey(primitive, entry)
	if h.seen[key] {
		return
	}
	h.seen[key] = true

	g, ok := h.byPrimitive[primitive]
	if !ok {
		g = &eventGroup{native: native}
		h.byPrimitive[primitive] = g
	}
	g.entries = append(g.entries, entry)
}

func dedupeKey(primitive string, entry hookEntry) string {
	var b strings.Builder
	b.WriteString(primitive)
	b.WriteByte(0)
	b.WriteString(entry.Matcher)
	for _, c := range entry.Hooks {
		b.WriteByte(0)
		b.WriteString(c.Type)
		b.WriteByte(0)
		b.WriteString(c.Command)
	}
	return b.String()
}

// MarshalJSON emits the top-level "hooks" keys in primitiveOrder.
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
