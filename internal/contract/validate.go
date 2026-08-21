// SPDX-FileCopyrightText: 2026 Lars Tønder
// SPDX-License-Identifier: Apache-2.0

package contract

import (
	"errors"
	"fmt"
	"strings"
)

var validKinds = map[string]bool{
	"blocking-gate":      true,
	"lifecycle-signal":   true,
	"observation-signal": true,
	"executable-path":    true,
	"skill":              true,
}

// Validate checks a Contract against the whippletree structural rules,
// collecting every violation via errors.Join rather than stopping at
// the first one.
func Validate(c *Contract) error {
	var errs []error
	if err := validateContractVersion(c.ContractVersion); err != nil {
		errs = append(errs, err)
	}

	usesHandlerWindows := false
	seen := make(map[string]bool, len(c.Requires))
	skillIDs := make(map[string]bool)

	for i := range c.Requires {
		req := &c.Requires[i]
		if req.Kind == "skill" && req.ID != "" {
			skillIDs[req.ID] = true
		}
	}

	for i := range c.Requires {
		req := &c.Requires[i]

		if seen[req.ID] {
			errs = append(errs, fmt.Errorf("duplicate id %q", req.ID))
		}
		seen[req.ID] = true

		if req.HardRequired == nil {
			errs = append(errs, fmt.Errorf("requirement %s: hardRequired is required and has no default", req.ID))
		}

		if !validKinds[req.Kind] {
			errs = append(errs, fmt.Errorf("requirement %s: unknown kind %q", req.ID, req.Kind))
			continue
		}

		if err := validateFallbackSkill(req, skillIDs); err != nil {
			errs = append(errs, err)
		}

		if req.Kind == "skill" {
			errs = append(errs, validateSkillReq(req)...)
			continue
		}

		if req.Kind == "executable-path" {
			if req.Path == "" {
				errs = append(errs, fmt.Errorf("requirement %s: path is required for executable-path", req.ID))
			} else if err := ValidateBundleRelPath(req.Path); err != nil {
				errs = append(errs, fmt.Errorf("requirement %s: %w", req.ID, err))
			}
			if req.Event != "" {
				errs = append(errs, fmt.Errorf("requirement %s: event must be empty for executable-path", req.ID))
			}
			if req.HandlerWindows != "" {
				errs = append(errs, fmt.Errorf("requirement %s: handlerWindows must be empty for executable-path", req.ID))
			}
			continue
		}

		primitive, _, err := ResolveEvent(req.Event)
		if err != nil {
			errs = append(errs, fmt.Errorf("requirement %s: %w", req.ID, err))
		} else if req.Kind == "observation-signal" && (primitive == "tool-pre" || primitive == "tool-post") && req.MinTier == T3 {
			errs = append(errs, fmt.Errorf("requirement %s: per-tool-call observation signals cannot land at T3", req.ID))
		}

		if req.Handler == "" {
			errs = append(errs, fmt.Errorf("requirement %s: handler is required", req.ID))
		} else if err := ValidateBundleRelPath(req.Handler); err != nil {
			errs = append(errs, fmt.Errorf("requirement %s: %w", req.ID, err))
		}
		if req.HandlerWindows != "" {
			usesHandlerWindows = true
			if err := ValidateBundleRelPath(req.HandlerWindows); err != nil {
				errs = append(errs, fmt.Errorf("requirement %s: handlerWindows: %w", req.ID, err))
			} else if err := ValidateWindowsHandler(req.HandlerWindows); err != nil {
				errs = append(errs, fmt.Errorf("requirement %s: %w", req.ID, err))
			}
		}
	}

	// A contract that uses handlerWindows has to say so, or an older
	// whippletree parses it, drops the field it does not know, and runs the
	// POSIX handler's absence as though the author had chosen it.
	if usesHandlerWindows {
		if err := requireContractVersion(c.ContractVersion, handlerWindowsSince, "handlerWindows"); err != nil {
			errs = append(errs, err)
		}
	}

	return errors.Join(errs...)
}

