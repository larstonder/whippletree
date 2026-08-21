package target

import "github.com/larstonder/whippletree/internal/contract"

// Known Backend values. hooks-json is the zero-value default: a
// target.yaml that omits backend entirely loads as hooks-json.
const (
	BackendHooksJSON = "hooks-json"
	BackendTSPlugin  = "ts-plugin"
)

// Def is the flattened, in-memory representation of a target's
// target.yaml, populated from the nested apiVersion/kind/metadata/spec
// YAML document.
type Def struct {
	Name  string
	Class int

	// SchemaVersion is metadata.schemaVersion: the version of the
	// target.yaml schema this file is written against.
	SchemaVersion string

	// TestedVersions is metadata.testedVersions: the range of harness
	// versions this definition has actually been probed against, as a
	// constraint string (see ParseConstraint). It is the currency claim
	// a target definition makes, and preflight checks the probed
	// version against it.
	TestedVersions string

	// Backend selects the compiler path used to emit this target's
	// artifacts: hooks-json writes a native JSON hooks file, ts-plugin
	// writes an in-process TypeScript shim.
	Backend        string
	ManifestDir    string
	Events         map[string]EventMapping
	ToolClassMap   map[string]*string
	Degradations   map[string]Degradation
	PluginRootVars []string
	Probe          ProbeSpec
	Capabilities   map[string]bool
	SkillChannel   SkillChannel

	// SourcePath is the filesystem path Load read this Def from, or
	// "embedded:<name>/target.yaml" for a Def LoadFS produced from the
	// embedded targets package. It is bookkeeping populated after YAML
	// decoding, not part of the target.yaml schema itself.
	SourcePath string

	// RawYAML is the exact bytes the Def was decoded from, set by
	// Load, LoadDir, and LoadFS alike. Vendoring writes this directly
	// rather than re-reading SourcePath from disk, which is what makes
	// vendoring work for embedded (non-disk-backed) Defs.
	RawYAML []byte
}

// EventMapping describes how an whippletree primitive event maps onto a
// target's native hook event.
type EventMapping struct {
	Native         string
	Blocking       bool
	LoopGuardField string
}

// Degradation records a known lossy fallback a target uses to approximate
// a capability it cannot implement natively.
type Degradation struct {
	TierRaw string
	Tier    contract.Tier
	Matcher string
	Lossage string
}

// ProbeSpec describes how to detect the target's installed version.
type ProbeSpec struct {
	Command        []string
	VersionPattern string
}

// SkillChannel describes how a target receives a bundle's skills.
// plugin-dir: skills travel inside the bundle via the plugin install
// channel, nothing is placed separately. copy-dir: install copies the
// built skill variant into Dest. An empty Kind means the target has no
// skill channel and skill requirements land Absent.
type SkillChannel struct {
	Kind string
	Dest string
}
