package contract

import "fmt"

const Namespace = "dev.whippletree.v1"

type Tier int

const (
	T1 Tier = iota + 1
	T2
	T3
	T4
)

func (t Tier) String() string { return [...]string{"", "T1", "T2", "T3", "T4"}[t] }

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
