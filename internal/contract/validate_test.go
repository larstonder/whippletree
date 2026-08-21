// SPDX-FileCopyrightText: 2026 Lars Tønder
// SPDX-License-Identifier: Apache-2.0

package contract

import (
	"strings"
	"testing"
)

func boolPtr(b bool) *bool { return &b }

func wrapRequirement(reqJSON string) []byte {
	return []byte(`{"name":"x","extensions":{"dev.whippletree.v1":{"contractVersion":"1.0.0","requires":[` + reqJSON + `]}}}`)
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
		{"handler escapes the bundle", `{"id":"a","kind":"blocking-gate","event":"turn-end","minTier":"T1","hardRequired":true,"handler":"../../../../bin/sh"}`, "escapes the bundle root"},
		{"handler is absolute", `{"id":"a","kind":"blocking-gate","event":"turn-end","minTier":"T1","hardRequired":true,"handler":"/bin/sh"}`, "must be relative"},
		{"executable-path escapes the bundle", `{"id":"a","kind":"executable-path","path":"../../../../etc/passwd","minTier":"T1","hardRequired":true}`, "escapes the bundle root"},
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

func TestValidateSkillKind(t *testing.T) {
	yes := true
	no := false

	valid := &Contract{ContractVersion: SupportedContractVersion, Requires: []Requirement{
		{ID: "s", Kind: "skill", Path: "./skills/s", MinTierRaw: "T1", MinTier: T1, HardRequired: &no},
	}}
	if err := Validate(valid); err != nil {
		t.Fatalf("valid skill requirement rejected: %v", err)
	}

	cases := []struct {
		name string
		req  Requirement
		want string
	}{
		{"missing path", Requirement{ID: "s", Kind: "skill", MinTierRaw: "T1", MinTier: T1, HardRequired: &no}, "path is required for skill"},
		{"path outside skills", Requirement{ID: "s", Kind: "skill", Path: "./content/s", MinTierRaw: "T1", MinTier: T1, HardRequired: &no}, "must have the form ./skills/<dir>"},
		{"nested path", Requirement{ID: "s", Kind: "skill", Path: "./skills/a/b", MinTierRaw: "T1", MinTier: T1, HardRequired: &no}, "must have the form ./skills/<dir>"},
		{"dot path", Requirement{ID: "s", Kind: "skill", Path: "./skills/.", MinTierRaw: "T1", MinTier: T1, HardRequired: &no}, "must have the form ./skills/<dir>"},
		{"dotdot path", Requirement{ID: "s", Kind: "skill", Path: "./skills/..", MinTierRaw: "T1", MinTier: T1, HardRequired: &no}, "must have the form ./skills/<dir>"},
		{"event set", Requirement{ID: "s", Kind: "skill", Path: "./skills/s", Event: "session-start", MinTierRaw: "T1", MinTier: T1, HardRequired: &no}, "event must be empty for skill"},
		{"handler set", Requirement{ID: "s", Kind: "skill", Path: "./skills/s", Handler: "./h.sh", MinTierRaw: "T1", MinTier: T1, HardRequired: &no}, "handler must be empty for skill"},
		{"loop guard set", Requirement{ID: "s", Kind: "skill", Path: "./skills/s", LoopGuardRequired: true, MinTierRaw: "T1", MinTier: T1, HardRequired: &no}, "loopGuardRequired must be false for skill"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := Validate(&Contract{ContractVersion: SupportedContractVersion, Requires: []Requirement{tc.req}})
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("want error containing %q, got %v", tc.want, err)
			}
		})
	}
	_ = yes
}

