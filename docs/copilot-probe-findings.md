# GitHub Copilot CLI probe findings (empirical)

Probe run: 2026-08-21, macOS (darwin 25.5.0, arm64).

| Item | Value |
|---|---|
| Install | `/opt/homebrew/bin/copilot` |
| Version reported | `GitHub Copilot CLI 1.0.80.` (note the trailing period) |
| Sandboxing | `mktemp -d` per probe, plugins mounted with `--plugin-dir` |

Every probe mounted its plugin with the global `--plugin-dir` flag rather than
installing one, so nothing was written to a marketplace or to the caller's
plugin state. Runs were authenticated: unlike the codex probe, these consumed
real model calls, because the questions about `Stop` and `PreToolUse` can only
be answered by an agent that actually runs a turn.

## 1. The headline: no new backend is needed

**Copilot CLI accepts the hooks file whippletree already emits, unchanged.**

The documented Copilot hook entry is flat — `{type, matcher, bash, powershell,
command, timeoutSec}` under a `{"version": 1, "hooks": {...}}` wrapper — and on
that basis a `hooksFormat` discriminator looked unavoidable. It is not. A file
containing the bare `{"hooks": {...}}` wrapper with Claude's *nested*
`{matcher, hooks: [{type, command}]}` entries, no `version` key and no
`timeoutSec`, registered and fired:

```json
{"hooks":{"SessionStart":[{"hooks":[{"type":"command","command":"\"${PLUGIN_ROOT}/h.sh\" session-start"}]}]}}
```

That is byte-identical in shape to what `internal/compile/hooksfile.go` writes
today. So `backend: hooks-json` carries Copilot with no emitter change, and the
`spec.discovery.hooksFormat` field sketched for this target is not needed.

## 2. Which manifest directory wins

Documented search order is `.plugin/plugin.json`, `plugin.json`,
`.github/plugin/plugin.json`, `.claude-plugin/plugin.json`.

Probed directly, because it decides whether whippletree's bundles work at all: a
plugin was built with **both** a root `plugin.json` carrying no `hooks` key —
which is exactly what every whippletree bundle ships today — and a
`.plugin/plugin.json` that did carry one. The hook fired, so `.plugin` wins.

This is the trap the target definition has to avoid. Hooks are **not**
auto-discovered: the manifest must name the file. Had the root manifest won,
Copilot would have loaded the bundle and silently registered zero hooks.
Hence `manifestDir: .plugin`.

## 3. Events

Every documented event name was registered at once, in PascalCase, and a
one-tool session was run.

| event | fired in a simple session |
|---|---|
| `SessionStart` | yes |
| `SessionEnd` | yes |
| `UserPromptSubmit` | yes |
| `PreToolUse` | yes |
| `PostToolUse` | yes |
| `Stop` | yes |
| `PostToolUseFailure`, `SubagentStart`, `SubagentStop`, `PreCompact`, `ErrorOccurred`, `Notification` | not exercised |

The bottom row is "this session did not provoke them", not "unsupported". The
target definition declares only the five primitives whippletree already binds,
all of which are measured above. `PostCompact` is not a documented Copilot event
and did not fire.

An unrecognised event key is **ignored**, not fatal: a file declaring
`NotARealEvent` alongside `SessionStart` still fired `SessionStart`. Hence
`strictness.unknownFieldsFatal: false`.

## 4. The finding that shapes the target: `Stop` does not block

Copilot's exit-code semantics are not Claude's. Measured, with a positive
control so that "did not block" is a result rather than an absence of evidence:

| event | handler exit 2 | outcome |
|---|---|---|
| `PreToolUse` | yes | **blocked.** Copilot printed `Denied by preToolUse hook: hook exited with code 2` and the model reported it could not run the command |
| `Stop` | yes | **did not block.** The handler fired once, wrote a message to stderr demanding more work, and the session ended anyway |

The `Stop` probe was designed to make a block visible: the handler would exit 2
on its first three firings and the session would have had to continue for the
second to happen. It fired exactly once, with `stop_hook_active=false`.

So `turn-end` is declared `blocking: false` on this target. A hard-required
`blocking-gate` at `turn-end` therefore **REFUSES** on Copilot, or compiles to
T3 instructions when the requirement carries a `fallbackSkill` — the same
verdict opencode gets, for a different reason.

Note that Copilot *does* deliver `stop_hook_active` in the `Stop` payload. The
field Claude uses to break a stop-hook loop is present on a harness that has no
such loop, so it is not evidence of blocking and `loopGuardField` is
deliberately not set.

## 5. Payload and tool names

The payload is snake_case and matches what `internal/dispatch/normalize.go`
already parses, with no adapter:

```json
{"hook_event_name":"SessionStart","session_id":"...","timestamp":"...","cwd":"...","source":"new","initial_prompt":"say hi"}
```

`PreToolUse`/`PostToolUse` add `tool_name` and `tool_args`. A census over one
session doing a read, a modify, a create and a shell command:

| operation | `tool_name` |
|---|---|
| read a file | `Read` |
| modify an existing file | `Edit` |
| create a new file | `Write` |
| shell command | `Bash` |

Copilot reports the full Claude tool vocabulary even though its own UI labels
the shell tool `shell`. So `toolClassMap` matches claude-code exactly, and
matcher alternation works — a `"Bash|shell"` matcher matched `Bash`.

**Carried over, not introduced here:** mapping `write` to `Write` alone means
the `file-write` alias misses `Edit`, i.e. misses modifications to existing
files. That is equally true of the claude-code target today. Copilot is
declared consistently with it rather than diverging, but the gap is real on
both and worth deciding separately.

## 6. `${PLUGIN_ROOT}` resolves

`"${PLUGIN_ROOT}/h.sh"` expanded to the mounted plugin directory. `PLUGIN_ROOT`
is the documented Copilot name; `CLAUDE_PLUGIN_ROOT` was not probed, and the
target lists both because the dispatcher's `selfBundleRoot` fallback does not
depend on either.

## 7. Installing

`copilot plugin install` in 1.0.80 does **not** accept a local directory,
despite the documentation's "Local path: `./my-plugin`". Its source grammar is
`plugin@marketplace`, `owner/repo`, `owner/repo:path` or a git URL. Two routes
work locally:

- `copilot plugin marketplace add <local path>` then
  `copilot plugin install <name>@<marketplace>` — the same two-step shape codex
  and claude-code use, which is why `capabilities.bundleChannel` stays true.
- `copilot --plugin-dir <dir>`, which mounts a plugin for one invocation
  without installing it. Used for every probe here.

## 8. What this does not establish

- **Only the five declared events are measured.** `subagent-*` and `compact-pre`
  were registered but never provoked, so nothing is claimed about them.
- **`bash`/`powershell` split untested.** Copilot's flat entry format has
  separate `bash` and `powershell` keys, which is the closest thing any harness
  has to `handlerWindows`. whippletree emits neither, since the nested shape
  works; whether the split would work is unprobed.
- **Nothing was run on Windows.** See `docs/windows-probe-findings.md` for what
  is known about the dispatcher path there.
- **One version, one platform.** `1.0.80` on macOS/arm64.
