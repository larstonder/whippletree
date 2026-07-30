# adapter-sdk

Declare an agent tool's lifecycle requirements once; compile them onto multiple
AI coding harnesses at the best verifiable fidelity tier. Class-1 spine:
Claude Code + Codex CLI. Spec: harness-adapter.architecture.md (presentations repo).

- `adapter-sdk build <bundle-dir>`: compile per-target variants
- `adapter-sdk preflight <bundle-dir> --target <name>`: probe + tier report
- `adapter-hook run <event> --target <name>`: hook dispatcher (invoked by harnesses, not humans)

## Performance

Dispatcher no-op dispatch (no matching handler) measured at ~4-5ms steady-state on Apple
Silicon. A real turn-end dispatch (handler execution included) measures ~15.6ms median /
~18.7ms p95, also on Apple Silicon (target <20ms).

## Commands

### `adapter-sdk build`

Compiles a bundle's `plugin.json` contract (`extensions.dev.adaptersdk.v1`) against every
target definition under `targets/`, writing per-target manifests, per-target hooks files,
and the vendored `.adapter-sdk/` directory the dispatcher reads at runtime.

The dispatcher binary isn't committed to the repo, so on a fresh clone build it first:

```
$ go build -o examples/kb-shaped/bin/adapter-hook ./cmd/adapter-hook
$ go run ./cmd/adapter-sdk build examples/kb-shaped
target claude-code: 4 satisfy, 0 degrade, 0 refuse, 0 absent
target codex: 4 satisfy, 0 degrade, 0 refuse, 0 absent
```

Every hooks-file command build just wrote invokes `<bundle>/bin/adapter-hook`, so build also
makes sure that binary exists: if it's missing, it tries copying an `adapter-hook` found
alongside the running `adapter-sdk` executable (the layout a packaged release ships both
binaries in); if that's not available either, it fails with the exact command to build one
yourself:

```
go build -o <bundle>/bin/adapter-hook ./cmd/adapter-hook
```

Pass `--allow-missing-dispatcher` to downgrade that failure to a warning instead (useful in
a build step that provisions the binary separately). `--targets-dir <dir>` overrides where
target definitions are loaded from (default `targets`, resolved against the working
directory); an empty or wrong directory is now a load error rather than a silent
zero-target build. `--allow-refuse` downgrades a build-time REFUSE (see below) from a
build failure to a warning.

If any target's contract requirement lands at REFUSE (a hard-required requirement the
target cannot satisfy at all, or only below its minimum tier), `build` exits 1 and names
every refusing target/requirement pair on stderr, unless `--allow-refuse` is set.

### `adapter-sdk preflight`

Probes an installed harness (or accepts an assumed version, for scripting) and renders a
terraform-plan-shaped report of what tier each requirement actually lands at.

```
$ go run ./cmd/adapter-sdk preflight examples/kb-shaped --target codex --assume-version 0.146.0
adapter-sdk preflight · target codex (probed 0.146.0)

  stop-gate             want ≥T1  got T1  SATISFY   native Stop + stop_hook_active
  session-start-signal  want ≥T2  got T1  SATISFY   native SessionStart
  file-read-signal      want ≥T4  got T2  SATISFY   matcher Bash|Edit|Write|apply_patch; misses reads in pipelines/heredocs/scripts; may double-count
  bin-reachable         want ≥T1  got T1  SATISFY   bundle channel

Plan: 4 satisfy, 0 degrade, 0 refuse.
```

Exit code is 1 if any requirement REFUSEs; otherwise `.adapter-sdk/install-state.json` is
written recording the harness, probed version, and the achieved tier per requirement.

### `adapter-hook run <event> --target <name>`

Not meant to be run by humans. This is the single command every emitted hooks file
invokes; harnesses call it directly (e.g.
`"${CLAUDE_PLUGIN_ROOT}/bin/adapter-hook" run session-start --target claude-code`). It
resolves the bundle root from the target's plugin-root env var (falling back to its own
grandparent directory), normalizes the harness's native stdin payload, and executes the
matching handler(s) declared in the contract.

## Handler convention

Each requirement's `handler` (or, for `executable-path`, `path`) is a script the dispatcher
invokes for that behavior. The dispatcher normalizes whatever the harness sent on its own
hook stdin into one JSON shape, common across targets, and pipes it to the handler on
stdin. A real normalized `session-start` event, captured from an e2e run against the
installed `claude` CLI:

