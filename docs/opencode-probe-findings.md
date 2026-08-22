# opencode probe findings (empirical)

Probe run: 2026-07-31, macOS (darwin 25.5.0, arm64).
Every probe run happened in a `mktemp -d` sandbox with `XDG_CONFIG_HOME` / `XDG_DATA_HOME` / `XDG_CACHE_HOME` / `XDG_STATE_HOME` pointed inside it. No user config, auth or credential file was read, written or modified. One exception to the sandboxing, disclosed in section 2 below: the version check that ran before the sandbox env vars were exported created four empty XDG skeleton directories in the real home.

Companion document: `docs/opencode-research.md` (source-reading research from 2026-07-30). This file records what an actual run does, and where that differs from the research.

## 1. Version and install

| Item | Value |
|---|---|
| Install command | `brew install anomalyco/tap/opencode` (worked first try) |
| Installed version | `opencode 1.18.10` (`/opt/homebrew/bin/opencode`, Cellar `1.18.10`) |
| Extra formulae pulled | `ripgrep 15.2.0`, `pcre2 10.47_1` |
| Session `info.version` reported by the runtime | `1.18.10` |

## 2. Auth answer: unauthenticated works

**The e2e can run with zero provider auth.** No `opencode auth login`, no API key env var, no credential file.

Evidence:

- `opencode run "say hi"` in the isolated sandbox completed (exit 0) and printed `Hi`.
- The internal log line was `message=stream providerID=opencode modelID=big-pickle` followed by `llm.runtime=ai-sdk llm.provider=opencode llm.model=big-pickle`. opencode ships a default hosted model (`opencode/big-pickle`) that serves anonymous requests.
- No `auth.json` was ever created in the sandbox config dir. The only files opencode wrote there were `.gitignore`, `package.json`, `package-lock.json`, `opencode.jsonc` (only `{"$schema": ...}`) plus a `node_modules/` tree.
- No `ANTHROPIC_API_KEY` (or any other provider key) exists in this machine's shell environment, so nothing could have leaked in through env.

Consequence for later tasks: the class-1 style e2e for opencode needs no auth fixture and no secret handling. It can run in CI as long as the machine has network access to opencode's hosted default model.

Caveat to carry forward: the default model is a hosted service, so the e2e is network dependent and the model's behaviour is not pinned. A test that asserts "the read tool fired" should prompt in a way that makes the tool call near certain (see the prompt used in section 5) and should tolerate the model choosing a different phrasing in its final text.

### Timing gotcha

The very first run in a fresh config dir installs a `node_modules/` tree under `$XDG_CONFIG_HOME/opencode/`. That cold start took over five minutes and looks like a hang: the process prints nothing, and opencode has already loaded and logged the plugin by then. Subsequent runs in the same config dir finish in roughly 20 to 40 seconds. An e2e harness should either reuse a warm config dir or use a generous first run timeout.

### Isolation caveats worth knowing

XDG isolation works: during the probe runs in sections 3 to 5, every path opencode wrote (config, database, logs, locks, `node_modules`) landed inside the sandbox. Two things fall outside that guarantee, both verified rather than assumed.

**1. The pre-sandbox version check touched the real home.** Step 1 of this probe was `brew install anomalyco/tap/opencode` followed by `opencode --version`, and that ran *before* any XDG env var was exported. That first invocation created an empty XDG skeleton under the real home:

- `~/.config/opencode/`
- `~/.local/share/opencode/` (containing empty `log/` and `repos/`)
- `~/.local/state/opencode/`
- `~/.cache/opencode/` (containing empty `bin/`)

All four were checked recursively afterwards: zero files in any of them, so no config content, no database, no auth material, nothing sensitive. They are directory skeletons only, and they remain in place. Anyone who wants a zero-footprint probe must export the XDG vars before the very first `opencode` invocation, version check included.

**2. Skill discovery ignores XDG.** The run log showed opencode reading skills from `~/.claude/skills/` and `~/.agents/skills/` (it warned about duplicate skill names across those two dirs). That is read-only discovery of Claude Code compatible skills, not auth, but a test that wants full hermeticity must account for it.

## 3. Plugin loading: what actually loads

Probe plugin (throwaway, not committed) written to `.opencode/plugin/probe.ts` in the project dir, with a **named** export:

