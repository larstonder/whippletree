// Package compile builds the per-target manifest and hooks-file variants
// a bundle needs, from its plugin.json contract and the set of loaded
// target definitions.
package compile

import (
	"encoding/json"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"

	"whippletree.dev/internal/contract"
	"whippletree.dev/internal/skillfile"
	"whippletree.dev/internal/target"
	"whippletree.dev/internal/tier"
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

// Build compiles bundleDir's contract for every target, writing
// .whippletree/contract.json and .whippletree/targets/<name>.yaml, plus
// per backend: hooks-json gets <manifestDir>/plugin.json and
// hooks/<name>.json; ts-plugin gets hooks/<name>.ts and no manifest,
// having none for whippletree to extend.
//
// Each hooks file is named for its target, never hooks/hooks.json.
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
		return nil, err
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
		tp := newTSPlugin()

		for _, req := range c.Requires {
			a := tier.Assign(req, td)
			assignments = append(assignments, a)

			// A Fallback assignment is satisfied by an expanded skill,
			// never by a hook entry: writeSkillVariants (below) is what
			// materializes it, or errors loudly on a target that can't
			// carry one.
			if a.Absent || a.Fallback || !hooksEmittingKinds[req.Kind] {
				continue
			}

			if td.Backend == target.BackendTSPlugin {
				if err := addTSHookEntry(tp, req, td, name); err != nil {
					return nil, err
				}
				continue
			}
			if err := addHookEntry(hf, req, td, name); err != nil {
				return nil, err
			}
		}
		result.PerTarget[name] = assignments

		if err := writeSkillVariants(bundleDir, name, td, c, assignments); err != nil {
			return nil, err
		}

		if err := vendorTargetYAML(bundleDir, name, td); err != nil {
			return nil, err
		}

		if td.Backend == target.BackendTSPlugin {
			if err := writeTSPluginFile(bundleDir, name, tp); err != nil {
				return nil, err
			}
			continue
		}

		if err := writeManifest(bundleDir, name, td, manifestFields); err != nil {
			return nil, err
		}
		if err := writeHooksFile(bundleDir, name, hf); err != nil {
			return nil, err
		}
	}

	return result, nil
}

// checkRequirementPaths verifies each requirement's on-disk file
// (Handler, or Path for executable-path) exists relative to bundleDir.
// A missing file is a build error naming the requirement and the path;
// an unnoticed typo here would otherwise only surface at hook-fire
// time, as a silently-ignored missing-handler warning.
func checkRequirementPaths(bundleDir string, c *contract.Contract) error {
	for _, req := range c.Requires {
		if req.Handler != "" {
			if err := statRequirementFile(bundleDir, req.ID, "handler", req.Handler); err != nil {
				return err
			}
		}
		if req.Kind == "skill" {
			if err := skillfile.Check(bundleDir, req); err != nil {
				return err
			}
			continue
		}
		if req.Kind == "executable-path" && req.Path != "" {
			if err := statRequirementFile(bundleDir, req.ID, "path", req.Path); err != nil {
				return err
			}
		}
	}
	return nil
}

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

	matcher := matcherFor(req, td, toolClass)

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

// matcherFor resolves the tool-id matcher a requirement fires under,
// shared by both backends' emitters: only observation-signal
// requirements carry a tool-id filter, resolved first via the
// target's native toolClassMap and falling back to a declared
// degradation. Any other kind (blocking-gate, lifecycle-signal) fires
// unconditionally, so its matcher is empty.
func matcherFor(req contract.Requirement, td *target.Def, toolClass string) string {
	if req.Kind != "observation-signal" {
		return ""
	}
	if v, ok := td.ToolClassMap[toolClass]; ok && v != nil {
		return *v
	}
	if d, ok := td.Degradations[req.Event]; ok {
		return d.Matcher
	}
	return ""
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

func vendorTargetYAML(bundleDir, name string, td *target.Def) error {
	// Guards only against a hand-constructed Def in a test; every real
	// producer of a Def (target.Load, LoadDir, LoadFS) always sets
	// RawYAML.
	if len(td.RawYAML) == 0 {
		return fmt.Errorf("target %q has no recorded RawYAML to vendor", name)
	}

	dir := filepath.Join(bundleDir, ".whippletree", "targets")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create vendored targets dir: %w", err)
	}
	if err := os.WriteFile(filepath.Join(dir, name+".yaml"), td.RawYAML, 0o644); err != nil {
		return fmt.Errorf("write vendored target %q: %w", name, err)
	}
	return nil
}

// CheckSkillFiles verifies every skill requirement's SKILL.md under
// bundleDir. Exported so preflight and install (checkAgainstTarget in
// cmd/whippletree) fail with the same message build does instead of
// passing a bundle build would reject.
func CheckSkillFiles(bundleDir string, c *contract.Contract) error {
	for _, req := range c.Requires {
		if req.Kind != "skill" {
			continue
		}
		if err := skillfile.Check(bundleDir, req); err != nil {
			return err
		}
	}
	return nil
}

// writeSkillVariants generates the per-target skill directory variants
// under .whippletree/skills/<target>/. copy-dir targets always get a
// variant (the compiled-by marker is install's ownership signal);
// plugin-dir and channel-less targets get none, and an expansion
// triggering there is a hard error rather than a silently unexpanded
// skill.
func writeSkillVariants(bundleDir, name string, td *target.Def, c *contract.Contract, assignments []tier.Assignment) error {
	expsBySkill := make(map[string][]skillfile.Expansion)
	for _, a := range assignments {
		if !a.Fallback {
			continue
		}
		primitive, _, err := contract.ResolveEvent(a.Req.Event)
		if err != nil {
			return fmt.Errorf("requirement %s: %w", a.Req.ID, err)
		}
		expsBySkill[a.Req.FallbackSkill] = append(expsBySkill[a.Req.FallbackSkill], skillfile.Expansion{
			Event: primitive, ReqID: a.Req.ID, Kind: a.Req.Kind,
			Handler: a.Req.Handler, Target: name,
		})
	}

	if td.SkillChannel.Kind != "copy-dir" {
		if len(expsBySkill) > 0 {
			return fmt.Errorf("target %q: skill expansion is not supported on plugin-dir targets", name)
		}
		return nil
	}

	version := skillfile.Version()
	for _, req := range c.Requires {
		if req.Kind != "skill" {
			continue
		}
		dirName := path.Base(req.Path)
		src := filepath.Join(bundleDir, filepath.FromSlash(strings.TrimPrefix(req.Path, "./")))
		dst := filepath.Join(bundleDir, ".whippletree", "skills", name, dirName)
		if err := skillfile.ExpandDir(src, dst, expsBySkill[req.ID], version); err != nil {
			return fmt.Errorf("target %q: skill %s: %w", name, req.ID, err)
		}
	}
	return nil
}
