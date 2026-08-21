# Security policy

## Reporting

Report vulnerabilities privately through GitHub's advisory form:
https://github.com/larstonder/whippletree/security/advisories/new

Please do not open a public issue for anything exploitable. This is a
single-maintainer project, so expect an acknowledgement within a few days rather
than a few hours.

## Supported versions

Pre-1.0. Only the latest release gets fixes.

## What is in scope

Whippletree compiles a bundle's declared contract into a harness's native hook
configuration, and `whippletree-hook` executes that bundle's handlers at
runtime. The interesting boundary is a bundle you did not author.

In scope:

- A bundle that makes `whippletree-hook` execute anything outside its own bundle
  root. The vendored `.whippletree/contract.json` is untrusted input: it is read
  without re-validation, so containment is enforced again at dispatch time. If
  you can defeat that, it is a vulnerability.
- A bundle that makes `whippletree build` or `whippletree install` write outside
  the bundle or the declared skill destination.
- A handler that can hang the harness indefinitely. Handler execution is bounded
  and fail-open by design.
- Anything that causes Whippletree to report `SATISFY` for a requirement the
  target does not actually satisfy. The verdicts are the product; a false one is
  a real defect even though nothing crashes.

Out of scope:

- A handler you authored doing something harmful. Handlers are arbitrary
  executables and run with your privileges by design; that is the feature.
- Vulnerabilities in the harnesses themselves (Claude Code, Codex CLI,
  opencode). Report those upstream.
- Running a bundle from a source you do not trust. Installing a bundle is
  equivalent to running its code.

## Verifying a release

Releases are signed with cosign keyless and carry SLSA build provenance. The
exact verification commands are in each release's notes.
