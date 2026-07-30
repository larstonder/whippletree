// Package tier assigns a contract.Requirement to the tier at which a
// specific target can satisfy it, given that target's Def.
package tier

import (
	"fmt"

	"github.com/larstonder/whippletree/internal/contract"
	"github.com/larstonder/whippletree/internal/target"
)

// Assignment records the outcome of assigning a Requirement to a target:
// the tier it lands at, the mechanism used to satisfy it, any known
// lossage from a degraded (non-native) implementation, and whether the
// target cannot satisfy it at all in this slice.
type Assignment struct {
	Req       contract.Requirement
	Tier      contract.Tier
	Mechanism string
	Lossage   string
	Absent    bool
}

// Assign determines how (and whether) target td can satisfy requirement
// req in this slice.
func Assign(req contract.Requirement, td *target.Def) Assignment {
	switch req.Kind {
	case "executable-path":
		return assignExecutablePath(req, td)
	case "lifecycle-signal":
		return assignLifecycleSignal(req, td)
	case "blocking-gate":
		return assignBlockingGate(req, td)
	case "observation-signal":
		return assignObservationSignal(req, td)
	default:
		return absent(req, "unknown kind")
	}
}

func absent(req contract.Requirement, reason string) Assignment {
	return Assignment{Req: req, Absent: true, Mechanism: reason}
}

func assignExecutablePath(req contract.Requirement, td *target.Def) Assignment {
	if td.Capabilities["bundleChannel"] {
		return Assignment{Req: req, Tier: contract.T1, Mechanism: "bundle channel"}
	}
	return absent(req, "no bundle channel on this target")
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
