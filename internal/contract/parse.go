package contract

import (
	"encoding/json"
	"fmt"
)

type pluginManifest struct {
	Extensions map[string]json.RawMessage `json:"extensions"`
}

// Parse extracts the whippletree Contract from a plugin manifest's
// "extensions" block, keyed by Namespace.
func Parse(pluginJSON []byte) (*Contract, error) {
	var manifest pluginManifest
	if err := json.Unmarshal(pluginJSON, &manifest); err != nil {
		return nil, fmt.Errorf("parse plugin manifest: %w", err)
	}

	raw, ok := manifest.Extensions[Namespace]
	if !ok {
		return nil, fmt.Errorf("plugin manifest missing extensions[%q]", Namespace)
	}

	var c Contract
	if err := json.Unmarshal(raw, &c); err != nil {
		return nil, fmt.Errorf("parse contract: %w", err)
	}

	for i := range c.Requires {
		req := &c.Requires[i]
		tier, err := ParseTier(req.MinTierRaw)
		if err != nil {
			return nil, fmt.Errorf("requirement %q: %w", req.ID, err)
		}
		req.MinTier = tier
	}

	return &c, nil
}
