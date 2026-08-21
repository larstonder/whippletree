# Contributing to Whippletree

## Sign your commits (DCO)

Whippletree uses the Developer Certificate of Origin rather than a CLA. There is
nothing to sign up for and no copyright to assign: you certify the origin of
what you contribute by adding a `Signed-off-by` line to each commit.

```
git commit -s -m "your message"
```

which appends:

```
Signed-off-by: Your Name <your.email@example.com>
```

Use your real name and an address you can be reached at. The full text you are
certifying:

```
Developer Certificate of Origin
Version 1.1

Copyright (C) 2004, 2006 The Linux Foundation and its contributors.

Everyone is permitted to copy and distribute verbatim copies of this
license document, but changing it is not allowed.


Developer's Certificate of Origin 1.1

By making a contribution to this project, I certify that:

(a) The contribution was created in whole or in part by me and I
    have the right to submit it under the open source license
    indicated in the file; or

(b) The contribution is based upon previous work that, to the best
    of my knowledge, is covered under an appropriate open source
    license and I have the right under that license to submit that
    work with modifications, whether created in whole or in part
    by me, under the same open source license (unless I am
    permitted to submit under a different license), as indicated
    in the file; or

(c) The contribution was provided directly to me by some other
    person who certified (a), (b) or (c) and I have not modified
    it.

(d) I understand and agree that this project and the contribution
    are public and that a record of the contribution (including all
    personal information I submit with it, including my sign-off) is
    maintained indefinitely and may be redistributed consistent with
    this project or the open source license(s) involved.```

## Before you open a pull request

```
gofmt -l .            # must print nothing
go vet ./...
GOOS=windows go vet ./...
go test ./... -race
```

CI runs all of that on Linux, macOS and Windows, plus a cross-compile of every
platform a release ships, a build against the Go version `go.mod` declares, and
a check that `whippletree build` still reproduces the committed
`examples/kb-shaped` artifacts byte-for-byte. That last one catches a silent
change to what every user's bundle looks like, so if it fails, look before you
regenerate.

Conventions worth knowing before your first change are in
[`CLAUDE.md`](CLAUDE.md) — in particular, comment what the code cannot say, and
put the rationale for a change in its commit message rather than in a source
file where it will rot.

## End-to-end tests

`test/e2e/run-codex.sh`, `test/e2e/run-claude.sh`, and `test/e2e/run-opencode.sh` install
the `examples/kb-shaped` bundle into a fresh, isolated harness home (`mktemp -d`,
`CODEX_HOME` / `CLAUDE_CONFIG_DIR` / an XDG-isolated set of dirs respectively) and drive
one real, unauthenticated turn against the installed `codex`, `claude`, and `opencode`
CLIs, then assert on a marker file the example's handlers write to. They are standalone
bash, not wired into `go test`; they're the environment-dependent, real-harness
conformance layer.

CI runs them nightly rather than per-push (`.github/workflows/e2e.yml`), installing each
harness from npm first. A failure there opens or comments on a drift issue instead of
failing the build: a harness that has moved on is a finding about
`metadata.testedVersions`, not a broken commit. Unit tests, vet, `gofmt` and an
artifacts-reproduce check run per-push on Linux, macOS and Windows.

```bash
test/e2e/run-codex.sh
test/e2e/run-claude.sh
test/e2e/run-opencode.sh
```

All three scripts run fully unauthenticated (no credentials are copied into the isolated
home): the session-start signal is verified to fire before the harness makes any
auth/model call, so the scripts tolerate the resulting 401/"not logged in" failure (or,
for opencode, an anonymous hosted-model call that succeeds on its own) and assert only on
the marker file. `run-codex.sh` proves `SessionStart` fires end to end through the
compiled hooks file and the dispatcher; `run-claude.sh` proves the same, plus that it
fires exactly once, confirming `hooks/hooks.json` is never emitted alongside the
per-target hooks file (Claude Code merges that file additively, so its presence would
double-fire every hook). `run-opencode.sh` proves the REFUSE-by-design behavior from
"opencode" above, then softens the bundle and proves the compiled shim installs and
fires session-start exactly once through a real `opencode run`.

Actual PASS output from the last verified run, against `codex-cli 0.144.5`,
`claude 2.1.220`, and `opencode 1.18.10`:

```
PASS: session-start fired on codex
```

```
PASS: session-start fired exactly once on claude
```

```
PASS: preflight refuses the hard stop gate on opencode
PASS: install placed the plugin shim at .opencode/plugin/
PASS: session-start fired exactly once on opencode
```

Every e2e script also prints a `harness=<name> version=<probed> date=<iso>` line before
it does anything else, so the exact upstream version and date a given PASS was measured
against is always available by grepping test output rather than trusting memory. That
line is also what backs the maintenance log's entries, see `MAINTENANCE.md`.

## Adding or changing a target

Target definitions are claims about how a real harness behaves, so they are
established by probing that harness, not by reading its documentation. See
`docs/opencode-probe-findings.md` and `docs/skill-discovery-probe.md` for the
method: a `mktemp -d` sandbox, an isolated harness home, and an honest record of
anything the probe could not verify.

`metadata.testedVersions` is the claim that a definition was actually exercised
against a version range. Do not widen it for a version nobody ran.

## Changing the contract surface

`dev.whippletree.v1` is versioned by `contractVersion`, and
`contract.SupportedContractVersion` is what a build will accept. Additive
changes are minor bumps; anything that changes the meaning of an existing field
is a major bump and needs discussion first. Open an issue before writing the
code.

## Licence

Contributions are accepted under Apache-2.0, the licence the project ships
under. See [`LICENSE`](LICENSE), and [`TRADEMARK.md`](TRADEMARK.md) for what the
licence does not cover.
