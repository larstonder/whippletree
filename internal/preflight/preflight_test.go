package preflight_test

import (
	"os"
	"strings"
	"testing"

	"whippletree.dev/internal/contract"
	"whippletree.dev/internal/preflight"
	"whippletree.dev/internal/target"
	"whippletree.dev/internal/tier"
)

func loadKbExample(t *testing.T) *contract.Contract {
	t.Helper()
	raw, err := os.ReadFile("../tier/testdata/kb-example.json")
	if err != nil {
		t.Fatal(err)
	}
	c, err := contract.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func loadTargets(t *testing.T) map[string]*target.Def {
	t.Helper()
	defs, err := target.LoadDir("../../targets")
	if err != nil {
		t.Fatal(err)
	}
	return defs
}

func lineByID(t *testing.T, r *preflight.Report, id string) preflight.Line {
	t.Helper()
	for _, l := range r.Lines {
		if l.ReqID == id {
			return l
		}
	}
	t.Fatalf("no line with reqID %q", id)
	return preflight.Line{}
}

func TestPreflightSatisfyAndDegrade(t *testing.T) {
	c := loadKbExample(t)
	targets := loadTargets(t)
	codex := targets["codex"]
	if codex == nil {
		t.Fatal("no codex target loaded")
	}

	report, err := preflight.Check(c, codex, preflight.Version("0.146.0"), false)
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if report.Refused {
		t.Fatalf("expected report not refused, got Refused=true; lines=%+v", report.Lines)
	}

	for _, id := range []string{"stop-gate", "session-start-signal", "file-read-signal", "bin-reachable"} {
		line := lineByID(t, report, id)
		if line.Verdict != "SATISFY" {
			t.Errorf("requirement %q: want verdict SATISFY, got %s (detail=%q)", id, line.Verdict, line.Detail)
		}
	}

	stopGate := lineByID(t, report, "stop-gate")
	if stopGate.Want != contract.T1 || stopGate.Got != contract.T1 {
		t.Errorf("stop-gate: want T1/T1, got want=%s got=%s", stopGate.Want, stopGate.Got)
	}

	fileRead := lineByID(t, report, "file-read-signal")
	if fileRead.Want != contract.T4 || fileRead.Got != contract.T2 {
		t.Errorf("file-read-signal: want T4/T2, got want=%s got=%s", fileRead.Want, fileRead.Got)
	}

	rendered := report.Render("codex", preflight.Version("0.146.0"))
	if !strings.Contains(rendered, "stop-gate") {
		t.Errorf("Render output missing %q:\n%s", "stop-gate", rendered)
	}
	if !strings.Contains(rendered, "T1") {
		t.Errorf("Render output missing %q:\n%s", "T1", rendered)
	}
}

func TestPreflightRefusesHardRequired(t *testing.T) {
	c := loadKbExample(t)
	targets := loadTargets(t)
	codex := *targets["codex"] // shallow copy so we can mutate Events safely

	events := make(map[string]target.EventMapping, len(targets["codex"].Events))
	for k, v := range targets["codex"].Events {
		events[k] = v
	}
	delete(events, "turn-end")
	codex.Events = events

	report, err := preflight.Check(c, &codex, preflight.Version("0.146.0"), false)
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if !report.Refused {
		t.Fatalf("expected Report.Refused true, got false; lines=%+v", report.Lines)
	}

	stopGate := lineByID(t, report, "stop-gate")
	if stopGate.Verdict != "REFUSE" {
		t.Errorf("stop-gate: want verdict REFUSE, got %s", stopGate.Verdict)
	}

	rendered := report.Render("codex", preflight.Version("0.146.0"))
	if !strings.Contains(rendered, "REFUSE") {
		t.Errorf("Render output missing %q:\n%s", "REFUSE", rendered)
	}
	if !strings.Contains(rendered, "stop-gate") {
		t.Errorf("Render output missing %q:\n%s", "stop-gate", rendered)
	}
}

func TestPreflightDegradeNamesTheLoss(t *testing.T) {
	targets := loadTargets(t)
	codex := targets["codex"]
	if codex == nil {
		t.Fatal("no codex target loaded")
	}

	falseVal := false
	c := &contract.Contract{
		ContractVersion: "1.0.0",
		Requires: []contract.Requirement{
			{
				ID:           "file-read-signal",
				Kind:         "observation-signal",
				Event:        "file-read",
				MinTier:      contract.T1,
				HardRequired: &falseVal,
			},
		},
	}

	report, err := preflight.Check(c, codex, preflight.Version("0.146.0"), false)
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if report.Refused {
		t.Fatalf("expected report not refused, got Refused=true; lines=%+v", report.Lines)
	}

	line := lineByID(t, report, "file-read-signal")
	if line.Verdict != "DEGRADE" {
		t.Fatalf("want verdict DEGRADE, got %s (detail=%q)", line.Verdict, line.Detail)
	}
	if !strings.Contains(line.Detail, "pipelines") {
		t.Errorf("Detail should name the loss (expected substring %q), got %q", "pipelines", line.Detail)
	}
}

func TestProbeFailsClosedNonInteractive(t *testing.T) {
	badTarget := &target.Def{
		Name: "not-a-real-target",
		Probe: target.ProbeSpec{
			Command:        []string{"definitely-not-a-binary-xyz"},
			VersionPattern: `(\d+\.\d+\.\d+)`,
		},
	}

	if _, err := preflight.Probe(badTarget); err == nil {
		t.Fatal("expected Probe to fail for a nonexistent binary, got nil error")
	}

	c := loadKbExample(t)
	targets := loadTargets(t)
	codex := targets["codex"]

	if _, err := preflight.Check(c, codex, preflight.Version(""), false); err == nil {
		t.Fatal("expected Check to fail closed on unprobed version in non-interactive mode")
	}

	report, err := preflight.Check(c, codex, preflight.Version(""), true)
	if err != nil {
		t.Fatalf("expected Check to proceed in interactive mode with unprobed version, got error: %v", err)
	}

	rendered := report.Render("codex", preflight.Version(""))
	if !strings.Contains(rendered, "unknown") {
		t.Errorf("Render output should show %q for unprobed version:\n%s", "unknown", rendered)
	}
}

// TestRenderShowsDashForAbsentGotTier: an ABSENT line has no achieved
// tier (contract.Tier's zero value stringifies to ""); Render must
// show an explicit no-value marker instead of a blank "got" column.
func TestRenderShowsDashForAbsentGotTier(t *testing.T) {
	falseVal := false
	c := &contract.Contract{
		ContractVersion: "1.0.0",
		Requires: []contract.Requirement{
			{ID: "ghost", Kind: "lifecycle-signal", Event: "compact-pre", MinTier: contract.T1, HardRequired: &falseVal},
		},
	}
	td := &target.Def{Name: "stub", Events: map[string]target.EventMapping{}}

	report, err := preflight.Check(c, td, preflight.Version("1.0.0"), false)
	if err != nil {
		t.Fatalf("Check: %v", err)
	}

	line := lineByID(t, report, "ghost")
	if line.Verdict != preflight.Absent {
		t.Fatalf("want verdict ABSENT, got %s", line.Verdict)
	}

	rendered := report.Render("stub", preflight.Version("1.0.0"))
	if !strings.Contains(rendered, "got —") {
		t.Errorf("Render output missing dash for absent got-tier:\n%s", rendered)
	}
}

func TestRenderFallbackDisclosure(t *testing.T) {
	no := false
	c := &contract.Contract{Requires: []contract.Requirement{
		{ID: "s", Kind: "skill", Path: "./skills/s", MinTierRaw: "T1", MinTier: contract.T1, HardRequired: &no},
		{ID: "g", Kind: "blocking-gate", Event: "turn-end", MinTierRaw: "T3", MinTier: contract.T3,
			HardRequired: &no, Handler: "./h.sh", FallbackSkill: "s"},
	}}
	td := &target.Def{
		Name:         "t",
		Events:       map[string]target.EventMapping{},
		SkillChannel: target.SkillChannel{Kind: "copy-dir", Dest: "~/.agents/skills"},
	}
	report, err := preflight.Check(c, td, preflight.Version("1.0.0"), false)
	if err != nil {
		t.Fatal(err)
	}
	out := report.Render("t", "1.0.0")
	if !strings.Contains(out, "compiled to instructions") {
		t.Fatalf("fallback line missing mechanism:\n%s", out)
	}
	if !strings.Contains(out, contract.T3Fidelity) {
		t.Fatalf("fallback disclosure must render contract.T3Fidelity verbatim:\n%s", out)
	}
	if strings.Contains(out, "REFUSE") {
		t.Fatalf("hard-free contract must not refuse:\n%s", out)
	}
}

func TestHardT3FlipsRefuseToSatisfy(t *testing.T) {
	yes := true
	gate := contract.Requirement{ID: "g", Kind: "blocking-gate", Event: "turn-end",
		MinTier: contract.T3, HardRequired: &yes, Handler: "./h.sh", FallbackSkill: "s"}
	td := &target.Def{
		Events:       map[string]target.EventMapping{},
		SkillChannel: target.SkillChannel{Kind: "copy-dir", Dest: "~/.agents/skills"},
	}

	if v := preflight.Classify(tier.Assign(gate, td)); v != preflight.Satisfy {
		t.Fatalf("hard minTier T3 with fallback must SATISFY, got %s", v)
	}

	hardT1 := gate
	hardT1.MinTier = contract.T1
	if v := preflight.Classify(tier.Assign(hardT1, td)); v != preflight.Refuse {
		t.Fatalf("hard minTier T1 must still REFUSE, got %s", v)
	}

	noFallback := gate
	noFallback.FallbackSkill = ""
	if v := preflight.Classify(tier.Assign(noFallback, td)); v != preflight.Refuse {
		t.Fatalf("hard gate without fallback must still REFUSE, got %s", v)
	}
}

// TestClassify exercises preflight.Classify directly, covering all five
// verdict branches (the tier-comparison direction: contract.Tier is
// best-to-worst T1..T4, so "achieved at least as good as wanted" is the
// numeric comparison got <= want, not got >= want).
func TestClassify(t *testing.T) {
	trueVal, falseVal := true, false

	cases := []struct {
		name string
		a    tier.Assignment
		want string
	}{
		{
			name: "absent and hard required refuses",
			a: tier.Assignment{
				Req:    contract.Requirement{MinTier: contract.T1, HardRequired: &trueVal},
				Absent: true,
			},
			want: preflight.Refuse,
		},
		{
			name: "absent and not hard required is merely absent",
			a: tier.Assignment{
				Req:    contract.Requirement{MinTier: contract.T1, HardRequired: &falseVal},
				Absent: true,
			},
			want: preflight.Absent,
		},
		{
			name: "achieved at least as good as wanted satisfies (kb matrix: want T4, got T2)",
			a: tier.Assignment{
				Req:  contract.Requirement{MinTier: contract.T4, HardRequired: &falseVal},
				Tier: contract.T2,
			},
			want: preflight.Satisfy,
		},
		{
			name: "achieved worse than wanted and hard required refuses",
			a: tier.Assignment{
				Req:  contract.Requirement{MinTier: contract.T1, HardRequired: &trueVal},
				Tier: contract.T2,
			},
			want: preflight.Refuse,
		},
		{
			name: "achieved worse than wanted and not hard required degrades",
			a: tier.Assignment{
				Req:  contract.Requirement{MinTier: contract.T1, HardRequired: &falseVal},
				Tier: contract.T2,
			},
			want: preflight.Degrade,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := preflight.Classify(tc.a)
			if got != tc.want {
				t.Errorf("Classify(%+v) = %s, want %s", tc.a, got, tc.want)
			}
		})
	}
}
