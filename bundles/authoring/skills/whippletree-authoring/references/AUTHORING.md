# Authoring a Whippletree bundle

This is the complete reference for writing a bundle: what's yours to write versus
what `whippletree build` generates, every field the contract accepts, the wire
format your handlers see, and how each target's own quirks show up in practice.
For the CLI's commands (`build`/`preflight`/`install`/`init`) and their flags, see
the [README](../README.md).

## Bundle anatomy

`whippletree init` writes only what you're expected to edit. From a fresh
`whippletree init acme-tool --kinds blocking-gate,lifecycle-signal,observation-signal,executable-path --yes`:

```
acme-tool/
├── plugin.json                       ← the contract (authored)
├── .claude-plugin/marketplace.json   ← local-install pointer (authored)
├── .gitignore                        ← ignores what build generates (authored)
├── README.md                         ← generated-file list + defaults table (authored)
├── handlers/
│   ├── blocking-gate.sh              ← your behavior (authored)
│   ├── lifecycle-signal.sh           ← your behavior (authored)
│   └── observation-signal.sh         ← your behavior (authored)
└── bin/
    └── acme-tool                     ← your tool's own executable, optional (authored)
```

Run `whippletree build .` on top of that and four more things appear (all four kinds
scaffold soft by default, so this build succeeds everywhere; a hard-required
`blocking-gate` can make `build`/`preflight`/`install` refuse on a per-target basis, see
"Per-target notes" below):

```
acme-tool/
├── .claude-plugin/plugin.json        ← generated: per-target manifest (hooks key)
├── .codex-plugin/plugin.json         ← generated: per-target manifest (hooks key)
├── .plugin/plugin.json               ← generated: per-target manifest (hooks key)
├── .whippletree/
│   ├── contract.json                 ← generated: normalized contract, vendored
│   ├── install-state.json            ← written by preflight/install: harness, version, tiers reached
│   ├── skills/<target>/              ← generated: per-target skill variants, when a T3 fallback applies
│   └── targets/*.yaml                ← generated: target defs used at build time, vendored
├── hooks/
│   ├── claude-code.json              ← generated: hooks-json manifest fragment
│   ├── codex.json                    ← generated: hooks-json manifest fragment
│   ├── copilot.json                  ← generated: hooks-json manifest fragment
│   └── opencode.ts                   ← generated: compiled ts-plugin shim
└── bin/
    └── whippletree-hook              ← generated: copied dispatcher binary
```

`init`'s own `.gitignore` matches this split exactly:

```
/hooks/
/.claude-plugin/plugin.json
/.codex-plugin/
/.plugin/
/.whippletree/
/bin/whippletree-hook
```

It ignores every generated path and nothing else: `.claude-plugin/marketplace.json`
(authored, hand-maintained) and `bin/<your-tool>` (your own executable) are left
untracked-but-not-ignored, same as any other source file.

`examples/kb-shaped/` in this repo breaks that rule on purpose: it commits
`hooks/`, `.whippletree/`, `.claude-plugin/plugin.json`, `.codex-plugin/` and `.plugin/`
instead of gitignoring them, because that bundle exists to be read as a worked
example of what `build` produces, not just run. Only `bin/whippletree-hook`
stays gitignored there too (via the repo's root `.gitignore`), since that one
file is a compiled binary tied to this repo's own `cmd/whippletree-hook`, with
no documentation value in freezing it. Don't copy this pattern into your own
bundle unless you have the same reason: for a real tool, gitignore everything
`build` generates.

## Contract field reference

A bundle's contract lives at `plugin.json`'s `extensions["dev.whippletree.v1"]`.
Each entry in `requires` is one requirement:

```json
{"id":"stop-gate","kind":"blocking-gate","event":"turn-end","minTier":"T1",
 "hardRequired":true,"loopGuardRequired":true,"handler":"./handlers/capture.sh"}
```

### `kind`

One of five, closed set:

| kind | fires on | needs |
|---|---|---|
| `blocking-gate` | a point where the harness can be told to refuse (only `turn-end` today) | `handler`, `event` |
| `lifecycle-signal` | a session/subagent/compact boundary | `handler`, `event` |
| `observation-signal` | a tool-class alias, e.g. a file read | `handler`, `event` |
| `executable-path` | nothing: it's a static check that a binary is reachable | `path`, no `event` |
| `skill` | nothing: it's a directory of instructions placed for the model to read | `path`, no `event`, no `handler` |

The first three run a handler against a real dispatch event; `executable-path`
never invokes anything, it's a build/preflight-time presence check. `skill` is
the odd one out among the static kinds: it doesn't check for a binary, it
places content, its own `SKILL.md` plus whatever else lives in its directory.
See "Skills and instruction fallback" below for the full picture.

### `event`

Nine primitives: `session-start`, `session-end`, `turn-end`, `tool-pre`,
`tool-post`, `subagent-start`, `subagent-stop`, `compact-pre`, `compact-post`.
Three aliases: `file-read`, `file-write`, `shell-exec`, each expanding to
`tool-post` plus a tool class (`read`, `write`, `shell` respectively). Write
whichever one reads clearly in your contract; the dispatcher resolves the
alias for you (see `ADAPTER_PRIMITIVE` below). Omit `event` entirely for
`executable-path`.

### `minTier`

The worst tier you'll accept: `T1` (native guarantee) down to `T4` (after-the-fact
observation, no default). Compare against what `preflight`/`build` reports a
target actually achieving for that requirement.

