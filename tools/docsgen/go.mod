// Separate module on purpose: the markdown renderer is a build-time tool, and
// keeping it out of the main module means the shipped binaries still depend on
// exactly one thing.
module whippletree.dev/tools/docsgen

go 1.22

require github.com/yuin/goldmark v1.8.5
