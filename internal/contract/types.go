package contract

import "fmt"

const Namespace = "dev.whippletree.v1"

// SupportedContractVersion is the highest contractVersion this build of
// whippletree understands.
//
// The compatibility rule, enforced by Validate:
//
//   - Major must match exactly. A major bump means the meaning of
//     existing fields changed, so an older binary cannot be trusted to
//     read a newer contract, nor the reverse.
//   - Minor and patch may be anything at or below this value. Minor
//     bumps are additive, so a 1.0.0 contract is readable by a 1.2.0
//     binary; a 1.2.0 contract is refused by a 1.0.0 binary, because it
//     may rely on a field this build would silently ignore.
//
// Silently ignoring a field a contract depends on is the failure mode
// worth preventing: whippletree's whole claim is that it either
// delivers what was declared or says plainly that it cannot.
const SupportedContractVersion = "1.0.0"

type Tier int

const (
	T1 Tier = iota + 1
	T2
	T3
	T4
)

func (t Tier) String() string { return [...]string{"", "T1", "T2", "T3", "T4"}[t] }

// T3Fidelity is the one fidelity sentence for instruction-compiled
// (fallbackSkill) behaviors. The preflight detail line and the
// generated SKILL.md provenance comment must both use it verbatim: the
// architecture doc's honesty contract requires the installer's promise
// and the artifact's own comment to make the same claim in the same
// words.
const T3Fidelity = "best-effort, no harness-level enforcement on this target: the model is instructed to run the step and usually will, but can skip it under pressure"

func ParseTier(s string) (Tier, error) {
	for t := T1; t <= T4; t++ {
		if t.String() == s {
			return t, nil
		}
	}
	return 0, fmt.Errorf("invalid tier %q", s)
}

type Contract struct {
	ContractVersion string        `json:"contractVersion"`
	Requires        []Requirement `json:"requires"`
}

type Requirement struct {
	ID                string `json:"id"`
	Kind              string `json:"kind"`
	Event             string `json:"event,omitempty"`
	MinTierRaw        string `json:"minTier"`
	MinTier           Tier   `json:"-"`
	HardRequired      *bool  `json:"hardRequired"`
	LoopGuardRequired bool   `json:"loopGuardRequired,omitempty"`
	Handler           string `json:"handler,omitempty"`
	FallbackSkill     string `json:"fallbackSkill,omitempty"`
	Path              string `json:"path,omitempty"`
	Description       string `json:"description,omitempty"`
}
