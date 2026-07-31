package target

import (
	"bytes"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"

	"github.com/larstonder/whippletree/internal/contract"
	"gopkg.in/yaml.v3"
)

// yamlDoc mirrors the on-disk apiVersion/kind/metadata/spec shape of a
// target.yaml file. Load flattens this into a Def.
type yamlDoc struct {
	APIVersion string       `yaml:"apiVersion"`
	Kind       string       `yaml:"kind"`
	Metadata   yamlMetadata `yaml:"metadata"`
	Spec       yamlSpec     `yaml:"spec"`
}

type yamlMetadata struct {
	Name           string `yaml:"name"`
	Class          int    `yaml:"class"`
	SchemaVersion  string `yaml:"schemaVersion"`
	TestedVersions string `yaml:"testedVersions"`
}

type yamlSpec struct {
	Backend      string                     `yaml:"backend"`
	Discovery    yamlDiscovery              `yaml:"discovery"`
	Probe        yamlProbe                  `yaml:"probe"`
	Events       map[string]yamlEvent       `yaml:"events"`
	ToolClassMap map[string]*string         `yaml:"toolClassMap"`
	Degradations map[string]yamlDegradation `yaml:"degradations"`
	Strictness   yamlStrictness             `yaml:"strictness"`
	Env          yamlEnv                    `yaml:"env"`
	Capabilities map[string]bool            `yaml:"capabilities"`
}

type yamlDiscovery struct {
	ManifestDir    string `yaml:"manifestDir"`
	HooksKey       string `yaml:"hooksKey"`
	MergeSemantics string `yaml:"mergeSemantics"`
}

type yamlProbe struct {
	Command        []string `yaml:"command"`
	VersionPattern string   `yaml:"versionPattern"`
}

type yamlEvent struct {
	Native         string `yaml:"native"`
	Blocking       bool   `yaml:"blocking"`
	LoopGuardField string `yaml:"loopGuardField"`
}

type yamlDegradation struct {
	Tier    string `yaml:"tier"`
	Matcher string `yaml:"matcher"`
	Lossage string `yaml:"lossage"`
}

type yamlStrictness struct {
	UnknownFieldsFatal bool `yaml:"unknownFieldsFatal"`
}

type yamlEnv struct {
	PluginRoot []string `yaml:"pluginRoot"`
}

// Load reads and strictly decodes a single target.yaml file, returning
// the flattened Def. Any unknown key anywhere in the document is an
// error.
func Load(path string) (*Def, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("open target file %s: %w", path, err)
	}
	return decodeTarget(raw, path)
}

// decodeTarget is the strict-decode logic shared by Load and LoadFS:
// it turns raw target.yaml bytes into a flattened Def, stamping
// sourcePath onto both Def.SourcePath and (verbatim, as the decoded
// bytes) Def.RawYAML.
func decodeTarget(raw []byte, sourcePath string) (*Def, error) {
	dec := yaml.NewDecoder(bytes.NewReader(raw))
	dec.KnownFields(true)

	var doc yamlDoc
	if err := dec.Decode(&doc); err != nil {
		return nil, fmt.Errorf("decode target file %s: %w", sourcePath, err)
	}

	backend := doc.Spec.Backend
	if backend == "" {
		backend = BackendHooksJSON
	}
	if backend != BackendHooksJSON && backend != BackendTSPlugin {
		return nil, fmt.Errorf("target %q: unknown backend %q", doc.Metadata.Name, backend)
	}

	def := &Def{
		Name:               doc.Metadata.Name,
		Class:              doc.Metadata.Class,
		Backend:            backend,
		ManifestDir:        doc.Spec.Discovery.ManifestDir,
		HooksKey:           doc.Spec.Discovery.HooksKey,
		MergeSemantics:     doc.Spec.Discovery.MergeSemantics,
		Events:             make(map[string]EventMapping, len(doc.Spec.Events)),
		ToolClassMap:       doc.Spec.ToolClassMap,
		Degradations:       make(map[string]Degradation, len(doc.Spec.Degradations)),
		UnknownFieldsFatal: doc.Spec.Strictness.UnknownFieldsFatal,
		PluginRootVars:     doc.Spec.Env.PluginRoot,
		Probe: ProbeSpec{
			Command:        doc.Spec.Probe.Command,
			VersionPattern: doc.Spec.Probe.VersionPattern,
		},
		Capabilities: doc.Spec.Capabilities,
		SourcePath:   sourcePath,
		RawYAML:      raw,
	}

	for name, ev := range doc.Spec.Events {
		def.Events[name] = EventMapping{
			Native:         ev.Native,
			Blocking:       ev.Blocking,
			LoopGuardField: ev.LoopGuardField,
		}
	}

	for name, deg := range doc.Spec.Degradations {
		tier, err := contract.ParseTier(deg.Tier)
		if err != nil {
			return nil, fmt.Errorf("target %q: degradation %q: %w", def.Name, name, err)
		}
		def.Degradations[name] = Degradation{
			TierRaw: deg.Tier,
			Tier:    tier,
			Matcher: deg.Matcher,
			Lossage: deg.Lossage,
		}
	}

	return def, nil
}

// LoadDir loads every target.yaml found in the immediate subdirectories
// of dir, keyed by each target's metadata name.
func LoadDir(dir string) (map[string]*Def, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("read target dir %s: %w", dir, err)
	}

	defs := make(map[string]*Def)
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		path := filepath.Join(dir, entry.Name(), "target.yaml")
		if _, err := os.Stat(path); err != nil {
			continue
		}
		def, err := Load(path)
		if err != nil {
			return nil, err
		}
		defs[def.Name] = def
	}

	if len(defs) == 0 {
		return nil, fmt.Errorf("no target definitions found in %s", dir)
	}

	return defs, nil
}

// LoadFS loads every target.yaml found in the immediate subdirectories
// of fsys (as matched by the glob "*/target.yaml"), keyed by each
// target's metadata name. It applies the same strict decoding Load
// does; the only difference is where the bytes come from. Each
// resulting Def's SourcePath is set to "embedded:<dir>/target.yaml"
// rather than a real filesystem path, since fsys is typically an
// embed.FS with no on-disk location of its own.
func LoadFS(fsys fs.FS) (map[string]*Def, error) {
	matches, err := fs.Glob(fsys, "*/target.yaml")
	if err != nil {
		return nil, fmt.Errorf("glob embedded targets: %w", err)
	}
	sort.Strings(matches)

	defs := make(map[string]*Def)
	for _, match := range matches {
		raw, err := fs.ReadFile(fsys, match)
		if err != nil {
			return nil, fmt.Errorf("read embedded target file %s: %w", match, err)
		}
		def, err := decodeTarget(raw, "embedded:"+match)
		if err != nil {
			return nil, err
		}
		defs[def.Name] = def
	}

	if len(defs) == 0 {
		return nil, fmt.Errorf("no target definitions found in embedded targets")
	}

	return defs, nil
}
