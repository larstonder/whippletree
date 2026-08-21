<picture>
  <source media="(prefers-color-scheme: dark)" srcset="site/brand/wordmark_white.svg">
  <img src="site/brand/wordmark_black.svg" alt="Whippletree" width="300">
</picture>

[![ci](https://github.com/larstonder/whippletree/actions/workflows/ci.yml/badge.svg)](https://github.com/larstonder/whippletree/actions/workflows/ci.yml)

**[whippletree.dev](https://whippletree.dev)**

Write one `plugin.json` saying what your agent tool needs from a coding harness:
run this handler when a session starts, block the turn if a check fails, tell me
when a file is read, make this binary reachable. `whippletree build` compiles that
into each harness's own native hook format, so you write the requirement once
instead of three times.

Harnesses support different slices of that lifecycle, and none of them tell you
which. Whippletree probes the harness you have installed and reports the tier
every requirement actually reaches before it writes anything:

```
  stop-gate             want ≥T1  got T1  SATISFY   native Stop + stop_hook_active
  file-read-signal      want ≥T4  got T2  SATISFY   matcher Bash|Edit|Write|apply_patch;
                                                    misses reads in pipelines and heredocs
```

A hard requirement the harness cannot meet refuses to install rather than
degrading quietly.

Targets today: Claude Code, Codex CLI, GitHub Copilot CLI, and opencode.

The name is the mechanism: a whippletree is the pivoting crossbar that lets a
single pull drive differently matched horses.

- `whippletree init [<dir>]`: scaffold a new bundle (wizard on a bare TTY, flags otherwise)
- `whippletree build <bundle-dir>`: compile per-target variants
- `whippletree preflight <bundle-dir> --target <name>`: probe + tier report
- `whippletree install <bundle-dir> --target <name> [--project <dir>]`: place or guide the actual install
- `whippletree version`: build provenance and the compiled-in target definitions
- `whippletree-hook run <event> --target <name>`: hook dispatcher (invoked by harnesses, not humans)

## Install

```
go install whippletree.dev/cmd/whippletree@latest
go install whippletree.dev/cmd/whippletree-hook@latest
```

Install both, to the same directory. `build` provisions a bundle's
`bin/whippletree-hook` by copying the one beside the running `whippletree` binary, so
the pair has to travel together. The [signed release archives][releases] contain both.

## Quickstart

```
$ whippletree init my-tool --yes
whippletree: scaffolded my-tool in ./my-tool

$ whippletree build my-tool
target claude-code: 1 satisfy, 0 degrade, 0 refuse, 0 absent
target codex: 1 satisfy, 0 degrade, 0 refuse, 0 absent
target opencode: 1 satisfy, 0 degrade, 0 refuse, 0 absent

$ whippletree preflight my-tool --target claude-code
whippletree preflight · target claude-code (probed 2.1.238)

  lifecycle-signal  want ≥T2  got T1  SATISFY   native SessionStart

Plan: 1 satisfy, 0 degrade, 0 refuse.
```

You edit `plugin.json` and the handler scripts. Everything else is generated: per-target
manifests, per-target hooks files, and the vendored `.whippletree/` the dispatcher reads
at runtime. See [Authoring a bundle][authoring] for the field reference and the wire
format handlers see.

## Commands

| command | does |
|---|---|
| `whippletree init [<dir>]` | scaffold a bundle; interactive wizard on a bare TTY |
| `whippletree build <dir>` | compile per-target artifacts |
| `whippletree preflight <dir> --target <t>` | probe the harness, report the tier each requirement reaches |
| `whippletree install <dir> --target <t>` | preflight, then install |
| `whippletree version` | build provenance and the compiled-in target definitions |
| `whippletree-hook run <event> --target <t>` | the dispatcher; harnesses invoke this, not you |

<details>
<summary><b>Flags</b></summary>

`init`

| flag | effect |
|---|---|
| `--name <s>` | bundle name, must match `^[a-z0-9-]+$` (default: `<dir>` basename) |
| `--kinds <csv>` | which requirement kinds to scaffold (default: `lifecycle-signal`) |
| `--hard <csv>` | which of those to mark `hardRequired`; must be a subset of `--kinds` |
| `--yes` | skip the wizard even on a TTY |

`build`

| flag | effect |
|---|---|
| `--targets-dir <dir>` | load target definitions from disk instead of the embedded set |
| `--allow-missing-dispatcher` | downgrade a missing `bin/whippletree-hook` to a warning |
| `--allow-refuse` | downgrade a build-time REFUSE to a warning |

`preflight` and `install`

| flag | effect |
|---|---|
| `--target <name>` | which target to check against (required) |
| `--assume-version <v>` | skip the probe and assume this harness version |
| `--targets-dir <dir>` | as above |
| `--project <dir>` | install destination for a ts-plugin target (default: cwd) |

</details>

Scaffolding `skill` alongside `blocking-gate` wires `fallbackSkill` and bumps the gate to
`minTier: T3` automatically. `init` writes nothing at all if any file it would create
already exists, and `install` only overwrites a destination whose first line is
Whippletree's own generated-by marker.

## Performance

Dispatcher no-op dispatch (no matching handler) measured at ~4-5ms steady-state on Apple
Silicon. A real turn-end dispatch (handler execution included) measures ~15.6ms median /
~18.7ms p95, also on Apple Silicon (target <20ms).

## The tier ladder

Where a requirement lands is what the harness can actually carry.

| tier | meaning |
|---|---|
| **T1** | native. The harness has a real hook, enforced by the harness itself. |
| **T2** | degraded. Approximated through a coarser mechanism, with the lossage stated. |
| **T3** | compiled to instructions. The model is told to run the step and usually will. |
| **T4** | observer. Reserved, not implemented. |

Comparing that against the requirement's declared `minTier` gives the verdict.

| verdict | meaning | exit |
|---|---|---|
| `SATISFY` | reached the declared minimum or better | 0 |
| `DEGRADE` | below the minimum, but the requirement is soft | 0 |
| `REFUSE` | below the minimum and hard-required | 1 |
| `ABSENT` | the target has no mechanism at all, requirement is soft | 0 |

A target definition also declares the harness versions it was actually probed against.
Probe something outside that range and preflight says so rather than claiming a
confidence it has not earned. That is a warning, never a refusal: a harness shipping a
new version must not break every install that day.

## Targets

| target | backend | install mechanism | tested against |
|---|---|---|---|
| `claude-code` | `hooks-json` | the harness's own plugin marketplace | `>=2.1.0` |
| `codex` | `hooks-json` | the harness's own plugin marketplace | `>=0.144.0` |
| `copilot` | `hooks-json` | the harness's own plugin marketplace | `>=1.0.80` |
| `opencode` | `ts-plugin` | Whippletree places the shim in `.opencode/plugin/` | `>=1.18.10` |

Definitions are embedded in the binary, so the CLI works from any directory. Two of them
cannot enforce a turn-end gate, for different reasons: opencode has no blocking stop
event at all, and Copilot has a `Stop` hook whose exit code it declines to act on. A hard
`turn-end` gate REFUSEs on both rather than pretending. See [opencode notes][opencode]
and [Copilot probe findings][copilot].

## Docs

| | |
|---|---|
| [Authoring a bundle][authoring] | contract fields, handler wire format, per-target notes |
| [opencode notes][opencode] | why it needs its own backend, and what it cannot do |
| [Copilot probe findings][copilot] | what was measured on Copilot CLI, and why `Stop` is not a gate |
| [Maintenance][maintenance] | the upkeep bet, and the versions each target was probed at |
| [Probe findings][probes] | what each harness actually does, established empirically |
| [Contributing][contributing] · [Security][security] · [Trademark][trademark] | |

[releases]: https://github.com/larstonder/whippletree/releases
[authoring]: docs/AUTHORING.md
[maintenance]: MAINTENANCE.md
[probes]: docs/
[opencode]: docs/opencode.md
[copilot]: docs/copilot-probe-findings.md
[contributing]: CONTRIBUTING.md
[security]: SECURITY.md
[trademark]: TRADEMARK.md

## Scope

This is the class-1 spine, Claude Code and Codex CLI, at whatever tier (T1/T2) each
requirement can verifiably reach on those harnesses today, plus opencode on its own
ts-plugin backend (see "opencode" above). Out of scope:

- Cursor and Zed targets
- the T4 (observer) tier
- the conformance kit
- uninstall and upgrade flows
- running a bundle on Windows, partly (see below)

This implementation is a slice of a larger architecture (a four-tier fidelity ladder
across harness classes); the remaining tiers and targets land in later slices.

## Windows

**Partly, and the boundary is precise.** Authoring, building and installing a bundle
work. Whether a bundle *runs* depends on what its handlers are written in.

A requirement may declare `handlerWindows` alongside `handler`, and the dispatcher picks
per platform at dispatch rather than at build, so one compiled bundle stays valid
everywhere. The command baked into each hooks file names the dispatcher without a
`.exe`, which resolves correctly on Windows across every spawn path a harness plausibly
uses — measured, in [`docs/windows-probe-findings.md`](docs/windows-probe-findings.md),
along with the reason shipping both filenames is the arrangement to avoid.

Handlers are limited to what the loader launches from a bare path: `.exe`, `.com`,
`.bat`, `.cmd`. The dispatcher execs the handler directly, with no interpreter, so
`.ps1` is not among them — wrap it in a `.cmd`. That is refused at build time rather
than left to fail open at dispatch, because a spawn failure fails open and a
hard-required gate would otherwise stop enforcing silently.

What is still missing:

- `whippletree init` scaffolds only `#!/usr/bin/env bash` handlers and never sets
  `handlerWindows`, so a scaffolded bundle carries nothing on Windows until you add one.
- `preflight` is platform-blind by construction: it reports what a *harness* can carry,
  and a harness is not a machine. A requirement with no `handlerWindows` still reports
  SATISFY, then gets skipped at dispatch.
- The T3 instruction fallback compiles a POSIX shell snippet, so it reads wrongly there.

Tests that depend on shell-script handlers skip on Windows rather than reporting a false
pass. Tracked as issues [#1](https://github.com/larstonder/whippletree/issues/1) and
[#17](https://github.com/larstonder/whippletree/issues/17).

## Licence and the name

The code is [Apache-2.0](LICENSE). The name and logo are not: Apache-2.0
section 6 reserves trademarks explicitly, and [`TRADEMARK.md`](TRADEMARK.md) says
what that means in practice. Fork freely; if you change how the verdicts behave,
call it something else.

Contributions are under the [DCO](CONTRIBUTING.md), not a CLA. Vulnerability
reports go through [`SECURITY.md`](SECURITY.md).