func TestValidateFallbackSkill(t *testing.T) {
	no := false
	skill := Requirement{ID: "cap", Kind: "skill", Path: "./skills/cap", MinTierRaw: "T3", MinTier: T3, HardRequired: &no}
	gate := func(event, fallback string) Requirement {
		return Requirement{ID: "g", Kind: "blocking-gate", Event: event, MinTierRaw: "T3", MinTier: T3,
			HardRequired: &no, Handler: "./h.sh", FallbackSkill: fallback}
	}
	signal := func(event, fallback string) Requirement {
		return Requirement{ID: "l", Kind: "lifecycle-signal", Event: event, MinTierRaw: "T3", MinTier: T3,
			HardRequired: &no, Handler: "./h.sh", FallbackSkill: fallback}
	}

	if err := Validate(&Contract{ContractVersion: SupportedContractVersion, Requires: []Requirement{skill, gate("turn-end", "cap")}}); err != nil {
		t.Fatalf("gate turn-end fallback rejected: %v", err)
	}
	if err := Validate(&Contract{ContractVersion: SupportedContractVersion, Requires: []Requirement{skill, signal("session-start", "cap")}}); err != nil {
		t.Fatalf("signal session-start fallback rejected: %v", err)
	}

	cases := []struct {
		name string
		reqs []Requirement
		want string
	}{
		{"gate wrong event", []Requirement{skill, gate("session-end", "cap")}, "fallbackSkill on blocking-gate requires event turn-end"},
		{"signal wrong event", []Requirement{skill, signal("session-end", "cap")}, "fallbackSkill on lifecycle-signal requires event session-start"},
		{"observation-signal", []Requirement{skill, {ID: "o", Kind: "observation-signal", Event: "file-read",
			MinTierRaw: "T4", MinTier: T4, HardRequired: &no, Handler: "./h.sh", FallbackSkill: "cap"}},
			"fallbackSkill is not allowed on observation-signal"},
		{"executable-path", []Requirement{skill, {ID: "x", Kind: "executable-path", Path: "./bin/x",
			MinTierRaw: "T1", MinTier: T1, HardRequired: &no, FallbackSkill: "cap"}},
			"fallbackSkill is not allowed on executable-path"},
		{"skill itself", []Requirement{{ID: "s2", Kind: "skill", Path: "./skills/s2", MinTierRaw: "T1",
			MinTier: T1, HardRequired: &no, FallbackSkill: "cap"}, skill},
			"fallbackSkill is not allowed on skill"},
		{"dangling reference", []Requirement{gate("turn-end", "nope")},
			`fallbackSkill "nope" does not name a skill requirement in this contract`},
		{"reference to non-skill", []Requirement{signal("session-start", "g2"),
			{ID: "g2", Kind: "blocking-gate", Event: "turn-end", MinTierRaw: "T1", MinTier: T1,
				HardRequired: &no, Handler: "./h.sh"}},
			`fallbackSkill "g2" does not name a skill requirement in this contract`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := Validate(&Contract{ContractVersion: SupportedContractVersion, Requires: tc.reqs})
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("want error containing %q, got %v", tc.want, err)
			}
		})
	}
}

func TestValidateContractVersion(t *testing.T) {
	req := Requirement{ID: "a", Kind: "lifecycle-signal", Event: "session-start",
		MinTierRaw: "T2", MinTier: T2, HardRequired: boolPtr(false), Handler: "./h.sh"}

	cases := []struct{ name, version, wantErr string }{
		{"supported exactly", SupportedContractVersion, ""},
		{"older minor is readable", "1.0.0", ""},
		{"missing", "", "contractVersion is required"},
		{"unparseable", "one point oh", "invalid version"},
		{"newer major", "2.0.0", "major versions must match"},
		{"older major", "0.9.0", "major versions must match"},
		{"newer minor is refused", "1.99.0", "newer than this whippletree supports"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := Validate(&Contract{ContractVersion: tc.version, Requires: []Requirement{req}})
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("Validate with contractVersion %q = %v, want nil", tc.version, err)
				}
				return
			}
			if err == nil {
				t.Fatalf("Validate with contractVersion %q = nil, want an error", tc.version)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("error = %q, want it to mention %q", err, tc.wantErr)
			}
		})
	}
}
