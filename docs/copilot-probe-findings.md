# GitHub Copilot CLI probe findings (empirical)

Probe run: 2026-08-21, macOS (darwin 25.5.0, arm64).

| Item | Value |
|---|---|
| Install | `/opt/homebrew/bin/copilot` |
| Version reported | `GitHub Copilot CLI 1.0.80.` (note the trailing period) |
| Sandboxing | `mktemp -d` per probe, plugins mounted with `--plugin-dir` |

Every probe in sections 1-6 mounted its plugin with the global `--plugin-dir`
flag rather than installing one, so Copilot wrote nothing to a marketplace or
to the caller's plugin state. `test/e2e/run-copilot.sh` exercised section 7's
marketplace route separately, against a sandboxed `COPILOT_HOME`. Every run
here was authenticated: unlike the codex probe, these consumed real model
calls, because only an agent that runs a turn can answer the questions about
`Stop` and `PreToolUse`.

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

That is the shape `internal/compile/hooksfile.go` writes. Entries carrying a
`matcher` were not hand-tested here, but they are covered: the file the real
emitter produces for `examples/kb-shaped` binds `file-read` to a
`"matcher": "Read"` entry on `PostToolUse`, and `test/e2e/run-copilot.sh`
observes it fire. So `backend: hooks-json` carries Copilot with no emitter
change, and the `spec.discovery.hooksFormat` field sketched for this target is
not needed.

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

The probe registered every documented event name at once, in PascalCase, and
ran a one-tool session.

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
all of which are measured above. `PostCompact` appears in the table only because
it was registered as a control: it is not a documented Copilot event, and it did
not fire.

An unrecognised event key is **ignored**, not fatal: a file declaring
`NotARealEvent` alongside `SessionStart` still fired `SessionStart`. Hence
`strictness.unknownFieldsFatal: false`.

## 4. The finding that shapes the target: `Stop` ignores exit codes

Copilot's exit-code semantics are not Claude's, and the first version of this
document got the conclusion half right for the wrong reason. `Stop` **does**
block. It does not listen on the channel whippletree speaks.

Two channels were tested, each twice, with `PreToolUse` as a positive control
proving the method can see a block at all. Every run used
`copilot --plugin-dir <p> -p "Say the single word ready." </dev/null`, so no
further input was available and a block could not be mistaken for a prompt for
one. The handler blocks on its first firings only, so a real block cannot spin.

| channel | handler does | result |
|---|---|---|
| exit code, on `PreToolUse` | `exit 2` | **blocks.** Copilot prints `Denied by preToolUse hook: hook exited with code 2` and the model reports it could not run the command |
| exit code, on `Stop` | `exit 2` + stderr | **does not block.** Fires once, session ends |
| stdout JSON, on `Stop` | `{"decision":"block","reason":...}`, exit 0 | **blocks.** Fires repeatedly, and the agent acts on the reason |

The `Stop` exit-2 handler logged exactly one line and the session ended:

```
Stop fired, attempt=0
```

The same handler, changed only to print a decision on stdout and exit 0, was
asked to block until the agent also said "banana":

```
Stop fired, attempt=0
Stop fired, attempt=1
Stop fired, attempt=2
```

with the session output showing the agent complying rather than finishing:

```
ready
banana
banana
```

A separate trial that blocked three times produced four firings and sent the
agent off searching the codebase for the string in its reason. The loop is
real and the reason reaches the model, so `stop_hook_active` in the payload
guards a real one.

### What that means for the target

`turn-end` is declared `blocking: false`, and that is **a statement about
whippletree, not about Copilot**. `internal/dispatch/run.go` signals a block by
exiting 2 and nothing else, so with today's dispatcher a `blocking-gate` at
`turn-end` cannot be enforced here, and a hard-required one correctly REFUSEs.

But the ceiling is higher than the current declaration. A dispatcher that
emitted `{"decision":"block"}` on stdout for this target would make turn-end
gates land at **T1**, natively enforced. That needs a per-target block dialect
in the dispatcher, a target-surface addition tracked separately and not made
here. The honest reading of the REFUSE this target produces today is
"whippletree cannot drive Copilot's gate yet", not "Copilot has no gate".

`loopGuardField` stays unset for the same reason: whippletree never triggers
the loop, so it never needs to break out of one.

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
matcher alternation works: a `"Bash|shell"` matcher matched `Bash`.

**Carried over, not introduced here:** mapping `write` to `Write` alone means
the `file-write` alias misses `Edit`, i.e. misses modifications to existing
files. That is equally true of the claude-code target today. The Copilot
target matches it rather than diverging, but the gap is real on both and worth
deciding separately.

## 6. `${PLUGIN_ROOT}` resolves

`"${PLUGIN_ROOT}/h.sh"` expanded to the mounted plugin directory, which is the
name the emitter actually uses: `internal/compile/emit.go` substitutes
`PluginRootVars[0]` and never reads the rest of the list. The target lists
`CLAUDE_PLUGIN_ROOT` second by analogy with codex, but it is inert: nothing
emits it, and it was not probed.

