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
    this project or the open source license(s) involved.
```

## Before you open a pull request

```
gofmt -l .            # must print nothing
go vet ./...
GOOS=windows go vet ./...
go test ./... -race
go build -o examples/kb-shaped/bin/whippletree-hook ./cmd/whippletree-hook
go run ./cmd/whippletree build examples/kb-shaped --targets-dir targets --allow-refuse
git status            # examples/kb-shaped must come back clean
(cd tools/docsgen && go run .)
```

`go build`, `go vet` and the race-enabled tests run in CI on Linux, macOS and
Windows. The rest runs on Linux alone: `gofmt`, the Windows vet, a check that
every Go file carries an SPDX licence header, a build and test against the Go
version `go.mod` declares, a cross-compile of the six GOOS/GOARCH pairs a
release ships, a docsgen run that resolves every internal link on the built
site, and the `examples/kb-shaped` rebuild above. That last one fails unless
`whippletree build` still reproduces the committed artifacts byte-for-byte,
which is what catches a silent change to what every user's bundle looks like.
If it fails, look before you regenerate.

Every Go file needs the two-line SPDX header, `SPDX-FileCopyrightText` and
`SPDX-License-Identifier`. [`CLAUDE.md`](CLAUDE.md) has the exact form, along
with the conventions worth knowing before your first change. Comment what the
code cannot say, and put the rationale for a change in its commit message rather
than in a source file where it will rot.

## End-to-end tests

`test/e2e/run-codex.sh`, `test/e2e/run-claude.sh`, `test/e2e/run-opencode.sh` and
`test/e2e/run-copilot.sh` each install the `examples/kb-shaped` bundle into a fresh
harness home under `mktemp -d` (`CODEX_HOME`, `CLAUDE_CONFIG_DIR`, an XDG-isolated set
of dirs, and `COPILOT_HOME` respectively) and drive a real run against the installed
CLI. Every script asserts on a marker file the example's handlers write to;
`run-claude.sh` also asserts on `preflight` output and on the dispatcher's own
stdout, and `run-opencode.sh` on `preflight` and `install`. They are standalone
bash, outside `go test`, because they need the real CLI on the machine.

CI runs the first three nightly rather than per-push (`.github/workflows/e2e.yml`),
installing each harness from npm first. `run-copilot.sh` is excluded: the events it
asserts on only fire once an agent takes a turn, so unlike the others it needs a real
login, and copilot installs from Homebrew rather than npm. A failed nightly fails the run and
also opens or comments on a drift issue, because a harness that has moved on is a
finding about `metadata.testedVersions` and not only a broken commit.

```bash
test/e2e/run-codex.sh
test/e2e/run-claude.sh
test/e2e/run-opencode.sh
test/e2e/run-copilot.sh   # needs a real login, see above
```

The first three run unauthenticated, copying no credentials into the isolated home.
`run-codex.sh` and `run-claude.sh` rest on session-start firing before the harness makes
any auth or model call, so each tolerates the 401 or "not logged in" that follows and
still proves the hook wiring. opencode meets no auth failure, because it serves its
default hosted model anonymously, but `run-opencode.sh` ignores the run's exit code
too and rests its assertions on the marker file. `run-codex.sh` proves `SessionStart` fires end to end
through the compiled hooks file and the dispatcher. `run-claude.sh` proves the same,
plus that it fires exactly once, which confirms `hooks/hooks.json` is never emitted
alongside the per-target hooks file (Claude Code merges that file additively, so its
presence would double-fire every hook). `run-opencode.sh` proves the REFUSE-by-design
behavior that [`docs/opencode.md`](docs/opencode.md) sets out, then softens the bundle and
proves the compiled shim installs and fires session-start exactly once through a real
`opencode run`.

Some of the PASS output from the last verified run, against `codex-cli 0.144.5`,
`claude 2.1.220` and `opencode 1.18.10`:

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

Every e2e script prints a `harness=<name> version=<probed> date=<iso>` line before it
does anything else, so the test output records which upstream version a given PASS was
measured against, and when. Those lines back the entries in
[`MAINTENANCE.md`](MAINTENANCE.md).

## Adding or changing a target

Target definitions are claims about how a real harness behaves, so you establish
them by probing that harness rather than reading its documentation.
[`docs/opencode-probe-findings.md`](docs/opencode-probe-findings.md) and
[`docs/skill-discovery-probe.md`](docs/skill-discovery-probe.md) show the method:
a `mktemp -d` sandbox, an isolated harness home, and an honest record of anything
the probe could not verify.

`metadata.testedVersions` is the claim that a definition was exercised against a
version range. Do not widen it for a version nobody ran.

## Changing the contract surface

`dev.whippletree.v1` is versioned by `contractVersion`, and
`contract.SupportedContractVersion` is the highest a build will accept. Additive
changes are minor bumps. Changing the meaning of an existing field is a major
bump: open an issue before you write the code.

## Licence

Contributions are accepted under Apache-2.0, the licence the project ships
under. See [`LICENSE`](LICENSE), and [`TRADEMARK.md`](TRADEMARK.md) for what the
licence does not cover.
