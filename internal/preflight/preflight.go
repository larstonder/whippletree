// Package preflight compares a bundle's contract against a target's
// tier assignment (and, optionally, its probed installed version),
// producing a terraform-plan-shaped report of what will satisfy,
// degrade, or refuse to install.
package preflight

import (
	"bytes"
	"fmt"
	"os/exec"
	"regexp"
	"strings"

	"github.com/larstonder/adapter-sdk/internal/contract"
	"github.com/larstonder/adapter-sdk/internal/target"
	"github.com/larstonder/adapter-sdk/internal/tier"
)

// Version is a target's detected (or assumed) installed version string,
// e.g. "0.146.0". An empty Version means the target was not probed.
type Version string

// Verdict values a Line may carry.
const (
	Satisfy = "SATISFY"
	Degrade = "DEGRADE"
	Refuse  = "REFUSE"
	Absent  = "ABSENT"
)

// Line is one requirement's preflight verdict: what tier it wants, what
// tier the target actually achieves, and why.
type Line struct {
	ReqID  string
	Want   contract.Tier
	Got    contract.Tier
	Verdict string
	Detail string
}

// Report is the full preflight result for a contract against a target,
// one Line per requirement in contract order.
type Report struct {
	Lines   []Line
	Refused bool
}

// Check evaluates every requirement in c against target td, using the
// tier assignment td achieves for each (see internal/tier), and
// produces a Report.
//
// probed carries the target's detected (or --assume-version'd)
// version. When it is empty (unprobed) and interactive is false, Check
// fails closed and returns an error rather than silently reporting
// against an unverified target. In interactive mode an empty probed
// version is allowed through; Render then shows it as "unknown".
func Check(c *contract.Contract, td *target.Def, probed Version, interactive bool) (*Report, error) {
	if probed == "" && !interactive {
		return nil, fmt.Errorf("no probed version for target %q and not running interactively", td.Name)
	}

	report := &Report{}
	for _, req := range c.Requires {
		a := tier.Assign(req, td)
		hard := req.HardRequired != nil && *req.HardRequired

		line := Line{ReqID: req.ID, Want: req.MinTier, Got: a.Tier}

		switch {
		case a.Absent && hard:
			line.Verdict = Refuse
			line.Detail = a.Mechanism
			report.Refused = true
		case a.Absent:
			line.Verdict = Absent
			line.Detail = a.Mechanism
		case a.Tier <= req.MinTier:
			line.Verdict = Satisfy
			line.Detail = withLossage(a.Mechanism, a.Lossage)
		case hard:
			line.Verdict = Refuse
			line.Detail = withLossage(a.Mechanism, a.Lossage)
			report.Refused = true
		default:
			line.Verdict = Degrade
			line.Detail = withLossage(a.Mechanism, a.Lossage)
		}

		report.Lines = append(report.Lines, line)
	}

	return report, nil
}

// withLossage appends lossage to mechanism (the literal-loss rule: name
// the mechanism and the specific gap, never just "reduced fidelity")
// when lossage is non-empty.
func withLossage(mechanism, lossage string) string {
	if lossage == "" {
		return mechanism
	}
	return mechanism + "; " + lossage
}

// Probe executes td.Probe.Command and extracts the target's installed
// version from its combined stdout+stderr output, using the first
// capture group of td.Probe.VersionPattern.
func Probe(td *target.Def) (Version, error) {
	if len(td.Probe.Command) == 0 {
		return "", fmt.Errorf("target %q declares no probe command", td.Name)
	}

	cmd := exec.Command(td.Probe.Command[0], td.Probe.Command[1:]...)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("probe %q: run %v: %w", td.Name, td.Probe.Command, err)
	}

	re, err := regexp.Compile(td.Probe.VersionPattern)
	if err != nil {
		return "", fmt.Errorf("probe %q: invalid version pattern %q: %w", td.Name, td.Probe.VersionPattern, err)
	}

	m := re.FindStringSubmatch(out.String())
	if len(m) < 2 {
		return "", fmt.Errorf("probe %q: version pattern %q did not match probe output %q", td.Name, td.Probe.VersionPattern, out.String())
	}

	return Version(m[1]), nil
}

// Render renders r as a terraform-plan-shaped report for targetName,
// probed at version v (rendered "unknown" when v is empty).
func (r *Report) Render(targetName string, v Version) string {
	versionStr := string(v)
	if versionStr == "" {
		versionStr = "unknown"
	}

	width := 0
	for _, l := range r.Lines {
		if len(l.ReqID) > width {
			width = len(l.ReqID)
		}
	}

	var b strings.Builder
	fmt.Fprintf(&b, "adapter-sdk preflight · target %s (probed %s)\n\n", targetName, versionStr)

	var satisfy, degrade, refuse, absent int
	for _, l := range r.Lines {
		fmt.Fprintf(&b, "  %-*s  want ≥%s  got %s  %-9s %s\n", width, l.ReqID, l.Want, l.Got, l.Verdict, l.Detail)

		switch l.Verdict {
		case Satisfy:
			satisfy++
		case Degrade:
			degrade++
		case Refuse:
			refuse++
		case Absent:
			absent++
		}
	}

	fmt.Fprintf(&b, "\nPlan: %d satisfy, %d degrade, %d refuse", satisfy, degrade, refuse)
	if absent > 0 {
		fmt.Fprintf(&b, ", %d absent", absent)
	}
	b.WriteString(".\n")

	return b.String()
}
