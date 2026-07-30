package contract

import (
	"strings"
	"testing"
)

func boolPtr(b bool) *bool { return &b }

func wrapRequirement(reqJSON string) []byte {
	return []byte(`{"name":"x","extensions":{"dev.adaptersdk.v1":{"contractVersion":"1.0.0","requires":[` + reqJSON + `]}}}`)
}

func TestValidateRejects(t *testing.T) {
	cases := []struct{ name, patch, wantErr string }{
		{"missing hardRequired", `{"id":"a","kind":"lifecycle-signal","event":"session-start","minTier":"T2"}`, "hardRequired"},
		{"unknown kind", `{"id":"a","kind":"magic","event":"session-start","minTier":"T2","hardRequired":false}`, "kind"},
		{"unknown event", `{"id":"a","kind":"lifecycle-signal","event":"turn-start","minTier":"T2","hardRequired":false}`, "event"},
		{"bad tier", `{"id":"a","kind":"lifecycle-signal","event":"session-start","minTier":"T9","hardRequired":false}`, "tier"},
		{"per-call obs at minTier T3", `{"id":"a","kind":"observation-signal","event":"file-read","minTier":"T3","hardRequired":false,"handler":"./h.sh"}`, "T3"},
		{"handler missing", `{"id":"a","kind":"blocking-gate","event":"turn-end","minTier":"T1","hardRequired":true}`, "handler"},
		{"path missing on executable-path", `{"id":"a","kind":"executable-path","minTier":"T1","hardRequired":true}`, "path"},
		{"duplicate id", "", "duplicate"}, // built directly below
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var errText string

			if tc.name == "duplicate id" {
				c := &Contract{
					ContractVersion: "1.0.0",
					Requires: []Requirement{
						{ID: "a", Kind: "lifecycle-signal", Event: "session-start", MinTier: T2, HardRequired: boolPtr(false), Handler: "./h.sh"},
						{ID: "a", Kind: "lifecycle-signal", Event: "session-start", MinTier: T2, HardRequired: boolPtr(false), Handler: "./h.sh"},
					},
				}
				if err := Validate(c); err != nil {
					errText = err.Error()
				}
			} else {
				c, parseErr := Parse(wrapRequirement(tc.patch))
				if parseErr != nil {
					errText = parseErr.Error()
				} else if err := Validate(c); err != nil {
					errText = err.Error()
				}
			}

			if errText == "" {
				t.Fatalf("want error containing %q, got none", tc.wantErr)
			}
			if !strings.Contains(errText, tc.wantErr) {
				t.Fatalf("error %q does not contain %q", errText, tc.wantErr)
			}
		})
	}
}

func TestValidateAcceptsKbExample(t *testing.T) {
	c, err := Parse([]byte(kbExample))
	if err != nil {
		t.Fatal(err)
	}
	if err := Validate(c); err != nil {
		t.Fatal(err)
	}
}

func TestResolveEvent(t *testing.T) {
	p, tc, err := ResolveEvent("file-read")
	if err != nil || p != "tool-post" || tc != "read" {
		t.Fatalf("got %q %q %v", p, tc, err)
	}
	if _, _, err := ResolveEvent("banana"); err == nil {
		t.Fatal("want error")
	}
}