```ts
import { appendFileSync } from "node:fs"
export const Probe = async (ctx: any) => ({
  event: async (input: any) => { /* log */ },
  "tool.execute.before": async (input: any, output: any) => { /* log */ },
  "tool.execute.after": async (input: any, output: any) => { /* log */ },
})
```

Top-level `import { appendFileSync } from "node:fs"` works. (`require` was not needed and was not tested; the ESM import is the safe form to emit.)

Loader matrix, tested in a single run with one file per variant, each logging its own identity:

| Path | Export shape | Loaded? |
|---|---|---|
| `.opencode/plugin/probe.ts` | named (`export const Probe`) | yes |
| `.opencode/plugin/v-default-ts.ts` | `export default` | yes |
| `.opencode/plugins/v-plural-ts.ts` | named | yes |
| `.opencode/plugin/v-js.js` | named | yes |
| `.opencode/plugin/v-mjs.mjs` | named | **no** |
| `.opencode/plugin/v-cjs.cjs` | named | **no** |

Directory `plugin` or `plugins` both work, the extension must be `.ts` or `.js`, and opencode accepts both default and named function exports. This matches `docs/opencode-research.md` rows 4, 5 and 15 exactly.

**Recommendation for the emitter:** `.opencode/plugin/<name>.ts` with a single named export. It is the shape the docs use and the one verified working here.

Local plugins do **not** appear in the `plugin.added` bus event. That event only carried opencode's own internal plugin ids (`agent`, `command`, `skill`, `models-dev`, plus one per provider: `anthropic`, `openai`, `google`, and so on, 45 of them in one run). Do not use `plugin.added` to assert that an emitted plugin loaded.

### `PluginInput` (the factory argument), observed

Keys, in enumeration order: `client`, `project`, `worktree`, `directory`, `experimental_workspace`, `serverUrl`, `$`.

- `directory` and `worktree` were both the project dir absolute path.
- `serverUrl` stringified to `http://localhost:4096/`.
- No `env` key, confirming research row 3.

## 4. `event` hook payload shape

The `event` hook receives a **single argument that wraps the bus event under an `event` key**:

```jsonc
{ "event": { "id": "evt_...", "type": "session.created", "properties": { /* per type */ } } }
```

That wrapper matters: a handler must read `input.event.type`, not `input.type`.

Verbatim capture: `internal/dispatch/testdata/opencode-probe/event-session-created.json`.

`session.created` `properties`:

- `sessionID` (string, `ses_...`)
- `info` object with: `id`, `slug` (a random human-readable pair such as `proud-wolf`), `version` (`"1.18.10"`), `projectID` (`"global"` here), `directory`, `path` (empty string), `title` (`"New session - <ISO timestamp>"`), `permission` (array of `{permission, pattern, action}`, defaulted here to `deny` for `question`, `plan_enter`, `plan_exit`), `cost` (0), `tokens` (`{input, output, reasoning, cache:{read, write}}`), `time` (`{created, updated}` epoch millis).

Event types observed on a plain `opencode run "say hi"` (counts from one run):

`plugin.added` 45, `message.part.delta` 12, `message.part.updated` 7, `message.updated` 5, `session.updated` 4, `session.status` 4, `session.diff` 2, `catalog.updated` 2, `session.created` 1, `reference.updated` 1, `integration.updated` 1, `session.idle` 1.

Two more shapes captured while probing (not committed as fixtures, recorded here because a Stop-hook analogue will need them):

```jsonc
{ "event": { "id": "evt_...", "type": "session.status",
             "properties": { "sessionID": "ses_...", "status": { "type": "busy" } } } }

{ "event": { "id": "evt_...", "type": "session.idle",
             "properties": { "sessionID": "ses_..." } } }
```

`session.idle` still fires on 1.18.10 despite being marked deprecated in the schema, and it carries only `sessionID`. `session.status` carries a nested `status.type`. Both are observation only: neither can veto or extend the agent loop, confirming research row 13.

Surprise versus the research: three event types fired that the docs list in `plugins.mdx` does not mention, namely `catalog.updated`, `reference.updated` and `integration.updated`. Treat the documented event list as incomplete rather than exhaustive.

## 5. `tool.execute.before` / `.after` payload shapes

Triggered with a `hello.txt` in the project dir and the prompt:

```
opencode run --auto "read the file hello.txt and say its contents"
```

