// Package compile builds the per-target manifest and hooks-file variants
// a bundle needs, from its plugin.json contract and the set of loaded
// target definitions.
package compile

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/larstonder/whippletree/internal/contract"
	"github.com/larstonder/whippletree/internal/target"
	"github.com/larstonder/whippletree/internal/tier"
)

// hooksEmittingKinds are the requirement kinds that produce entries in a
// target's hooks file. executable-path requirements are satisfied via
// the bundle's manifest/bin layout instead, so they emit nothing here.
var hooksEmittingKinds = map[string]bool{
	"blocking-gate":      true,
	"lifecycle-signal":   true,
	"observation-signal": true,
}

// Result collects, per target name, every requirement's tier.Assignment
// as computed by Build.
type Result struct {
	PerTarget map[string][]tier.Assignment
}

// Build reads bundleDir's plugin.json, parses and validates its
// whippletree contract, and for every target in targets writes:
//
//   - <manifestDir>/plugin.json: the bundle's original manifest fields
//     plus a "hooks" pointer to that target's hooks file.
//   - hooks/<name>.json: the native hooks file, one entry per
//     non-absent blocking-gate/lifecycle-signal/observation-signal
//     requirement.
//   - .whippletree/contract.json: the normalized, parsed contract.
//   - .whippletree/targets/<name>.yaml: a byte copy of the target's
//     source target.yaml.
//
// It never writes hooks/hooks.json; each target's hooks file is named
// for that target.
func Build(bundleDir string, targets map[string]*target.Def) (*Result, error) {
	for name := range targets {
		if name == "hooks" {
			return nil, fmt.Errorf("target name %q is reserved: it would collide with hooks/hooks.json", name)
		}
	}

	manifestPath := filepath.Join(bundleDir, "plugin.json")
	rawManifest, err := os.ReadFile(manifestPath)
	if err != nil {
		return nil, fmt.Errorf("read plugin manifest: %w", err)
	}

	c, err := contract.Parse(rawManifest)
	if err != nil {
		return nil, fmt.Errorf("parse contract: %w", err)
	}
	if err := contract.Validate(c); err != nil {
		return nil, fmt.Errorf("validate contract: %w", err)
	}

	var manifestFields map[string]any
	if err := json.Unmarshal(rawManifest, &manifestFields); err != nil {
		return nil, fmt.Errorf("parse plugin manifest fields: %w", err)
	}

	if err := checkRequirementPaths(bundleDir, c); err != nil {
		return nil, err
	}

	if err := vendorContract(bundleDir, c); err != nil {
		return nil, err
	}

	result := &Result{PerTarget: make(map[string][]tier.Assignment, len(targets))}

	for name, td := range targets {
		assignments := make([]tier.Assignment, 0, len(c.Requires))
		hf := newHooksFile()

		for _, req := range c.Requires {
			a := tier.Assign(req, td)
			assignments = append(assignments, a)

			if a.Absent || !hooksEmittingKinds[req.Kind] {
				continue
			}
			if err := addHookEntry(hf, req, td, name); err != nil {
				return nil, err
			}
		}
		result.PerTarget[name] = assignments

		if err := writeManifest(bundleDir, name, td, manifestFields); err != nil {
			return nil, err
		}
		if err := writeHooksFile(bundleDir, name, hf); err != nil {
			return nil, err
		}
		if err := vendorTargetYAML(bundleDir, name, td); err != nil {
			return nil, err
		}
	}

	return result, nil
}

// checkRequirementPaths verifies, for every requirement in c, that any
// file it names on disk actually exists relative to bundleDir: a
// non-empty Handler (every kind but executable-path) or, for
// executable-path requirements, Path. Both are bundle-root-relative
// regardless of target, so this runs once per Build rather than per
// target. A missing file is a build error naming the requirement and
// the path, since an unnoticed typo here would otherwise only surface
// at hook-fire time, as a silently-ignored missing-handler warning.
func checkRequirementPaths(bundleDir string, c *contract.Contract) error {
	for _, req := range c.Requires {
		if req.Handler != "" {
			if err := statRequirementFile(bundleDir, req.ID, "handler", req.Handler); err != nil {
				return err
			}
		}
		if req.Kind == "executable-path" && req.Path != "" {
			if err := statRequirementFile(bundleDir, req.ID, "path", req.Path); err != nil {
				return err
			}
		}
	}
	return nil
}

