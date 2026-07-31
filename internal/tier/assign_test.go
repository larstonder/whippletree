package tier_test

import (
	"os"
	"strings"
	"testing"

	"github.com/larstonder/whippletree/internal/contract"
	"github.com/larstonder/whippletree/internal/target"
	"github.com/larstonder/whippletree/internal/tier"
)

func loadKbExample(t *testing.T) *contract.Contract {
	t.Helper()
	raw, err := os.ReadFile("testdata/kb-example.json")
	if err != nil {
		t.Fatal(err)
	}
	c, err := contract.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func requirementByID(t *testing.T, c *contract.Contract, id string) contract.Requirement {
	t.Helper()
	for _, req := range c.Requires {
		if req.ID == id {
			return req
		}
	}
	t.Fatalf("no requirement with id %q", id)
	return contract.Requirement{}
}

func TestAssignKbEngineMatrix(t *testing.T) {
	targets, err := target.LoadDir("../../targets")
	if err != nil {
		t.Fatal(err)
	}
	c := loadKbExample(t)

	cases := []struct {
		reqID      string
		targetName string
		want       contract.Tier
		wantAbsent bool
	}{
		{"stop-gate", "claude-code", contract.T1, false},
		{"stop-gate", "codex", contract.T1, false},
		{"stop-gate", "opencode", 0, true}, // opencode has no turn-end mapping
		{"session-start-signal", "claude-code", contract.T1, false},
		{"session-start-signal", "codex", contract.T1, false},
		{"session-start-signal", "opencode", contract.T1, false},
		{"file-read-signal", "claude-code", contract.T1, false},
		{"file-read-signal", "codex", contract.T2, false}, // via matcher alternation degradation
		{"file-read-signal", "opencode", contract.T1, false},
		{"bin-reachable", "claude-code", contract.T1, false},
		{"bin-reachable", "codex", contract.T1, false},
		{"bin-reachable", "opencode", contract.T1, false},
	}

	for _, tc := range cases {
		t.Run(tc.reqID+"/"+tc.targetName, func(t *testing.T) {
			td, ok := targets[tc.targetName]
			if !ok {
				t.Fatalf("no target def for %q", tc.targetName)
			}
			req := requirementByID(t, c, tc.reqID)

			got := tier.Assign(req, td)

			if got.Absent != tc.wantAbsent {
				t.Fatalf("Absent = %v, want %v (got %+v)", got.Absent, tc.wantAbsent, got)
			}
			if !tc.wantAbsent && got.Tier != tc.want {
				t.Fatalf("Tier = %v, want %v (got %+v)", got.Tier, tc.want, got)
			}

			if tc.reqID == "file-read-signal" && tc.targetName == "codex" {
				if !strings.Contains(got.Mechanism, "Bash|Edit|Write|apply_patch") {
					t.Errorf("Mechanism = %q, want it to contain the matcher alternation", got.Mechanism)
				}
				if got.Lossage == "" {
					t.Errorf("Lossage = empty, want non-empty for degraded observation-signal")
				}
			}

			if tc.reqID == "stop-gate" && tc.targetName == "opencode" {
				if !strings.Contains(got.Mechanism, "no native mapping") {
					t.Errorf("Mechanism = %q, want it to contain %q", got.Mechanism, "no native mapping")
				}
			}
			if tc.reqID == "session-start-signal" && tc.targetName == "opencode" {
				if !strings.Contains(got.Mechanism, "event:session.created") {
					t.Errorf("Mechanism = %q, want it to contain %q", got.Mechanism, "event:session.created")
				}
			}
			if tc.reqID == "file-read-signal" && tc.targetName == "opencode" {
				if !strings.Contains(got.Mechanism, "read") {
					t.Errorf("Mechanism = %q, want it to contain the native read matcher", got.Mechanism)
				}
			}
			if tc.reqID == "bin-reachable" && tc.targetName == "opencode" {
				if got.Mechanism != "installer-resolved absolute path" {
					t.Errorf("Mechanism = %q, want %q", got.Mechanism, "installer-resolved absolute path")
				}
			}
		})
	}
}

func TestAssignBlockingGateAbsentWithoutLoopGuardField(t *testing.T) {
	targets, err := target.LoadDir("../../targets")
	if err != nil {
		t.Fatal(err)
	}
	c := loadKbExample(t)

	base, ok := targets["codex"]
	if !ok {
		t.Fatal("no target def for codex")
	}

	// Blank turn-end's loop-guard field so a LoopGuardRequired gate
	// can't be satisfied natively.
	modified := *base
	events := make(map[string]target.EventMapping, len(base.Events))
	for k, v := range base.Events {
		events[k] = v
	}
	turnEnd := events["turn-end"]
	turnEnd.LoopGuardField = ""
	events["turn-end"] = turnEnd
	modified.Events = events

	req := requirementByID(t, c, "stop-gate")
	if !req.LoopGuardRequired {
		t.Fatal("fixture expectation broken: stop-gate must have LoopGuardRequired=true")
	}

	got := tier.Assign(req, &modified)

	if !got.Absent {
		t.Fatalf("Absent = false, want true (got %+v)", got)
	}
	if !strings.Contains(got.Mechanism, "no loop-guard field on Stop") {
		t.Errorf("Mechanism = %q, want it to name the missing loop-guard field on the native event", got.Mechanism)
	}
}

// TestAssignAbsentReasonsPerBranch covers every Absent branch
// tier.Assign can take, verifying each carries the specific reason the
// caller (build's error path, preflight's Render) needs.
func TestAssignAbsentReasonsPerBranch(t *testing.T) {
	// td has a native mapping for session-start (non-blocking) and for
	// turn-end (blocking, but with no loop-guard field). It deliberately
	// has no mapping for tool-post, so requirements resolving to that
	// primitive hit the "no native mapping" branch.
	td := &target.Def{
		Name: "stub",
		Events: map[string]target.EventMapping{
			"session-start": {Native: "SessionStart", Blocking: false},
			"turn-end":      {Native: "Stop", Blocking: true},
		},
		ToolClassMap: map[string]*string{},
		Degradations: map[string]target.Degradation{},
		Capabilities: map[string]bool{},
	}

	// tdWithToolPost additionally maps tool-post, so an observation-signal
	// requirement can reach past the "no native mapping" branch and hit
	// the "no <toolClass> tool and no degradation declared" branch.
	tdWithToolPost := *td
	events := make(map[string]target.EventMapping, len(td.Events)+1)
	for k, v := range td.Events {
		events[k] = v
	}
	events["tool-post"] = target.EventMapping{Native: "PostToolUse", Blocking: false}
	tdWithToolPost.Events = events

	falseVal := false

	cases := []struct {
		name       string
		req        contract.Requirement
		td         *target.Def
		wantSubstr string
	}{
		{
			name:       "unknown kind",
			req:        contract.Requirement{Kind: "mystery", HardRequired: &falseVal},
			td:         td,
			wantSubstr: "unknown kind",
		},
		{
			name:       "unresolvable event on lifecycle-signal",
			req:        contract.Requirement{Kind: "lifecycle-signal", Event: "turn-start", HardRequired: &falseVal},
			td:         td,
			wantSubstr: "unresolvable event turn-start",
		},
		{
			name:       "unresolvable event on blocking-gate",
			req:        contract.Requirement{Kind: "blocking-gate", Event: "turn-start", HardRequired: &falseVal},
			td:         td,
			wantSubstr: "unresolvable event turn-start",
		},
		{
			name:       "unresolvable event on observation-signal",
			req:        contract.Requirement{Kind: "observation-signal", Event: "turn-start", HardRequired: &falseVal},
			td:         td,
			wantSubstr: "unresolvable event turn-start",
		},
		{
			name:       "no native mapping on lifecycle-signal",
			req:        contract.Requirement{Kind: "lifecycle-signal", Event: "tool-post", HardRequired: &falseVal},
			td:         td,
			wantSubstr: "no native mapping for tool-post on this target",
		},
		{
			name:       "no native mapping on blocking-gate",
			req:        contract.Requirement{Kind: "blocking-gate", Event: "tool-post", HardRequired: &falseVal},
			td:         td,
			wantSubstr: "no native mapping for tool-post on this target",
		},
		{
			name:       "no native mapping on observation-signal",
			req:        contract.Requirement{Kind: "observation-signal", Event: "file-read", HardRequired: &falseVal},
			td:         td,
			wantSubstr: "no native mapping for tool-post on this target",
		},
		{
			name:       "non-blocking event for blocking-gate",
			req:        contract.Requirement{Kind: "blocking-gate", Event: "session-start", HardRequired: &falseVal},
			td:         td,
			wantSubstr: "SessionStart is not blocking on this target",
		},
		{
			name:       "observation with no class and no degradation",
			req:        contract.Requirement{Kind: "observation-signal", Event: "file-read", HardRequired: &falseVal},
			td:         &tdWithToolPost,
			wantSubstr: "no read tool and no degradation declared",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := tier.Assign(tc.req, tc.td)
			if !got.Absent {
				t.Fatalf("Absent = false, want true (got %+v)", got)
			}
			if !strings.Contains(got.Mechanism, tc.wantSubstr) {
				t.Errorf("Mechanism = %q, want it to contain %q", got.Mechanism, tc.wantSubstr)
			}
		})
	}
}

// TestAssignExecutablePathAbsentReportsNoBundleChannel covers the
// executable-path Absent branch: a target without a bundle channel has
// no other class-1 mechanism to fall back to, so Assign reports that
// specific cause rather than a generic one.
func TestAssignExecutablePathAbsentReportsNoBundleChannel(t *testing.T) {
	falseVal := false
	req := contract.Requirement{Kind: "executable-path", HardRequired: &falseVal}
	td := &target.Def{Name: "stub", Capabilities: map[string]bool{}}

	got := tier.Assign(req, td)
	if !got.Absent {
		t.Fatalf("Absent = false, want true (got %+v)", got)
	}
	if got.Mechanism != "no bundle channel on this target" {
		t.Errorf("Mechanism = %q, want %q", got.Mechanism, "no bundle channel on this target")
	}
}