## 7. Skills, and why the manifest key replaces rather than adds

This section is the evidence `internal/compile/emit.go` cites when it points a
plugin-dir target's `skills` key at a per-target variant directory.

**Naming a skills directory suppresses the conventional one.** A plugin was
built with a skill named `rootonly` in the conventional `skills/` directory and
a second named `expandedonly` in a directory named by the manifest's
`"skills": ["expanded"]` key. Asked to list its skills, the agent returned:

```
expandedonly
find-skills
tdd
customize-cloud-agent
github-pr-media
```

`rootonly` is absent. So the key replaces discovery rather than extending it,
which is why `writeManifest` sets it only when a variant exists: setting it
unconditionally would hide the skills of every bundle that has no expansion.

**All skills land in the variant, not just expanded ones.** The obvious way
that design fails is a bundle with two skills where only one carries a
fallback: if the variant held only the expanded skill, the other would vanish
the moment the key was set. Built with `alpha` (target of a `fallbackSkill`)
and `beta` (ordinary), both appeared:

```
alpha
beta
find-skills
...
```

Only `alpha`'s `SKILL.md` carries the `compiled-tier: T3` provenance comment;
`beta` is copied through unchanged apart from the compiled-by marker.

**The expansion is what the agent sees.** For a bundle scaffolded with
`init --kinds skill,blocking-gate`, the T3 variant Copilot lists carries the
canonical fidelity sentence verbatim:

```
<!-- compiled-tier: T3
     source-requirement: blocking-gate (blocking-gate, turn-end)
     fidelity: best-effort, no harness-level enforcement on this target: the
     model is instructed to run the step and usually will, but can skip it
     under pressure
```

Note the path form: the manifest says `"skills": ["./.whippletree/skills/copilot"]`,
resolved relative to the plugin root rather than to `.plugin/`, same as the
`hooks` key beside it.

## 8. Installing

`copilot plugin install` in 1.0.80 does **not** accept a local directory,
despite the documentation's "Local path: `./my-plugin`". Its source grammar is
`plugin@marketplace`, `owner/repo`, `owner/repo:path` or a git URL. Two routes
work locally:

- `copilot plugin marketplace add <local path>` then
  `copilot plugin install <name>@<marketplace>`. This is the same two-step
  shape codex and claude-code use, which is why `capabilities.bundleChannel`
  stays true.
- `copilot --plugin-dir <dir>`, which mounts a plugin for one invocation
  without installing it. Used for every probe here.

## 9. What this does not establish

- **Only the five declared events are measured.** `subagent-*` and `compact-pre`
  were registered but never provoked, so nothing is claimed about them.
  `SessionEnd` and `UserPromptSubmit` both fired; the first is mapped, the
  second has no whippletree primitive and is dropped.
- **`bash`/`powershell` split untested.** Copilot's flat entry format has
  separate `bash` and `powershell` keys, the closest thing any harness has to
  `handlerWindows`. whippletree emits neither, since the nested shape works.
- **Whether Copilot has a pre-auth event is unknown.** Every run here was
  authenticated. `test/e2e/run-copilot.sh` assumes there is no event that fires
  before login, which is why it is excluded from the nightly matrix; that
  assumption is untested.
- **Unknown *fields* were not probed, only unknown *event keys*.** §3 shows a
  bogus event key is ignored. A stray key inside a hook entry may behave
  differently, so `strictness.unknownFieldsFatal: false` is narrower evidence
  than its name suggests.
- **`mergeSemantics` was never probed at all.** The target declares `replace`
  by analogy with codex. Nothing reads the field, so nothing depends on it.
- **`CLAUDE_PLUGIN_ROOT` was not probed.** Only `PLUGIN_ROOT` was. The target
  lists both, but `internal/compile/emit.go` only ever emits the first element,
  so the second is inert.
- **Nothing was run on Windows**, and this is one version on one platform:
  `1.0.80`, macOS/arm64.

### Which declared fields are load-bearing

Several fields in `targets/copilot/target.yaml` are documented harness research
that no code reads: `hooksKey`, `mergeSemantics`, `strictness.unknownFieldsFatal`,
`capabilities.stopLoopGuard` and `capabilities.matcherAlternation`. They are
recorded because a future backend may need them, not because they take effect
today. A reader should not treat `mergeSemantics: replace` as measured.

## 10. Reproducing

No sandbox harness is needed beyond a `mktemp -d` and the `--plugin-dir` flag,
which mounts a plugin for one invocation without touching installed state:

```
copilot --plugin-dir <dir> -p "<prompt>" </dev/null
```

Each probe above is a `.plugin/plugin.json` naming a `hooks.json`, plus a shell
handler that appends to a marker file outside the plugin. The `Stop` cases are
the ones worth re-running first: they decide a REFUSE, and each was run twice.

Note that runs consume real model credits, unlike the codex and claude-code
probes, because `Stop` and `PreToolUse` only fire once an agent takes a turn.
