# whippletree

[![ci](https://github.com/larstonder/whippletree/actions/workflows/ci.yml/badge.svg)](https://github.com/larstonder/whippletree/actions/workflows/ci.yml)

A whippletree is the pivoting crossbar that lets one pull drive differently matched
horses in harness. This does the same for agent tools across AI coding harnesses.

Declare an agent tool's lifecycle requirements once; compile them onto multiple
AI coding harnesses at the best verifiable fidelity tier. Class-1 spine:
Claude Code + Codex CLI, plus opencode on its own backend.

- `whippletree init [<dir>]`: scaffold a new bundle (wizard on a bare TTY, flags otherwise)
- `whippletree build <bundle-dir>`: compile per-target variants
- `whippletree preflight <bundle-dir> --target <name>`: probe + tier report
- `whippletree install <bundle-dir> --target <name> [--project <dir>]`: place or guide the actual install
- `whippletree version`: build provenance and the compiled-in target definitions
- `whippletree-hook run <event> --target <name>`: hook dispatcher (invoked by harnesses, not humans)

## Performance

Dispatcher no-op dispatch (no matching handler) measured at ~4-5ms steady-state on Apple
Silicon. A real turn-end dispatch (handler execution included) measures ~15.6ms median /
~18.7ms p95, also on Apple Silicon (target <20ms).

## Authoring a tool

A tool author's deliverable is a small bundle: a contract (`plugin.json`) plus
behavior scripts. `whippletree init` scaffolds one; `whippletree build` generates
everything harness-specific from it. See [`docs/AUTHORING.md`](docs/AUTHORING.md)
for the complete field reference, the env vars and stdin shape every handler
runs under, handler best practices, and per-target notes.

### Quickstart

```
$ whippletree init my-tool --yes
whippletree: scaffolded my-tool in .../my-tool
```

That's `plugin.json` (one soft `lifecycle-signal` requirement by default), an
authored `.claude-plugin/marketplace.json`, `handlers/lifecycle-signal.sh`, a
`.gitignore` for what `build` generates, and a `README.md`. Build and preflight
it:

```
$ whippletree build my-tool
target claude-code: 1 satisfy, 0 degrade, 0 refuse, 0 absent
target codex: 1 satisfy, 0 degrade, 0 refuse, 0 absent
target opencode: 1 satisfy, 0 degrade, 0 refuse, 0 absent

$ whippletree preflight my-tool --target claude-code --assume-version 2.1.220
whippletree preflight · target claude-code (probed 2.1.220)

  lifecycle-signal  want ≥T2  got T1  SATISFY   native SessionStart

Plan: 1 satisfy, 0 degrade, 0 refuse.
```

(Real output; the `init` line's path is truncated to `.../my-tool`, eliding only the
scratch directory it ran in.)

Add a blocking gate with `whippletree init gated-tool --kinds lifecycle-signal,blocking-gate
--hard blocking-gate --yes` and you also get `handlers/blocking-gate.sh`, a plain
executable like any handler, showing the loop guard every `turn-end` handler needs:

```bash
#!/usr/bin/env bash
set -euo pipefail
# Runs on turn-end. Exit 0 to allow the turn to finish; exit 2 with a
# reason on stderr to block it. ADAPTER_STOP_ACTIVE is "true" when
# this hook already blocked once this turn: always allow then, or the
# harness loops forever.
if [ "${ADAPTER_STOP_ACTIVE:-}" = "true" ]; then
  exit 0
fi
# Replace this with your real gate condition.
exit 0
```

Add a `skill` kind alongside that `blocking-gate` and it wires `fallbackSkill`
automatically: on any target with no native stop gate, the skill carries the
step as instructions instead of refusing to install. See
[`docs/AUTHORING.md`](docs/AUTHORING.md#skills-and-instruction-fallback) for
the whole mechanism.

Install into a class-1 harness via its own plugin-marketplace command pointed at the
bundle (using the authored `marketplace.json` `init` scaffolded above):

```
claude plugin marketplace add ./my-tool
claude plugin install my-tool@my-tool-mkt

codex plugin marketplace add ./my-tool
codex plugin add my-tool@my-tool-mkt
```

`examples/kb-shaped/` is the full working reference: all four original requirement
kinds, real handlers, both class-1 targets (it predates the `skill` kind, so it has no
skill of its own). opencode's install step works differently from the marketplace
commands above; see "opencode" below.

## Commands

### `whippletree init [<dir>]`

Scaffolds a new bundle: `plugin.json`, an authored `.claude-plugin/marketplace.json`,
one handler stub per requirement kind, a `.gitignore` for what `build` generates, and
a `README.md` listing what got created and the defaults chosen. `<dir>` defaults to
`.`; intermediate directories are created as needed.

Bare on a TTY it runs an interactive wizard (bundle name, kinds, hard-required per
gate/path kind). Bare on non-TTY input, or with any flag below, it runs
non-interactively:

- `--name <s>`: bundle name, must match `^[a-z0-9-]+$` (defaults to `<dir>`'s basename).
- `--kinds <csv>`: which requirement kinds to scaffold, closed set
  `skill,blocking-gate,lifecycle-signal,observation-signal,executable-path`
  (defaults to `lifecycle-signal` alone). Scaffolding `skill` alongside
  `blocking-gate` wires `fallbackSkill` and bumps the gate to `minTier: T3`
  automatically; see [`docs/AUTHORING.md`](docs/AUTHORING.md#skills-and-instruction-fallback).
- `--hard <csv>`: which of the chosen kinds to mark `hardRequired: true`; must be a
  subset of `--kinds`. Only affects `blocking-gate` and `executable-path`;
  `lifecycle-signal`, `observation-signal`, and `skill` are always soft.
- `--yes`: skip the wizard even on a TTY and use the flag defaults above.

`init` refuses to run, and writes nothing at all, if any file it would create already
exists.

### `whippletree build`

Compiles a bundle's `plugin.json` contract (`extensions.dev.whippletree.v1`) against the
three built-in target definitions (`claude-code`, `codex`, `opencode`), writing per-target
manifests, per-target hooks files, and the vendored `.whippletree/` directory the
dispatcher reads at runtime. Those target definitions are embedded in the `whippletree`
binary itself, so this needs no `targets/` directory anywhere on disk: the CLI works the
same from any working directory, not just a checkout of this repo.

The dispatcher binary isn't committed to the repo, so on a fresh clone build it first:

```
$ go build -o examples/kb-shaped/bin/whippletree-hook ./cmd/whippletree-hook
$ go run ./cmd/whippletree build examples/kb-shaped
target claude-code: 4 satisfy, 0 degrade, 0 refuse, 0 absent
target codex: 4 satisfy, 0 degrade, 0 refuse, 0 absent
```

Every hooks-file command build just wrote invokes `<bundle>/bin/whippletree-hook`, so build also
makes sure that binary exists: if it's missing, it tries copying a `whippletree-hook` found
alongside the running `whippletree` executable (the layout a packaged release ships both
binaries in); if that's not available either, it fails with the exact command to build one
yourself:

```
go build -o <bundle>/bin/whippletree-hook ./cmd/whippletree-hook
```

Pass `--allow-missing-dispatcher` to downgrade that failure to a warning instead (useful in
a build step that provisions the binary separately). `--targets-dir <dir>` overrides the
embedded defaults with on-disk target definitions loaded from `<dir>` instead; an empty or
wrong directory is a load error. `--allow-refuse` downgrades a build-time REFUSE (see
below) from a build failure to a warning.

If any target's contract requirement lands at REFUSE (a hard-required requirement the
target cannot satisfy at all, or only below its minimum tier), `build` exits 1 and names
every refusing target/requirement pair on stderr, unless `--allow-refuse` is set.

### `whippletree preflight`

Probes an installed harness (or accepts an assumed version, for scripting) and renders a
terraform-plan-shaped report of what tier each requirement actually lands at.

```
$ go run ./cmd/whippletree preflight examples/kb-shaped --target codex --assume-version 0.146.0
whippletree preflight · target codex (probed 0.146.0)

  stop-gate             want ≥T1  got T1  SATISFY   native Stop + stop_hook_active
  session-start-signal  want ≥T2  got T1  SATISFY   native SessionStart
  file-read-signal      want ≥T4  got T2  SATISFY   matcher Bash|Edit|Write|apply_patch; misses reads in pipelines/heredocs/scripts; may double-count
  bin-reachable         want ≥T1  got T1  SATISFY   bundle channel

Plan: 4 satisfy, 0 degrade, 0 refuse.
```

Exit code is 1 if any requirement REFUSEs; otherwise `.whippletree/install-state.json` is
written recording the harness, probed version, and the achieved tier per requirement.

Each target definition declares the harness versions it was actually probed against
(`metadata.testedVersions`). When the probed version falls outside that range, preflight
says so and stops claiming the verdicts were verified:

```
whippletree preflight · target codex (probed 0.100.0)

  ! probed 0.100.0 is below the tested range >=0.144.0
  ! the verdicts below were not verified against this version

  stop-gate             want ≥T1  got T1  SATISFY   native Stop + stop_hook_active
```

This is a warning, never a refusal. A harness shipping a new version must not break every
install that day, and whippletree cannot know whether the change matters; what it can do is
stop asserting a confidence it has not earned.

### `whippletree version`

Prints build provenance and, more usefully, the target definitions compiled into this
binary:

```
$ whippletree version
whippletree v0.1.0
  commit:  7742da4
  built:   2026-08-21
  go:      go1.26.5 darwin/arm64
  contract: 1.0.0

targets (3):
  claude-code  schema 1.0.0    tested >=2.1.0
  codex        schema 1.0.0    tested >=0.144.0
  opencode     schema 1.0.0    tested >=1.18.10
```

A whippletree binary carries a probe corpus, and "which harness versions was this tested
against" is a question about the binary in your hand rather than about the repository.

### `whippletree install <bundleDir> --target <name>`

Runs the same check `preflight` does, then, on anything short of a REFUSE, performs the
actual install. What that means depends on the target's backend:

- A hooks-json target (Claude Code, Codex) has its own plugin-marketplace mechanism;
  installing into it is that harness's job, not whippletree's, so `install` prints the
  two commands from "Authoring a tool" above, pointed at this bundle.
- A ts-plugin target (opencode) has no such mechanism, so `install` places the shim
  itself: it copies the compiled `hooks/<target>.ts` into
  `<project>/.opencode/plugin/whippletree-<bundle name>.ts`, resolving the dispatcher
  placeholder to the bundle's absolute `bin/whippletree-hook` path along the way.

```
$ go run ./cmd/whippletree install examples/kb-shaped --target claude-code --assume-version 2.1.220
whippletree preflight · target claude-code (probed 2.1.220)

  stop-gate             want ≥T1  got T1  SATISFY   native Stop + stop_hook_active
  session-start-signal  want ≥T2  got T1  SATISFY   native SessionStart
  file-read-signal      want ≥T4  got T1  SATISFY   native matcher Read
  bin-reachable         want ≥T1  got T1  SATISFY   bundle channel

Plan: 4 satisfy, 0 degrade, 0 refuse.
install for claude-code is the harness's own plugin mechanism:

  claude plugin marketplace add examples/kb-shaped
  claude plugin install kb-shaped@kb-shaped-mkt
```

`--project <dir>` picks the destination project for a ts-plugin target (defaults to the
current directory; ignored for a hooks-json target, which installs nothing itself).
Exit code and `install-state.json` behavior match `preflight`. A pre-existing destination
file is only overwritten if its first line is whippletree's own generated-by marker, so a
hand-authored file at that path is left alone.

### `whippletree-hook run <event> --target <name>`

Not meant to be run by humans. This is the single command every emitted hooks file
invokes; harnesses call it directly (e.g.
`"${CLAUDE_PLUGIN_ROOT}/bin/whippletree-hook" run session-start --target claude-code`). It
resolves the bundle root from the target's plugin-root env var (falling back to its own
grandparent directory), normalizes the harness's native stdin payload, and executes the
matching handler(s) declared in the contract.

## Handler convention

See [`docs/AUTHORING.md`](docs/AUTHORING.md) for the full stdin JSON shape, the
env-var table, and the exit-code contract every handler runs under.

## Conventions this adds

Three additions this implementation makes relative to `harness-adapter.architecture.md`:

1. Each requirement gains a `handler` field: a path, relative to the bundle root, to the
   executable the dispatcher runs for that behavior. `executable-path` requirements gain a
   `path` field instead (there's nothing to invoke, only a binary to check for).
2. The handler convention above (stdin/env/exit codes) is new; the architecture doc
   described the contract and target definitions but not the wire format a handler sees.
3. Compiled bundles carry a vendored `.whippletree/` directory: `contract.json` (the
   normalized contract) and `targets/<name>.yaml` (the exact target definition used at
   build time). This is what the dispatcher reads at runtime, so it never has to
   re-resolve the contract or reload target YAMLs the author might have changed since
   build.

## opencode

opencode has no blocking stop event at all: there is no native primitive `turn-end` can
map to on this target. A hooks-json target blocks a whole turn; opencode can only ever
block a single tool call, and even that isn't a native gate. It's whippletree's own
convention layered on top: the compiled shim throws a JavaScript error when the
dispatcher exits 2, that error fails the one `tool.execute` call it was thrown from, and
the agent loop continues past it. There is nothing on this target an author can point a
`hardRequired` stop-gate at and have it actually stop anything.

The backend is different too. `targets/opencode/target.yaml` sets `backend: ts-plugin`
instead of the hooks-json default, so `whippletree build` writes `hooks/opencode.ts`
rather than a manifest pair: an in-process TypeScript plugin shim, zero npm imports,
that spawns the compiled dispatcher directly via `node:child_process`'s `spawnSync`.

Because the stop-gate requirement genuinely cannot be satisfied here, `examples/kb-shaped`
refuses preflight and install on opencode exactly as shipped, by design: its `stop-gate`
requirement is `hardRequired: true`, and the best tier opencode can ever land it at is
Absent, never Satisfy or Degrade.

```
$ go run ./cmd/whippletree preflight examples/kb-shaped --target opencode --assume-version 1.18.10
whippletree preflight · target opencode (probed 1.18.10)

  stop-gate             want ≥T1  got —  REFUSE    no native mapping for turn-end on this target
  session-start-signal  want ≥T2  got T1  SATISFY   native event:session.created
  file-read-signal      want ≥T4  got T1  SATISFY   native matcher read
  bin-reachable         want ≥T1  got T1  SATISFY   installer-resolved absolute path

Plan: 3 satisfy, 0 degrade, 1 refuse.
```

Soften that one field to `hardRequired: false` and the requirement lands Absent instead
of refusing; `install` then places the shim rather than stopping:

```
$ go run ./cmd/whippletree install my-tool --target opencode --project my-project --assume-version 1.18.10
whippletree preflight · target opencode (probed 1.18.10)

  stop-gate             want ≥T1  got —  ABSENT    no native mapping for turn-end on this target
  session-start-signal  want ≥T2  got T1  SATISFY   native event:session.created
  file-read-signal      want ≥T4  got T1  SATISFY   native matcher read
  bin-reachable         want ≥T1  got T1  SATISFY   installer-resolved absolute path

Plan: 3 satisfy, 0 degrade, 0 refuse, 1 absent.
```

The shim lands at `my-project/.opencode/plugin/whippletree-my-tool.ts` with the
dispatcher path baked in absolute (there's no plugin-root environment variable on this
target to resolve it from at runtime, the way `${CLAUDE_PLUGIN_ROOT}` works for Claude
Code). See "`whippletree install`" under Commands for the overwrite-protection rule.

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

## Scope

This is the class-1 spine, Claude Code and Codex CLI, at whatever tier (T1/T2) each
requirement can verifiably reach on those two harnesses today, plus opencode as a third
target on its own ts-plugin backend (see "opencode" above). Out of scope:

- Cursor and Zed targets
- the T4 (observer) tier
- the conformance kit
- uninstall and upgrade flows
- Windows (see below)

This implementation is a slice of a larger architecture (a four-tier fidelity ladder
across harness classes); the remaining tiers and targets land in later slices.

## Windows

**whippletree does not support Windows yet.** Authoring a bundle works, but running one
does not, and the gap is in the bundle format rather than in any single bug:

- Every handler `init` scaffolds is a `#!/usr/bin/env bash` script. Windows has no shebang
  support, so the handler cannot be executed no matter how it is invoked.
- The command compiled into each hooks file is `"${PLUGIN_ROOT}/bin/whippletree-hook" run
  ...`: POSIX shell variable syntax, forward slashes, and a binary name with no `.exe`.

Closing this means giving a requirement a per-platform handler, the way GitHub Copilot
CLI's own hooks file splits `bash` and `powershell`. That is a change to the contract
surface, so it is deliberately being decided before `dev.whippletree.v1` is frozen at
v1.0 rather than patched in now.

The dispatcher itself is Windows-clean: it no longer applies a POSIX mode check that
Windows can never satisfy, so a bundle whose handlers are real executables will run. Tests
that depend on shell-script handlers skip on Windows rather than reporting a false pass.
