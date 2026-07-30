package contract

import (
	"errors"
	"fmt"
)

var validKinds = map[string]bool{
	"blocking-gate":      true,
	"lifecycle-signal":   true,
	"observation-signal": true,
	"executable-path":    true,
}

// Validate checks a Contract against the adapter-sdk structural rules,
// collecting every violation via errors.Join rather than stopping at
// the first one.
func Validate(c *Contract) error {
	var errs []error
	seen := make(map[string]bool, len(c.Requires))

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

		if req.Kind == "executable-path" {
			if req.Path == "" {
				errs = append(errs, fmt.Errorf("requirement %s: path is required for executable-path", req.ID))
			}
			if req.Event != "" {
				errs = append(errs, fmt.Errorf("requirement %s: event must be empty for executable-path", req.ID))
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
		}
	}

	return errors.Join(errs...)
}