// statRequirementFile verifies relPath exists as a regular file under
// bundleDir, returning a build error naming reqID, field, and relPath
// otherwise.
func statRequirementFile(bundleDir, reqID, field, relPath string) error {
	full := filepath.Join(bundleDir, relPath)
	info, err := os.Stat(full)
	if err != nil {
		return fmt.Errorf("requirement %s: %s %q: %w", reqID, field, relPath, err)
	}
	if info.IsDir() {
		return fmt.Errorf("requirement %s: %s %q is a directory, want a file", reqID, field, relPath)
	}
	return nil
}

// addHookEntry appends the hooks-file entry for req (assigned to target
// name via td) onto hf.
func addHookEntry(hf *hooksFile, req contract.Requirement, td *target.Def, name string) error {
	primitive, toolClass, err := contract.ResolveEvent(req.Event)
	if err != nil {
		return fmt.Errorf("requirement %s: %w", req.ID, err)
	}

	mapping, ok := td.Events[primitive]
	if !ok {
		return fmt.Errorf("requirement %s: target %q has no mapping for primitive %q", req.ID, name, primitive)
	}

	var matcher string
	if req.Kind == "observation-signal" {
		if v, ok := td.ToolClassMap[toolClass]; ok && v != nil {
			matcher = *v
		} else if d, ok := td.Degradations[req.Event]; ok {
			matcher = d.Matcher
		}
	}

	if len(td.PluginRootVars) == 0 {
		return fmt.Errorf("requirement %s: target %q declares no pluginRoot env var", req.ID, name)
	}
	command := fmt.Sprintf("\"${%s}/bin/whippletree-hook\" run %s --target %s", td.PluginRootVars[0], req.Event, name)

	hf.add(primitive, mapping.Native, hookEntry{
		Matcher: matcher,
		Hooks:   []hookCommand{{Type: "command", Command: command}},
	})
	return nil
}

// writeManifest writes <manifestDir>/plugin.json for target name,
// carrying over the bundle's original manifest fields verbatim and
// adding a "hooks" pointer to that target's hooks file.
func writeManifest(bundleDir, name string, td *target.Def, manifestFields map[string]any) error {
	targetManifest := make(map[string]any, len(manifestFields)+1)
	for k, v := range manifestFields {
		targetManifest[k] = v
	}
	targetManifest["hooks"] = "./hooks/" + name + ".json"

	body, err := json.MarshalIndent(targetManifest, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal manifest for target %q: %w", name, err)
	}
	body = append(body, '\n')

	dir := filepath.Join(bundleDir, td.ManifestDir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create manifest dir for target %q: %w", name, err)
	}
	if err := os.WriteFile(filepath.Join(dir, "plugin.json"), body, 0o644); err != nil {
		return fmt.Errorf("write manifest for target %q: %w", name, err)
	}
	return nil
}

// writeHooksFile writes hooks/<name>.json for target name.
func writeHooksFile(bundleDir, name string, hf *hooksFile) error {
	body, err := json.MarshalIndent(hf, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal hooks file for target %q: %w", name, err)
	}
	body = append(body, '\n')

	dir := filepath.Join(bundleDir, "hooks")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create hooks dir for target %q: %w", name, err)
	}
	if err := os.WriteFile(filepath.Join(dir, name+".json"), body, 0o644); err != nil {
		return fmt.Errorf("write hooks file for target %q: %w", name, err)
	}
	return nil
}

// vendorContract writes .whippletree/contract.json: the normalized,
// parsed contract.
func vendorContract(bundleDir string, c *contract.Contract) error {
	dir := filepath.Join(bundleDir, ".whippletree")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create .whippletree dir: %w", err)
	}

	body, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal vendored contract: %w", err)
	}
	body = append(body, '\n')

	if err := os.WriteFile(filepath.Join(dir, "contract.json"), body, 0o644); err != nil {
		return fmt.Errorf("write vendored contract: %w", err)
	}
	return nil
}

// vendorTargetYAML writes .whippletree/targets/<name>.yaml: a byte copy
// of the target's source target.yaml.
func vendorTargetYAML(bundleDir, name string, td *target.Def) error {
	if td.SourcePath == "" {
		return fmt.Errorf("target %q has no recorded SourcePath to vendor", name)
	}
	src, err := os.ReadFile(td.SourcePath)
	if err != nil {
		return fmt.Errorf("read target source for %q: %w", name, err)
	}

	dir := filepath.Join(bundleDir, ".whippletree", "targets")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create vendored targets dir: %w", err)
	}
	if err := os.WriteFile(filepath.Join(dir, name+".yaml"), src, 0o644); err != nil {
		return fmt.Errorf("write vendored target %q: %w", name, err)
	}
	return nil
}