`--auto` auto-approves permissions and let the tool call go through without an interactive prompt. The model chose the `read` tool, tool id string `"read"`, confirming research row 10.

Both hooks take **two** arguments (`input`, `output`). The committed fixtures wrap them as `{"input": ..., "output": ...}` so one file captures both.

### `tool.execute.before` (`internal/dispatch/testdata/opencode-probe/tool-before-read.json`)

```jsonc
{
  "input":  { "tool": "read", "sessionID": "ses_...", "callID": "call_00_..." },
  "output": { "args": { "filePath": "/abs/path/hello.txt" } }
}
```

Note the asymmetry: at `before` time the args live on **`output.args`**, not on `input`. `output` is the mutable slot, so this is the interception point for rewriting arguments.

### `tool.execute.after` (`internal/dispatch/testdata/opencode-probe/tool-after-read.json`)

```jsonc
{
  "input": {
    "tool": "read", "sessionID": "ses_...", "callID": "call_00_...",
    "args": { "filePath": "/abs/path/hello.txt" }
  },
  "output": {
    "title": "hello.txt",
    "output": "<path>...</path>\n<type>file</type>\n<content>\n1: hello from the probe fixture\n\n(End of file - total 1 lines)\n</content>",
    "metadata": {
      "preview": "hello from the probe fixture",
      "truncated": false,
      "loaded": [],
      "display": { "type": "file", "path": "/abs/path/hello.txt",
                   "text": "hello from the probe fixture",
                   "lineStart": 1, "lineEnd": 1, "totalLines": 1, "truncated": false }
    }
  }
}
```

At `after` time `args` has moved onto `input`, and `output` is the tool result (`title`, `output` string, `metadata`). Field names to key on across both hooks: `input.tool`, `input.sessionID`, `input.callID`.

Mapping notes for the dispatch layer:

- The stable pair across both hooks is `(sessionID, callID)`. Use it to correlate a `before` with its `after`.
- `read`'s argument key is `filePath` (camelCase, absolute path, already resolved against the project dir). It is not `file_path` as in Claude Code.
- The `read` result's `output` string is opencode's own XML-ish envelope with line numbers, not raw file content. Anything that wants raw text should use `metadata.display.text` or `metadata.preview`.

## 6. Surprises versus `docs/opencode-research.md`

1. **Row 20 resolved, and better than expected.** The research flagged "do plugins load and does `session.created` fire with zero auth" as unverified. Both do. Beyond that, the model call itself also succeeds unauthenticated via the built-in `opencode/big-pickle` default, which the research did not anticipate. That removes the auth question from the plan.
2. **`before` args live on `output`, not `input`.** Easy to get wrong from the type names alone.
3. **Local plugins are invisible in `plugin.added`.** The bus event only reports opencode's internal plugins.
4. **The documented event list is incomplete** (`catalog.updated`, `reference.updated`, `integration.updated` all fired and are not in `plugins.mdx`).
5. **Cold start is slow enough to look like a hang** (over five minutes on first run in a fresh config dir, because of the `node_modules` install).
6. **XDG isolation is real but not total**: opencode still discovers skills in `~/.claude/skills` and `~/.agents/skills` regardless of XDG, and any `opencode` invocation made before the XDG vars are exported (even `opencode --version`) creates an empty XDG skeleton in the real home. See section 2.
7. Everything else the research asserted and this probe touched (plugin dirs, extensions, export shapes, `PluginInput` keys, tool id `read`, `session.idle` present but toothless) held exactly.

## 7. Reproducing

```bash
S=$(mktemp -d)
export XDG_CONFIG_HOME=$S/cfg XDG_DATA_HOME=$S/data XDG_CACHE_HOME=$S/cache XDG_STATE_HOME=$S/state
export PROBE_LOG=$S/probe.log
mkdir -p "$S/proj/.opencode/plugin"
# write the probe plugin from section 3 to $S/proj/.opencode/plugin/probe.ts
printf 'hello from the probe fixture\n' > "$S/proj/hello.txt"
cd "$S/proj"
opencode run "say hi"                                      # first run: allow 6+ minutes
opencode run --auto "read the file hello.txt and say its contents"
```

macOS note: there is no `timeout` binary by default, so wrap long runs with a background watchdog rather than `timeout 120 ...`.
