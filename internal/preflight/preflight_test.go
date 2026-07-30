package preflight_test

import (
	"os"
	"strings"
	"testing"

	"github.com/larstonder/adapter-sdk/internal/contract"
	"github.com/larstonder/adapter-sdk/internal/preflight"
	"github.com/larstonder/adapter-sdk/internal/target"
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

	// Fail closed: unprobed (empty Version) + non-interactive must error.
	if _, err := preflight.Check(c, codex, preflight.Version(""), false); err == nil {
		t.Fatal("expected Check to fail closed on unprobed version in non-interactive mode")
	}

	// Interactive mode may proceed without a probed version.
	report, err := preflight.Check(c, codex, preflight.Version(""), true)
	if err != nil {
		t.Fatalf("expected Check to proceed in interactive mode with unprobed version, got error: %v", err)
	}

	rendered := report.Render("codex", preflight.Version(""))
	if !strings.Contains(rendered, "unknown") {
		t.Errorf("Render output should show %q for unprobed version:\n%s", "unknown", rendered)
	}
}
