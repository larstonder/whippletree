package tier_test

import (
	"os"
	"strings"
	"testing"

	"github.com/larstonder/adapter-sdk/internal/contract"
	"github.com/larstonder/adapter-sdk/internal/target"
	"github.com/larstonder/adapter-sdk/internal/tier"
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
		{"session-start-signal", "claude-code", contract.T1, false},
		{"session-start-signal", "codex", contract.T1, false},
		{"file-read-signal", "claude-code", contract.T1, false},
		{"file-read-signal", "codex", contract.T2, false}, // via matcher alternation degradation
		{"bin-reachable", "claude-code", contract.T1, false},
		{"bin-reachable", "codex", contract.T1, false},
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

	// Copy the codex Def and blank the turn-end mapping's LoopGuardField so
	// a LoopGuardRequired blocking-gate can no longer be satisfied natively.
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
}
