# whippletree-authoring

A pure-knowledge whippletree bundle: one skill that teaches an agent how
to build whippletree bundles. Built and shipped with whippletree itself.

Install into Claude Code from the repo root marketplace:

    /plugin marketplace add larstonder/whippletree
    /plugin install whippletree-authoring@whippletree-mkt

Or build and preflight it with whippletree directly:

    whippletree build bundles/authoring --allow-missing-dispatcher
    whippletree preflight bundles/authoring --target claude-code

The skill's `references/AUTHORING.md` is a copy of `docs/AUTHORING.md`;
`go test ./bundles/` fails if the two drift.
