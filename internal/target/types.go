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
	// Backend selects the compiler path used to emit this target's
	// artifacts: hooks-json writes a native JSON hooks file, ts-plugin
	// writes an in-process TypeScript shim.
	Backend            string
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
