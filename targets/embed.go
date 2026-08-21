// Package targets embeds the repo's built-in target definitions into the
// whippletree binary, so the CLI has targets to load when run from
// outside a whippletree checkout.
package targets

import "embed"

//go:embed */target.yaml
var FS embed.FS