// validateSkillReq checks the skill-kind-specific rules: path of the
// form ./skills/<dir> (one path element, so the plugin-dir discovery
// convention skills/<name>/SKILL.md holds), and no hook-only fields.
func validateSkillReq(req *Requirement) []error {
	var errs []error
	if req.Path == "" {
		errs = append(errs, fmt.Errorf("requirement %s: path is required for skill", req.ID))
	} else {
		rest, ok := strings.CutPrefix(req.Path, "./skills/")
		if !ok || rest == "" || rest == "." || rest == ".." || strings.Contains(rest, "/") {
			errs = append(errs, fmt.Errorf("requirement %s: skill path %q must have the form ./skills/<dir>", req.ID, req.Path))
		}
	}
	if req.Event != "" {
		errs = append(errs, fmt.Errorf("requirement %s: event must be empty for skill", req.ID))
	}
	if req.Handler != "" {
		errs = append(errs, fmt.Errorf("requirement %s: handler must be empty for skill", req.ID))
	}
	if req.HandlerWindows != "" {
		errs = append(errs, fmt.Errorf("requirement %s: handlerWindows must be empty for skill", req.ID))
	}
	if req.LoopGuardRequired {
		errs = append(errs, fmt.Errorf("requirement %s: loopGuardRequired must be false for skill", req.ID))
	}
	return errs
}

// validateFallbackSkill enforces where a fallbackSkill link may appear
// (the self-observable-trigger events the class-1 targets natively
// satisfy) and that it references a skill requirement in this contract.
func validateFallbackSkill(req *Requirement, skillIDs map[string]bool) error {
	if req.FallbackSkill == "" {
		return nil
	}
	switch req.Kind {
	case "blocking-gate":
		if req.Event != "turn-end" {
			return fmt.Errorf("requirement %s: fallbackSkill on blocking-gate requires event turn-end", req.ID)
		}
	case "lifecycle-signal":
		if req.Event != "session-start" {
			return fmt.Errorf("requirement %s: fallbackSkill on lifecycle-signal requires event session-start", req.ID)
		}
	default:
		return fmt.Errorf("requirement %s: fallbackSkill is not allowed on %s", req.ID, req.Kind)
	}
	if !skillIDs[req.FallbackSkill] {
		return fmt.Errorf("requirement %s: fallbackSkill %q does not name a skill requirement in this contract", req.ID, req.FallbackSkill)
	}
	return nil
}

// validateContractVersion enforces the SupportedContractVersion rule.
func validateContractVersion(raw string) error {
	if raw == "" {
		return fmt.Errorf("contractVersion is required")
	}
	got, err := ParseSemver(raw)
	if err != nil {
		return fmt.Errorf("contractVersion: %w", err)
	}
	supported, err := ParseSemver(SupportedContractVersion)
	if err != nil {
		return fmt.Errorf("contractVersion: internal: %w", err)
	}

	if got.Major != supported.Major {
		return fmt.Errorf("contractVersion %s: this whippletree supports %s; major versions must match", raw, SupportedContractVersion)
	}
	if supported.Less(got) {
		return fmt.Errorf("contractVersion %s is newer than this whippletree supports (%s); upgrade whippletree", raw, SupportedContractVersion)
	}
	return nil
}

// handlerWindowsSince is the contract version that introduced handlerWindows.
const handlerWindowsSince = "1.1.0"

// requireContractVersion rejects a contract that uses a field newer than the
// version it declares.
func requireContractVersion(raw, since, field string) error {
	got, err := ParseSemver(raw)
	if err != nil {
		return nil // already reported by validateContractVersion
	}
	min, err := ParseSemver(since)
	if err != nil {
		return fmt.Errorf("contractVersion: internal: %w", err)
	}
	if got.Less(min) {
		return fmt.Errorf("%s requires contractVersion %s or later, but this contract declares %s", field, since, raw)
	}
	return nil
}
