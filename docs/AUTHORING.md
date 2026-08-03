# Authoring a whippletree bundle

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
├── .whippletree/
│   ├── contract.json                 ← generated: normalized contract, vendored
│   └── targets/*.yaml                ← generated: target defs used at build time, vendored
├── hooks/
│   ├── claude-code.json              ← generated: hooks-json manifest fragment
│   ├── codex.json                    ← generated: hooks-json manifest fragment
│   └── opencode.ts                   ← generated: compiled ts-plugin shim
└── bin/
    └── whippletree-hook              ← generated: copied dispatcher binary
```

`init`'s own `.gitignore` matches this split exactly:

```
/hooks/
/.claude-plugin/plugin.json
/.codex-plugin/
/.whippletree/
/bin/whippletree-hook
```

It ignores every generated path and nothing else: `.claude-plugin/marketplace.json`
(authored, hand-maintained) and `bin/<your-tool>` (your own executable) are left
untracked-but-not-ignored, same as any other source file.

`examples/kb-shaped/` in this repo breaks that rule on purpose: it commits
`hooks/`, `.whippletree/`, `.claude-plugin/plugin.json`, and `.codex-plugin/`
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

One of four, closed set:

| kind | fires on | needs |
|---|---|---|
| `blocking-gate` | a point where the harness can be told to refuse (only `turn-end` today) | `handler`, `event` |
| `lifecycle-signal` | a session/subagent/compact boundary | `handler`, `event` |
| `observation-signal` | a tool-class alias, e.g. a file read | `handler`, `event` |
| `executable-path` | nothing: it's a static check that a binary is reachable | `path`, no `event` |

The first three run a handler against a real dispatch event; `executable-path`
never invokes anything, it's a build/preflight-time presence check.

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
`executable-path`, which has no handler to run, only a file to check for.

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
| `ADAPTER_TARGET` | the target name (`claude-code`, `codex`, `opencode`) | never |
| `ADAPTER_PRIMITIVE` | the resolved primitive (`tool-post` when `ADAPTER_EVENT` is the `file-read` alias) | never |
| `ADAPTER_STOP_ACTIVE` | `"true"`/`"false"` | every event except `turn-end` on claude-code/codex (the only primitive either target declares a loop-guard field for); always empty on opencode, which has no `turn-end` mapping at all |
| `ADAPTER_CWD` | the harness-reported working directory | the harness gave no cwd |
| `ADAPTER_PATH` | the first normalized path | no path applies to this event |

`ADAPTER_PATH` is a convenience for the common one-file case; the stdin JSON's
`paths` array keeps every path the event touched, in order, including
duplicates the matcher heuristic can produce. Real captured output, a
`file-read` alias dispatched on codex, `handlers/dump.sh` echoing its own
environment to stderr and its stdin verbatim. The `[...]` brackets around
`ADAPTER_STOP_ACTIVE`'s value below are `dump.sh`'s own delimiters (so an empty
value is visible as `[]` rather than disappearing), not something whippletree
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

Note on the dispatcher's own plumbing: it only forwards a handler's **stderr**
back to the harness (for the block-reason exit-code-2 dialect below); a
handler's stdout is not read at all. Write your own logging/diagnostics to
stderr or a file you control, never expect stdout to go anywhere.

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
- **Codex rejects unknown fields in its own manifest.** Codex's plugin loader
  is strict about the hooks-json shape it accepts
  (`targets/codex/target.yaml`'s `strictness.unknownFieldsFatal: true`); this
  is Codex's own parsing behavior, not something whippletree enforces. It
  means `whippletree build`'s codex output must stick to exactly the fields
  Codex recognizes, and it's also why hand-editing a compiled
  `.codex-plugin/plugin.json` to add a stray field will break the install on
  Codex specifically, even though the same manifest shape is tolerated fine
  on Claude Code (`unknownFieldsFatal: false` there).
