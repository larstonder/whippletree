// SPDX-FileCopyrightText: 2026 Lars Tønder
// SPDX-License-Identifier: Apache-2.0

// Package tier assigns a contract.Requirement to the tier at which a
// specific target can satisfy it, given that target's Def.
package tier

import (
	"fmt"

	"whippletree.dev/internal/contract"
	"whippletree.dev/internal/target"
)

// Assignment records the outcome of assigning a Requirement to a target:
// the tier it lands at, the mechanism used to satisfy it, any known
// lossage from a degraded (non-native) implementation, and whether the
// target cannot satisfy it at all.
type Assignment struct {
	Req       contract.Requirement
	Tier      contract.Tier
	Mechanism string
	Lossage   string
	Absent    bool
	// Fallback marks a T3 landed via a fallbackSkill expansion rather
	// than a hook; preflight's disclosure and build's expansion both
	// key on it, never on the tier value.
	Fallback bool
}

// Assign determines how (and whether) target td can satisfy requirement
// req.
func Assign(req contract.Requirement, td *target.Def) Assignment {
	a := assignByKind(req, td)
	if a.Absent && req.FallbackSkill != "" && td.SkillChannel.Kind != "" &&
		(req.Kind == "blocking-gate" || req.Kind == "lifecycle-signal") {
		return Assignment{Req: req, Tier: contract.T3, Mechanism: "compiled to instructions", Fallback: true}
	}
	return a
}

func assignByKind(req contract.Requirement, td *target.Def) Assignment {
	switch req.Kind {
	case "executable-path":
		return assignExecutablePath(req, td)
	case "lifecycle-signal":
		return assignLifecycleSignal(req, td)
	case "blocking-gate":
		return assignBlockingGate(req, td)
	case "observation-signal":
		return assignObservationSignal(req, td)
	case "skill":
		return assignSkill(req, td)
	default:
		return absent(req, "unknown kind")
	}
}

// assignSkill reports placement fidelity only, never behavioral
// fidelity: a placed skill is words the model may or may not act on,
// which is why the mechanism says "placed", not "enforced".
func assignSkill(req contract.Requirement, td *target.Def) Assignment {
	if td.SkillChannel.Kind == "" {
		return absent(req, "no skill channel on this target")
	}
	return Assignment{Req: req, Tier: contract.T1, Mechanism: "placed via " + td.SkillChannel.Kind + " skill channel"}
}

func absent(req contract.Requirement, reason string) Assignment {
	return Assignment{Req: req, Absent: true, Mechanism: reason}
}

func assignExecutablePath(req contract.Requirement, td *target.Def) Assignment {
	if td.Capabilities["bundleChannel"] {
		return Assignment{Req: req, Tier: contract.T1, Mechanism: "bundle channel"}
	}
	if td.Capabilities["installerPath"] {
		return Assignment{Req: req, Tier: contract.T1, Mechanism: "installer-resolved absolute path"}
	}
	return absent(req, "no bundle channel or installer path on this target")
}

func assignLifecycleSignal(req contract.Requirement, td *target.Def) Assignment {
	primitive, _, err := contract.ResolveEvent(req.Event)
	if err != nil {
		return absent(req, fmt.Sprintf("unresolvable event %s", req.Event))
	}
	mapping, ok := td.Events[primitive]
	if !ok {
		return absent(req, fmt.Sprintf("no native mapping for %s on this target", primitive))
	}
	return Assignment{Req: req, Tier: contract.T1, Mechanism: "native " + mapping.Native}
}

func assignBlockingGate(req contract.Requirement, td *target.Def) Assignment {
	primitive, _, err := contract.ResolveEvent(req.Event)
	if err != nil {
		return absent(req, fmt.Sprintf("unresolvable event %s", req.Event))
	}
	mapping, ok := td.Events[primitive]
	if !ok {
		return absent(req, fmt.Sprintf("no native mapping for %s on this target", primitive))
	}
	if !mapping.Blocking {
		return absent(req, fmt.Sprintf("%s is not blocking on this target", mapping.Native))
	}
	if mapping.LoopGuardField == "" && req.LoopGuardRequired {
		return absent(req, fmt.Sprintf("no loop-guard field on %s", mapping.Native))
	}

	mechanism := "native " + mapping.Native
	if mapping.LoopGuardField != "" {
		mechanism += " + " + mapping.LoopGuardField
	}
	return Assignment{Req: req, Tier: contract.T1, Mechanism: mechanism}
}

func assignObservationSignal(req contract.Requirement, td *target.Def) Assignment {
	primitive, toolClass, err := contract.ResolveEvent(req.Event)
	if err != nil {
		return absent(req, fmt.Sprintf("unresolvable event %s", req.Event))
	}
	if _, ok := td.Events[primitive]; !ok {
		return absent(req, fmt.Sprintf("no native mapping for %s on this target", primitive))
	}

	if v, ok := td.ToolClassMap[toolClass]; ok && v != nil {
		return Assignment{Req: req, Tier: contract.T1, Mechanism: "native matcher " + *v}
	}

	if d, ok := td.Degradations[req.Event]; ok {
		return Assignment{Req: req, Tier: d.Tier, Mechanism: "matcher " + d.Matcher, Lossage: d.Lossage}
	}

	return absent(req, fmt.Sprintf("no %s tool and no degradation declared", toolClass))
}
