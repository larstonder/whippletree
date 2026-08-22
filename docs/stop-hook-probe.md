# Stop hook probe (empirical)

Probe run: 2026-07-31, macOS (darwin 25.5.0, arm64), `claude --version` reports `2.1.220 (Claude Code)`.

Question: in `test/e2e/run-claude.sh`'s isolated, unauthenticated flow, does the Stop hook (logical event `turn-end`, handled by `examples/kb-shaped/handlers/capture.sh`) fire at all, and does it write `turn-end-blocked` / `turn-end-allowed` marker lines?

## What was run

1. `test/e2e/run-claude.sh` unmodified, exactly as committed. It built `whippletree-hook`, added and installed the `kb-shaped` plugin into a fresh `mktemp -d` sandbox (`CLAUDE_CONFIG_DIR` pointed inside it), then ran `claude -p "say hi" --permission-mode bypassPermissions` in an unauthenticated state. Output:

   ```
   harness=claude-code version=2.1.220 date=2026-07-31T13:33:01Z
   Adding marketplace…✔ Successfully added marketplace: kb-shaped-mkt (declared in user settings)
   Installing plugin "kb-shaped@kb-shaped-mkt"...✔ Successfully installed plugin: kb-shaped@kb-shaped-mkt (scope: user)
   Not logged in · Please run /login
   PASS: session-start fired exactly once on claude
   ```

2. The script leaves its sandbox behind (no cleanup trap), so its `$E2E_MARKER` file was located under `$TMPDIR` and inspected directly, then `claude -p "say hi" --permission-mode bypassPermissions` was rerun by hand in that same sandbox (same `CLAUDE_CONFIG_DIR` and `E2E_MARKER`, same installed plugin) to get a second, isolated data point without rebuilding or reinstalling anything.

3. For the second manual run, `--debug hooks --debug-file <path>` was added (an additional flag on the `claude` invocation, not a change to any repo file) to see whether the harness invokes the Stop hook internally even if `capture.sh` never gets to write to the marker.

No file inside the Whippletree repo was modified. `git status` in the repo is clean before and after this probe (the only build artifact, `examples/kb-shaped/bin/whippletree-hook`, is gitignored).

## Verbatim marker content

After both runs (the scripted one and the manual rerun), `$E2E_MARKER` contains only `session-start` lines, one per run, and zero `turn-end` lines:

```
session-start {"event":"session-start","transcriptPath":"/var/folders/.../tmp.lJkoR5G0e9/home/projects/.../a52e282e-6b44-4ba2-9659-ccf2b556f5a7.jsonl","cwd":"/private/var/folders/.../tmp.lJkoR5G0e9/proj","raw":{"session_id":"a52e282e-6b44-4ba2-9659-ccf2b556f5a7","transcript_path":"/var/folders/.../a52e282e-6b44-4ba2-9659-ccf2b556f5a7.jsonl","cwd":"/private/var/folders/.../tmp.lJkoR5G0e9/proj","hook_event_name":"SessionStart","source":"startup"}}
session-start {"event":"session-start","transcriptPath":"/var/folders/.../tmp.lJkoR5G0e9/home/projects/.../a83af432-7762-4093-98d7-7a1ca49df67f.jsonl","cwd":"/private/var/folders/.../tmp.lJkoR5G0e9/proj","raw":{"session_id":"a83af432-7762-4093-98d7-7a1ca49df67f","transcript_path":"/var/folders/.../a83af432-7762-4093-98d7-7a1ca49df67f.jsonl","cwd":"/private/var/folders/.../tmp.lJkoR5G0e9/proj","hook_event_name":"SessionStart","source":"startup"}}
```

(paths truncated with `...` for readability; the full paths are just the sandbox's own `mktemp -d` directory, nothing sensitive)

`grep -c turn-end` against this file returns `0`. Neither `turn-end-blocked` nor `turn-end-allowed` appears. `capture.sh` was never invoked, since it appends one of those two lines on every invocation.

## Why: the debug log

The `--debug hooks --debug-file` run shows the hook system working up to a point:

```
Read manifest hooks for plugin kb-shaped (enabled=true): ./hooks/claude-code.json
Registered 3 hooks from 1 plugins
...
dispatching to firstParty model=claude-opus-5[1m]
[ERROR] API error (attempt 1/11): Could not resolve authentication method. ...
...
Released PID lock for 2.1.220
```

Claude Code registers all three hooks (SessionStart, PostToolUse, Stop) from the plugin manifest, and SessionStart fires (it wrote the marker line above). Right after the single API call fails on missing authentication, the process tears down (LSP shutdown, PID lock release) with no further hook activity logged and no second attempt at a turn. There is no completed assistant turn in this run: the CLI never gets past the first API call, so there is nothing for the agent loop to "stop". The absence of `turn-end` here follows from auth failing before any turn completes, not from a wiring gap: the hook is registered exactly like the one that did fire.

## Outcome

**(b): fires only with auth.** The wiring (manifest, hook registration, handler script) is provably correct, matching the SessionStart hook that does fire in the same run. Observing `turn-end` needs a completed model turn, which this sandbox cannot produce without real authentication. Settling this definitively (as opposed to by strong inference) would require one authenticated run, which is out of scope here per the no-auth constraint: never authenticate in this probe.

## Decision for Task 2

Because the Stop hook did not fire unauthenticated, Task 2 does not add a hard e2e assertion for `turn-end` / `capture.sh` marker lines; the existing unit tests remain the evidence for that behavior, and `run-claude.sh` keeps asserting only `session-start`, exactly as it does today.
