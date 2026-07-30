package target

import "github.com/larstonder/whippletree/internal/contract"

// Def is the flattened, in-memory representation of a target's
// target.yaml, populated from the nested apiVersion/kind/metadata/spec
// YAML document.
type Def struct {
	Name               string
	Class              int
	ManifestDir        string
	HooksKey           string
	MergeSemantics     string
	Events             map[string]EventMapping
	ToolClassMap       map[string]*string
	Degradations       map[string]Degradation
	UnknownFieldsFatal bool
	PluginRootVars     []string
	Probe              ProbeSpec
	Capabilities       map[string]bool

	// SourcePath is the filesystem path Load read this Def from. It is
	// bookkeeping populated after YAML decoding, not part of the
	// target.yaml schema itself.
	SourcePath string
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
