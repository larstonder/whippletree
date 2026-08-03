// Package targets embeds the repo's built-in target definitions
// (claude-code, codex, opencode) into the whippletree binary, so the
// CLI has a set of targets to load even when run from outside the
// whippletree repo (where a cwd-relative "targets" directory would
// not exist).
package targets

import "embed"

//go:embed */target.yaml
var FS embed.FS