```json
{"event":"session-start","transcriptPath":".../home/projects/-private-var-folders-.../2cfaee6b-80f9-408c-8844-f59997e89294.jsonl","cwd":".../proj","raw":{"session_id":"2cfaee6b-80f9-408c-8844-f59997e89294","transcript_path":"...","cwd":"...","hook_event_name":"SessionStart","source":"startup"}}
```

`raw` always carries the original harness payload verbatim, so a handler that needs a
harness-specific field the normalizer doesn't surface can still reach it.

Alongside stdin, the dispatcher sets:

- `ADAPTER_EVENT`: the logical event name (`session-start`, `turn-end`, `file-read`, ...)
- `ADAPTER_TARGET`: the target name (`claude-code`, `codex`)
- the parent environment, passed through unchanged (including the harness's own
  plugin-root variable)

Exit codes:

- **0**: proceed. The turn/tool call continues normally.
- **2**: block. Stderr is the reason shown back to the harness/user. This is the one
  refusal dialect both class-1 harnesses share (Claude Code's hook protocol and Codex's
  Stop-hook loop guard both treat exit 2 + stderr as "block, here's why").
- **anything else**: logged to stderr as ignored, then treated as a pass (fail-open,
  matching how the class-1 harnesses themselves behave on unexpected hook exit codes).

If a contract declares more than one handler for the same event, the dispatcher runs them
in order and returns as soon as one exits 2; handlers later in the list never run once an
earlier one has already blocked.

## Spec additions

Three additions this implementation made relative to `harness-adapter.architecture.md`,
to be folded back into that doc:

1. Each requirement gains a `handler` field: a path, relative to the bundle root, to the
   executable the dispatcher runs for that behavior. `executable-path` requirements gain a
   `path` field instead (there's nothing to invoke, only a binary to check for).
2. The handler convention above (stdin/env/exit codes) is new; the architecture doc
   described the contract and target definitions but not the wire format a handler sees.
3. Compiled bundles carry a vendored `.adapter-sdk/` directory: `contract.json` (the
   normalized contract) and `targets/<name>.yaml` (the exact target definition used at
   build time). This is what the dispatcher reads at runtime, so it never has to
   re-resolve the contract or reload target YAMLs the author might have changed since
   build.

## End-to-end tests

`test/e2e/run-codex.sh` and `test/e2e/run-claude.sh` install the `examples/kb-shaped`
bundle into a fresh, isolated harness home (`mktemp -d`, `CODEX_HOME` /
`CLAUDE_CONFIG_DIR` respectively) and drive one real, unauthenticated turn against the
installed `codex` and `claude` CLIs, then assert on a marker file the example's handlers
write to. They are standalone bash, not wired into `go test`; they're the
environment-dependent, real-harness conformance layer.

```bash
test/e2e/run-codex.sh
test/e2e/run-claude.sh
```

Both scripts run fully unauthenticated (no credentials are copied into the isolated
home): `SessionStart` is verified to fire before either harness makes any auth/model
call, so the scripts tolerate the resulting 401/"not logged in" failure and assert only
on the marker file. `run-codex.sh` proves `SessionStart` fires end to end through the
compiled hooks file and the dispatcher; `run-claude.sh` proves the same, plus that it
fires exactly once, confirming `hooks/hooks.json` is never emitted alongside the
per-target hooks file (Claude Code merges that file additively, so its presence would
double-fire every hook).

Actual PASS output from the last verified run, against `codex-cli 0.144.5` and
`claude 2.1.220`:

```
PASS: session-start fired on codex
```

```
PASS: session-start fired exactly once on claude
```

## Scope

This is the class-1 spine only: Claude Code and Codex CLI, at whatever tier (T1/T2) each
requirement can verifiably reach on those two harnesses today. Explicitly out of scope
here, and left to later plans:

- Cursor, opencode, and Zed targets
- T3 (compiled/coarse-trigger) and T4 (observer) tiers
- the conformance kit
- uninstall and upgrade flows

See `blog-posts/harness-adapter.architecture.md` in the presentations repo for the full
architecture this implementation is a slice of.