### `hardRequired`

No default. `contract.Validate` fails the build if it's omitted from any
requirement, so it's always explicit, never implied. `true` means: refuse
install rather than silently run at a tier below `minTier`. `false` means:
degrade quietly and still install.

Worked example, the exact case that matters most in practice: a hard
`observation-signal` on `file-read` at `minTier: T1`.

```json
{"id":"file-read-hard","kind":"observation-signal","event":"file-read","minTier":"T1",
 "hardRequired":true,"handler":"./handlers/file-read-hard.sh"}
```

Claude Code and opencode both have a native file-read tool (`Read` and `read`
respectively), so this requirement lands at T1 there. Codex has no such tool
(`targets/codex/target.yaml`'s `toolClassMap.read` is `null`); a `file-read` on
codex only ever degrades to T2 via a command-matcher heuristic
(`Bash|Edit|Write|apply_patch`). Because `hardRequired` is `true` and T2 is
below the declared `T1` floor, `preflight` refuses on codex specifically:

```
$ whippletree preflight . --target codex --assume-version 0.146.0
whippletree preflight · target codex (probed 0.146.0)

  file-read-hard  want ≥T1  got T2  REFUSE    matcher Bash|Edit|Write|apply_patch; misses reads in pipelines/heredocs/scripts; may double-count

Plan: 0 satisfy, 0 degrade, 1 refuse.
```

Soften `hardRequired` to `false` on the same requirement and this becomes a
silent DEGRADE-to-T2 on codex instead, install succeeding everywhere.

### `loopGuardRequired`

`blocking-gate`-only. Demands the target supply a native double-fire guard
(Claude Code and Codex both expose `stop_hook_active` on their Stop event);
without it a `turn-end` gate can't reach T1, since nothing stops the harness
from calling the hook again after it already blocked once.

### `handler` / `path`

`handler`: path to your script or binary, relative to the bundle root, for
`blocking-gate`/`lifecycle-signal`/`observation-signal`. `path`: same, for
`executable-path` and `skill`, neither of which has a handler to run: one
checks for a file, the other places a directory.

A requirement may also carry `handlerWindows`, which the dispatcher prefers on
Windows. See [Windows handlers](#windows-handlers) for the extensions that are
legal there and why the set is small.

### `fallbackSkill`

Only legal on `blocking-gate` at event `turn-end` or `lifecycle-signal` at
event `session-start`, the two events a `skill`'s own trigger clause can
describe in one imperative sentence a model reliably acts on. Names the `id`
of a `skill` requirement in the same contract; that skill's compiled variant
carries the step as instructions on any target where the gate or signal would
otherwise land Absent. See "Skills and instruction fallback" below for the
full mechanism.

## Env vars and stdin

The dispatcher normalizes whatever the harness sent on its own hook stdin into
one JSON shape, common across targets, and pipes it to the handler on stdin. A
real normalized `session-start` event, captured from an e2e run against the
installed `claude` CLI:

```json
{"event":"session-start","transcriptPath":".../home/projects/-private-var-folders-.../2cfaee6b-80f9-408c-8844-f59997e89294.jsonl","cwd":".../proj","raw":{"session_id":"2cfaee6b-80f9-408c-8844-f59997e89294","transcript_path":"...","cwd":"...","hook_event_name":"SessionStart","source":"startup"}}
```

Alongside stdin, the dispatcher sets six env vars, always present (empty
string where the concept doesn't apply to this invocation):

| var | value | empty when |
|---|---|---|
| `ADAPTER_EVENT` | the logical event name exactly as your contract wrote it (may be an alias, e.g. `file-read`) | never |
| `ADAPTER_TARGET` | the target name (`claude-code`, `codex`, `copilot`, `opencode`) | never |
| `ADAPTER_PRIMITIVE` | the resolved primitive (`tool-post` when `ADAPTER_EVENT` is the `file-read` alias) | never |
| `ADAPTER_STOP_ACTIVE` | `"true"`/`"false"` | every event except `turn-end` on claude-code/codex (the only primitive either target declares a loop-guard field for); empty on opencode's hook path, and on copilot, which delivers `stop_hook_active` in its own payload but has no `loopGuardField` declared because whippletree cannot drive its gate; a T3 fallback skill instructs the model to set it explicitly when running the handler manually |
| `ADAPTER_CWD` | the harness-reported working directory | the harness gave no cwd |
| `ADAPTER_PATH` | the first normalized path | no path applies to this event |

`ADAPTER_PATH` is a convenience for the common one-file case; the stdin JSON's
`paths` array keeps every path the event touched, in order, including
duplicates the matcher heuristic can produce. Real captured output, a
`file-read` alias dispatched on codex, `handlers/dump.sh` echoing its own
environment to stderr and its stdin verbatim. The `[...]` brackets around
`ADAPTER_STOP_ACTIVE`'s value below are `dump.sh`'s own delimiters (so an empty
value is visible as `[]` rather than disappearing), not something Whippletree
itself emits:

```
$ echo '{"session_id":"s1","transcript_path":"/tmp/r.jsonl","cwd":"/tmp/proj","hook_event_name":"PostToolUse","tool_name":"Bash","tool_input":{"command":"rg --files -g '"'"'hello.txt'"'"' && sed -n '"'"'1,120p'"'"' hello.txt"},"tool_response":"...","tool_use_id":"exec-1"}' \
    | whippletree-hook run file-read --target codex
ADAPTER_EVENT=file-read ADAPTER_PRIMITIVE=tool-post ADAPTER_TARGET=codex ADAPTER_STOP_ACTIVE=[] ADAPTER_CWD=/tmp/proj ADAPTER_PATH=hello.txt
{"event":"tool-post","alias":"file-read","toolClass":"read","command":"rg --files -g 'hello.txt' \u0026\u0026 sed -n '1,120p' hello.txt","paths":["hello.txt","hello.txt"],"transcriptPath":"/tmp/r.jsonl","cwd":"/tmp/proj","raw":{"session_id":"s1","transcript_path":"/tmp/r.jsonl","cwd":"/tmp/proj","hook_event_name":"PostToolUse","tool_name":"Bash","tool_input":{"command":"rg --files -g 'hello.txt' \u0026\u0026 sed -n '1,120p' hello.txt"},"tool_response":"...","tool_use_id":"exec-1"}}
```

`paths` has `hello.txt` twice: the matcher regex found the filename in both
halves of the shell command, the exact double-counting the codex degradation
note warns about (see the worked example above).

A `turn-end` on codex, run twice to show `ADAPTER_STOP_ACTIVE` flip:

```
$ echo '{"session_id":"s1","transcript_path":"/tmp/r.jsonl","cwd":"/tmp/proj","hook_event_name":"Stop","stop_hook_active":false,"last_assistant_message":"done"}' \
    | whippletree-hook run turn-end --target codex
ADAPTER_EVENT=turn-end ADAPTER_PRIMITIVE=turn-end ADAPTER_TARGET=codex ADAPTER_STOP_ACTIVE=[false] ADAPTER_CWD=/tmp/proj ADAPTER_PATH=
{"event":"turn-end","stopHookActive":false,"transcriptPath":"/tmp/r.jsonl","cwd":"/tmp/proj","raw":{"session_id":"s1","transcript_path":"/tmp/r.jsonl","cwd":"/tmp/proj","hook_event_name":"Stop","stop_hook_active":false,"last_assistant_message":"done"}}

$ echo '{"session_id":"s1","transcript_path":"/tmp/r.jsonl","cwd":"/tmp/proj","hook_event_name":"Stop","stop_hook_active":true}' \
    | whippletree-hook run turn-end --target codex
ADAPTER_EVENT=turn-end ADAPTER_PRIMITIVE=turn-end ADAPTER_TARGET=codex ADAPTER_STOP_ACTIVE=[true] ADAPTER_CWD=/tmp/proj ADAPTER_PATH=
{"event":"turn-end","stopHookActive":true,"transcriptPath":"/tmp/r.jsonl","cwd":"/tmp/proj","raw":{"session_id":"s1","transcript_path":"/tmp/r.jsonl","cwd":"/tmp/proj","hook_event_name":"Stop","stop_hook_active":true}}
```

`raw` always carries the harness's original payload verbatim, so a handler
needing a harness-specific field the normalizer doesn't surface can still
reach it.

Note on the dispatcher's own plumbing: a handler's **stderr** is forwarded
to the harness and carries the block reason for the exit-code-2 dialect. A
handler's **stdout** is forwarded verbatim to the dispatcher's own stdout,
in handler order, regardless of exit code; what it means is up to the
harness. On claude-code, a SessionStart handler's stdout becomes
additional context the agent reads, which is how a tool can prompt the
agent at session start. On codex the bytes are forwarded to its hook
runner, with no specific effect probed or promised. On opencode the
generated shim captures the dispatcher's stdout in its spawnSync result
and never uses it, so stdout is discarded on this target. Keep
diagnostics on stderr or in files you control: stdout is the payload
channel, not the logging channel.

## Handler best practices

- **Fail-open vs. fail-closed is the exit code, nothing else.** Exit 0 means
  proceed; exit 2 means block, with the reason on stderr, the one dialect both
  Claude Code and Codex share. Any other exit is logged and treated as
  fail-open: a crashing or misconfigured handler never silently blocks a turn
  it wasn't meant to. If a contract declares more than one handler for the
  same event, the dispatcher runs them in order and stops as soon as one
  exits 2: first block wins, and later handlers in the list never run once an
  earlier one already blocked.
- **Low-millisecond budget, no network, on tool events.** `tool-pre`/`tool-post`
  handlers run in the hot path of every matching tool call; a slow or
  network-bound handler there is felt on every read/write/shell call the
  contract watches, not just once per turn. Keep those handlers local and fast;
  push anything slower to a `lifecycle-signal` (session/turn boundaries, far
  less frequent) or do it out-of-band from a background process the handler
  only kicks off.
- **Idempotency.** A handler can run more than once for what looks like one
  logical event (a `blocking-gate` firing again after `ADAPTER_STOP_ACTIVE`
  goes `true`, or a harness retrying a hook call). Guard side effects
  accordingly, e.g. the way `examples/kb-shaped/handlers/capture.sh` checks
  `ADAPTER_STOP_ACTIVE` before deciding to block again.
- **Test the handler standalone, no CLI involved.** Since the whole contract is
  stdin JSON in, env vars alongside it, exit code out, you can drive a handler
  directly:

  ```
  $ echo '{}' | ADAPTER_EVENT=session-start ADAPTER_PRIMITIVE=session-start \
      ADAPTER_TARGET=claude-code ADAPTER_CWD="$PWD" ADAPTER_STOP_ACTIVE= ADAPTER_PATH= \
      ./handlers/lifecycle-signal.sh
  $ echo $?
  0
  ```

  No bundle build, no installed harness, no `whippletree-hook` required: this
  is the fastest loop while writing a handler's actual logic.

## Skills and instruction fallback

A `skill` requirement never runs against a dispatch event: it's a directory of
instructions, `path: "./skills/<dir>"`, placed for the model to read rather
than a handler the dispatcher invokes. Beyond the closed field set
`contract.Validate` checks, one more rule is enforced by the shared
filesystem check that build, preflight, and install all run
(`internal/skillfile.Check`): the `SKILL.md` frontmatter's `name` must equal
`<dir>` exactly, the identity the plugin-dir discovery convention keys on.
`whippletree init --kinds skill` scaffolds `skills/<name>/SKILL.md` with the
bundle's own name already in place, so this is never worked out by hand:

```
$ whippletree init acme-tool --kinds skill,blocking-gate --hard blocking-gate --yes
$ cat acme-tool/skills/acme-tool/SKILL.md
---
name: acme-tool
description: Replace this with one sentence saying when the agent should use this skill.
---
Replace this body with the knowledge or workflow the skill teaches.
```

That same scaffold shows the other half of the feature: pairing a `skill`
with a `blocking-gate` wires `fallbackSkill` and bumps the gate's `minTier` to
`T3` automatically, and the generated `plugin.json` and `README.md` agree on
it (both come from the one `scaffoldRequirements` helper, so they can't
drift):

```json
{
  "id": "blocking-gate", "kind": "blocking-gate", "event": "turn-end",
  "minTier": "T3", "hardRequired": true, "loopGuardRequired": true,
  "handler": "./handlers/blocking-gate.sh", "fallbackSkill": "skill"
}
```

```
| kind | id | event | minTier | hardRequired |
|---|---|---|---|---|
| skill | skill | (none) | T1 | false |
| blocking-gate | blocking-gate | turn-end | T3 | true |
```

### `fallbackSkill`'s two legal pairs

`fallbackSkill` is only legal in two (kind, event) combinations, enforced by
`contract.Validate`: `blocking-gate` at `turn-end`, or `lifecycle-signal` at
`session-start`. Both are events a skill's one-sentence trigger clause can
describe unambiguously; the other seven primitives have no such natural,
single-sentence framing a model reliably acts on from a standing skill
listing alone, so `fallbackSkill` is refused there.

### Absent-only: the fallback never overrides a working native gate

The expansion only ever fires where the requirement would otherwise land
Absent, never on a target that already has a native (if lesser) mechanism.
`internal/tier.Assign` checks this directly: a T3 fallback only replaces an
Absent assignment, so a `blocking-gate` that degrades to some non-Absent tier
on a target keeps that degradation untouched, it never gets silently
upgraded (or downgraded) to the instruction path just because a skill happens
to be wired.

### The trigger clause

Whichever event a skill falls back for, its description gains exactly one
clause, appended once per fallback-eligible requirement it covers:

| event | clause appended to the skill's `description` |
|---|---|
| `turn-end` | Use this skill before writing any message that declares the task complete. |
| `session-start` | Use this skill at the start of a session, before other work. |

This is the only thing that changes about the skill's standing, always-loaded
listing; the model sees a slightly longer one-line description, not a whole
new mechanism, until the trigger condition is actually reached.

### The two-run protocol

Beneath the trigger clause, the compiled variant gains a body section: a
literal command to run and an explicit protocol for the double-fire case a
real `turn-end` hook would otherwise absorb via `ADAPTER_STOP_ACTIVE`. Real
captured output, `.whippletree/skills/opencode/acme-tool/SKILL.md` after
`whippletree build .` on the scaffold above:

```
---
name: acme-tool
description: Replace this with one sentence saying when the agent should use this skill. Use this skill before writing any message that declares the task complete.
compiled-by: whippletree v0.0.0-20260803130917-1305256b94e4+dirty
---
Replace this body with the knowledge or workflow the skill teaches.

<!-- compiled-tier: T3
     source-requirement: blocking-gate (blocking-gate, turn-end)
     fidelity: best-effort, no harness-level enforcement on this target: the model is instructed to run the step and usually will, but can skip it under pressure
     compiled-by: whippletree v0.0.0-20260803130917-1305256b94e4+dirty, do not hand-edit (edit the bundle contract instead) -->
## Manual step on this harness (turn-end)

This harness has no enforced turn-end hook. Before writing any message that
declares the task complete, run:

    echo '{}' | ADAPTER_EVENT=turn-end ADAPTER_PRIMITIVE=turn-end \
      ADAPTER_TARGET=opencode ADAPTER_CWD="$PWD" ADAPTER_STOP_ACTIVE=false ADAPTER_PATH= \
      __WHIPPLETREE_BUNDLE_ROOT__/handlers/blocking-gate.sh

If it exits 2, read its stderr and do what it says. Then run the same command
once more with ADAPTER_STOP_ACTIVE=true and continue; a second exit 2 means the
step still failed and you should tell the user rather than silently finish.
```

`__WHIPPLETREE_BUNDLE_ROOT__` is a placeholder in the built variant; `whippletree
install` resolves it (replace-all) to the bundle's absolute path when it
places the skill, exactly the way `placeTSPlugin` resolves its own HOOK
placeholder for the ts-plugin backend. The two-run shape mirrors what a real
`turn-end` hook does automatically: run once with `ADAPTER_STOP_ACTIVE=false`,
and if that blocks (exit 2), run again with `ADAPTER_STOP_ACTIVE=true` so the
handler's own loop guard can let it through the second time, the same
contract every `blocking-gate` handler already has to honor (see "Handler
best practices" above).

### The stdin-`{}` tolerance requirement

The compiled instructions pipe a bare `echo '{}'` on stdin, not the full
normalized event JSON the dispatcher would build from a real harness payload
(no `transcriptPath`, no `paths`, no `raw`). A model running the command by
hand has no way to reconstruct that shape, and shouldn't have to: a handler
reachable via `fallbackSkill` must treat every field beyond the env vars as
optional and tolerate an empty object on stdin without erroring. This is
already true of the scaffolded `blocking-gate.sh` stub (it never reads stdin
at all), and it's the bar any hand-written handler wired to a `fallbackSkill`
needs to clear too: don't assume the JSON the dispatcher normally supplies is
there.

### Placement fidelity versus behavioral fidelity

`preflight` is explicit that a `skill` requirement and a T3 fallback promise
two different, weaker things than a native hook does. A plain `skill`
requirement (`assignSkill` in `internal/tier`) reports *placement* fidelity
only: T1 means the file landed at the right path, nothing about whether the
model reads or acts on it. A T3 fallback goes further and reports on
*behavior*, but with the weakest honesty disclosure `preflight` ever prints,
`contract.T3Fidelity` verbatim, rendered directly beneath the fallback's own
line so it can't be missed. Real captured output, the same scaffold, probed
against opencode:

```
$ whippletree preflight . --target opencode --assume-version 1.18.10
whippletree preflight · target opencode (probed 1.18.10)

  skill          want ≥T1  got T1  SATISFY   placed via copy-dir skill channel
  blocking-gate  want ≥T3  got T3  SATISFY   compiled to instructions
                 best-effort, no harness-level enforcement on this target: the model is instructed to run the step and usually will, but can skip it under pressure

Plan: 2 satisfy, 0 degrade, 0 refuse.
```

The `skill` line's own T1 is real, the file did land, but says nothing about
whether the model ever opens `SKILL.md`. The `blocking-gate` line's T3 is the
honest ceiling for an instruction-carried gate: it's SATISFY against its own
`minTier: T3`, never mistaken for the T1 a native `Stop` hook earns on
claude-code or codex, and the disclosure line underneath says exactly why in
the same words the generated `SKILL.md`'s own provenance comment uses.

### Where a compiled variant is written

A target that needs an expansion gets its own copy of every skill under
`.whippletree/skills/<target>/`, so two targets can carry different
instructions for the same skill. How that copy reaches the harness depends on
the skill channel:

- **copy-dir** targets (opencode): `install` copies the variant into the
  harness's own skills directory. The variant always exists, because the
  compiled-by marker is how `install` recognises a directory it owns.
- **plugin-dir** targets (claude-code, codex, copilot): skills normally travel
  inside the bundle and the harness discovers `skills/` on its own. A variant
  is written only when a requirement actually falls back, and then the
  generated manifest gets a `"skills": ["./.whippletree/skills/<target>"]` key
  pointing at it.

That key **replaces** the harness's discovery of `skills/` rather than adding
to it, which is why it is set only when there is a variant, and why the variant
contains every skill in the contract rather than just the expanded one. Both
behaviours are measured in `docs/copilot-probe-findings.md`.

In practice this only triggers where a gate cannot be enforced natively, so
claude-code and codex bundles never carry the key: both map `turn-end` and
`session-start` and block on them, so nothing falls back there.

## Per-target notes

- **opencode has no true blocking gate.** There is no native `turn-end`
  primitive on this target at all: opencode can only ever refuse a single
  `tool.execute` call (the compiled shim throws a JS error on exit 2, which
  fails that one call and lets the agent loop continue), never a whole turn.
  A `hardRequired: true` `blocking-gate` therefore always lands Absent, never
  Satisfy or Degrade, on opencode; either soften it to `hardRequired: false`
  for this target or accept that `install`/`preflight` will refuse there by
  design. See the README's "opencode" section for the full architecture
  behind this.
- **Copilot's turn-end gate cannot be driven yet.** Copilot maps `turn-end` to
  its `Stop` hook and does honour a block there, but only via a JSON decision
  written to stdout; it ignores the handler's exit code, which is the only
  block signal the dispatcher emits. So a `hardRequired: true` `blocking-gate`
  at `turn-end` REFUSEs on copilot the same way it does on opencode, for a
  different reason: opencode has no gate, copilot has one Whippletree cannot
  yet reach. Pair it with a `fallbackSkill` to land T3 instead, or soften it.
  Measured in `docs/copilot-probe-findings.md`; tracked as issue 19.
- **Codex rejects unknown fields in its own manifest.** Codex's plugin loader
  is strict about the hooks-json shape it accepts
  (`targets/codex/target.yaml`'s `strictness.unknownFieldsFatal: true`); this
  is Codex's own parsing behavior, not something Whippletree enforces. It
  means `whippletree build`'s codex output must stick to exactly the fields
  Codex recognizes, and it's also why hand-editing a compiled
  `.codex-plugin/plugin.json` to add a stray field will break the install on
  Codex specifically, even though the same manifest shape is tolerated fine
  on Claude Code (`unknownFieldsFatal: false` there).

## Windows handlers

`handler` is a path to something the dispatcher executes. On Windows a
`#!/usr/bin/env bash` script is not executable at all, because Windows has no
shebang support, so a requirement may declare a second handler for it:

```json
{
  "id": "capture-gate",
  "kind": "blocking-gate",
  "event": "turn-end",
  "minTier": "T1",
  "hardRequired": true,
  "handler": "./handlers/capture.sh",
  "handlerWindows": "./handlers/capture.cmd"
}
```

Both are validated at build time and both must exist. Which one runs is decided
at dispatch, not at build, because a bundle is compiled once and may be
installed on a different platform than it was built on.

### What Windows can actually launch

The dispatcher runs a handler by path, with no interpreter. That limits
`handlerWindows` to what the loader starts on its own: **`.exe`, `.com`, `.bat`
and `.cmd`**. Anything else is rejected at build time.

`.ps1` is the one that catches people out. PowerShell scripts are not in the
default `PATHEXT` and do not launch from a bare path — measured, not assumed;
see `docs/windows-probe-findings.md`. Nor does `.sh`. Both fail in the loader
with `%1 is not a valid Win32 application`, and since a spawn failure fails
open, a hard-required gate would quietly stop enforcing. Hence the build-time
refusal: it is the last point where the author still sees the problem.

To use PowerShell, wrap it:

```bat
@echo off
powershell -NoProfile -ExecutionPolicy Bypass -File "%~dp0capture.ps1"
```

Handlers take no arguments — the payload arrives on stdin — so the argument
quoting that makes `.bat` and `.cmd` dangerous to invoke from a program does
not arise here.

### Omitting it

A requirement with no `handlerWindows` is **not carried on Windows**: the
dispatcher says so and moves on, rather than trying to exec a shell script and
failing with a loader error that looks like a broken install. Omitting it is
therefore a decision, not an oversight, and contracts that never mention it
behave exactly as they did before.

Note that `preflight` does not model this. It reports the tier a requirement
reaches on a target, and a target is a harness, not a platform — so a
requirement with no `handlerWindows` still reports SATISFY, and is then skipped
at dispatch on Windows. Read a preflight verdict as "what this harness can
carry", not "what will run on this machine".

One gap remains: when a requirement falls back to T3, the instructions compiled
into `SKILL.md` are a POSIX shell snippet naming `handler`, never
`handlerWindows`. A T3 fallback therefore reads wrongly on Windows.

`handlerWindows` is additive in `contractVersion` 1.1.0, and a contract that
uses it must declare 1.1.0 or later. That is enforced, not advisory: an older
whippletree does not know the field, would drop it silently, and would then
treat the missing Windows handler as the author's choice.
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
