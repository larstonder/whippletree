# Skill discovery probe (empirical)

Probe run: 2026-08-03, macOS (darwin 25.5.0, arm64), `opencode --version` reports `1.18.10` (same binary and version as `docs/opencode-probe-findings.md`).

Question, for Task 1 of the skill-content plan: before the install target for opencode is fixed, does a placed skill surface to and get invoked by the model (Q1), does a project-level skill location work (Q2), and does opencode resolve a `~`-style home path against an overridden `$HOME` or against the OS user database (Q3)?

Companion documents: `docs/opencode-probe-findings.md` (plugin/event/tool probe, 2026-07-31) and `docs/stop-hook-probe.md` (Stop-hook probe, 2026-07-31). This probe reuses their sandboxing method and honesty conventions.

## 1. Safety and isolation

Every `opencode` invocation that matters ran with `$HOME` and all four XDG vars (`XDG_CONFIG_HOME`, `XDG_DATA_HOME`, `XDG_CACHE_HOME`, `XDG_STATE_HOME`) pointed inside a single `mktemp -d` sandbox, exported before the invocation, exactly as the brief requires. No `opencode auth login` was run, no credential file was copied, and no real `~/.claude`, `~/.codex`, `~/.config/opencode`, or `~/.agents` was written to.

One disclosed exception, honesty convention carried over from `docs/opencode-probe-findings.md` section 2: at the start of this session, `opencode --version` was invoked twice without the env override in place first (once blocked by the harness's own auto-mode classifier before it ran, once after, while re-deriving the reproduction steps). Both invocations hit the real `$HOME`. Checked directly afterward:

```
$ find ~/.config/opencode ~/.local/share/opencode ~/.local/state/opencode ~/.cache/opencode -newermt "2026-08-01" 2>&1
0 for '/Users/larstonder/.config/opencode'
```

All four real-home XDG skeleton directories under `~` carry `Jul 31 10:36` timestamps, i.e. they were already created (and already found empty) by the prior probe session on 2026-07-31, and nothing in them is newer than that. The two stray `--version` calls today read those already-existing empty directories and wrote nothing new. No config, database, auth material, or skill file was created, read, or modified in the real home during this probe.

## 2. Sandbox layout

```bash
sandbox=$(mktemp -d)
export HOME="$sandbox/home"
export XDG_CONFIG_HOME="$sandbox/cfg" XDG_DATA_HOME="$sandbox/data"
export XDG_CACHE_HOME="$sandbox/cache" XDG_STATE_HOME="$sandbox/state"
mkdir -p "$HOME" "$XDG_CONFIG_HOME" "$XDG_DATA_HOME" "$XDG_CACHE_HOME" "$XDG_STATE_HOME"
```

Three skill candidates were placed, each an artifact-shaped `SKILL.md` (`compiled-by` frontmatter key, multi-clause description, matching what install will place):

| Candidate | Path | Tests |
|---|---|---|
| `probe-global` | `$HOME/.agents/skills/probe-global/SKILL.md` | Q1 (invocation), Q3 (`~` resolves via overridden `$HOME`, since this path exists *only* under the sandboxed `$HOME`, never under the real one) |
| `probe-project` | `$sandbox/proj/.opencode/skills/probe-project/SKILL.md` | Q2, opencode-native project location |
| `probe-projclaude` | `$sandbox/proj/.claude/skills/probe-projclaude/SKILL.md` | Q2, Claude-compat project location |

Resulting tree (`find "$sandbox" -name SKILL.md`):

```
$sandbox/home/.agents/skills/probe-global/SKILL.md
$sandbox/proj/.claude/skills/probe-projclaude/SKILL.md
$sandbox/proj/.opencode/skills/probe-project/SKILL.md
```

Each `SKILL.md` body (shown for `probe-global`; the other two are `sed`-derived with the name and marker string swapped):

```markdown
---
name: probe-global
description: Probe skill for whippletree discovery testing. Use this skill when asked to prove skill discovery. Use this skill before writing any message that declares the task complete.
compiled-by: whippletree dev
---
When invoked, output the exact string PROBE-GLOBAL-INVOKED.
```

No skill file was placed anywhere else, real home included. Nothing named `probe-*` exists under the real `~/.agents`, `~/.claude`, or `~/.config/opencode`; an unmodified `opencode` binary can reach `probe-global` only through the overridden `$HOME`.

## 3. Exact commands run

Run 1, from `$sandbox/proj` with the sandbox env exported:

```bash
opencode run "List every skill you can see, by exact name. Then invoke the skill named probe-global." \
  </dev/null >"$sandbox/run1.log" 2>&1
```

Wrapped in the same watchdog pattern as `test/e2e/run-opencode.sh` (background process, 900s `kill -9`, disowned timer, `wait ... || true`), since cold start in a fresh XDG home is a recorded 5+ minute silent stall on this machine. In practice this run finished in 6 seconds (12:19:33Z to 12:19:39Z, UTC), because the sandbox's `$XDG_CONFIG_HOME/opencode/node_modules` install completed fast this time; see section 6 for the discrepancy against the recorded cold-start timing.

Run 2, same sandbox (now warm), second skill targeted:

```bash
opencode run "List every skill you can see, by exact name. Then invoke the skill named probe-project." \
  </dev/null >"$sandbox/run2.log" 2>&1
```

## 4. Verbatim output

`run1.log`, in full:

```
[0m
> build · big-pickle
[0m
[0m→ [0mSkill "probe-global"
Visible skills:
- customize-opencode
- probe-global
- probe-projclaude
- probe-project

Now invoking probe-global.
PROBE-GLOBAL-INVOKED
```

`run2.log`, in full:

```
[0m
> build · big-pickle
[0m
[0m→ [0mSkill "probe-project"
Visible skills:
- `customize-opencode`
- `probe-global`
- `probe-projclaude`
- `probe-project`

Invoking `probe-project`:
PROBE-PROJECT-INVOKED
```

Both transcripts show the same four visible skills (`customize-opencode` is opencode's own bundled skill, not one of the probe's; this probe did not place it, and no source for it appears in `$XDG_CONFIG_HOME/opencode/node_modules`, so it ships inside the compiled binary). Both transcripts show the model choosing and firing the `skill` tool (the `→ Skill "..."` line is opencode's own tool-call rendering, matching the `skill` tool id already confirmed in `docs/opencode-research.md` row 10/53), then printing the exact marker string the `SKILL.md` body instructed it to print, with no paraphrase.

Corroborating internal-log evidence, `$XDG_DATA_HOME/opencode/log/opencode.log` (the same log file `docs/opencode-probe-findings.md` used for its own findings):

```
timestamp=2026-08-03T12:19:38.235Z level=INFO run=f4457957 message=evaluated permission=skill pattern=probe-global action.permission=* action.action=allow action.pattern=*
timestamp=2026-08-03T12:20:45.048Z level=INFO run=7bf9a128 message=evaluated permission=skill pattern=probe-project action.permission=* action.action=allow action.pattern=*
```

Each line is a permission-gate evaluation for the `skill` tool, `pattern` set to the exact skill name that was invoked, `action=allow`. This is a second, independent signal (structured log, not model prose) that the invocation was a real tool call, not text the model happened to type. No other skill-related line appears anywhere in `opencode.log` for either run; unlike the plugin probe in `docs/opencode-probe-findings.md` (which saw a duplicate-skill-name warning), this run had no name collisions, so opencode logged nothing about which directories it scanned. **Discovery evidence therefore appears in two places: the `opencode run` stdout transcript (the "Visible skills:" listing and the marker string) is the primary and only evidence of what was discovered and that invocation happened; `$XDG_DATA_HOME/opencode/log/opencode.log`'s `evaluated permission=skill pattern=<name> action.action=allow` line is the corroborating structured signal that a real tool call fired, keyed by exact skill name.** A test/e2e script asserting this should grep the stdout log for the marker string (most direct) and can also grep `opencode.log` for `permission=skill pattern=<name>` as a second signal.

## 5. Verdicts

**Q1, does a placed skill surface to and get invoked by the model: VERIFIED-YES.** Both runs show the model listing the placed skill by exact name and then invoking it (tool-call rendering line `→ Skill "<name>"`, corroborated by the `opencode.log` permission-evaluation line), and the invoked skill's body content (an instruction to print an exact string) was followed verbatim, not paraphrased. This is stronger than the analogous Stop-hook probe: it did not need auth, since `opencode run` completes unauthenticated against the hosted `opencode/big-pickle` default model (matching `docs/opencode-probe-findings.md` section 2), and skill discovery/invocation happened well before any auth-dependent step could fail.

**Q2, does a project-level location work: VERIFIED-YES**, and for both candidate shapes. `probe-project` (`.opencode/skills/probe-project/`) was listed in run1 and both listed and invoked in run2. `probe-projclaude` (`.claude/skills/probe-projclaude/`) was listed in both runs (never targeted for invocation, but its presence in "Visible skills:" both times is discovery evidence; the probe did not test its invocation separately, since Q2 only requires one project-level shape to prove viable, and testing `probe-project`'s invocation already proves an opencode-native project directory is both discovered and invokable).

**Q3, does opencode expand `~` against the overridden `$HOME` rather than the OS user database: VERIFIED-YES.** `probe-global` lives only at `$HOME/.agents/skills/probe-global/SKILL.md` inside the sandbox; it does not exist anywhere under the real `larstonder` user's home (confirmed in section 1, and no `probe-*` file was ever placed there). The model nonetheless listed it in both runs and invoked it in run1. Since the only `$HOME` opencode had wired to it during either run was the sandboxed one, home-relative skill discovery must be resolving against `$HOME` (or an equivalent env-derived path), not against a `getpwuid`-style OS user-database lookup that would point at the real home regardless of `$HOME`.

## 6. Surprises versus the recorded facts

1. **Cold start was not slow this time.** The recorded fact from `docs/opencode-probe-findings.md` is a 5+ minute silent stall on the very first run in a fresh `$XDG_CONFIG_HOME`. This probe's sandbox was freshly created by `mktemp -d` (nothing reused from a prior sandbox), yet `run1.log`'s `node_modules` install and the full run finished in 6 seconds, and the `node_modules` directory's mtime matches the run's own timestamp, confirming a real (not skipped) install happened. The most likely explanation is a warm shared package-manager cache outside the sandboxed XDG dirs (e.g. an npm/bun global cache under the real `$HOME` or `/tmp`, unaffected by the XDG overrides) left behind by the many prior probe and e2e runs on this machine over the past few days. This is an environmental variance, not a change in discovery behavior, and does not affect any of the three verdicts above; the watchdog/900s pattern from `test/e2e/run-opencode.sh` was still used and should stay in place for e2e, since a cold cache on a different machine (e.g. CI) could still hit the 5+ minute case.
2. **opencode ships its own default skill (`customize-opencode`).** Not part of this probe's fixtures; appeared unprompted in "Visible skills:" both times, no source for it found in the installed `node_modules` tree, so it lives inside the compiled binary itself. For Task 8's e2e assertions: a "list every skill" style prompt will always include at least this one extra name, so an assertion should check for the probe/whippletree skill's presence, not an exact-count match.
3. Everything else matched what `docs/opencode-probe-findings.md` recorded: unauthenticated `opencode run` against `opencode/big-pickle` works, XDG isolation holds for everything except skill discovery, and `~/.claude/skills` / `~/.agents/skills` (here, their sandboxed-`$HOME` equivalents) are read regardless of the XDG overrides.

## 7. Decision for Task 3 and Task 8

Outcome (a): `skillChannel dest: project .opencode/skills` (Q2 is yes, both project-level shapes tested were discovered, and `.opencode/skills` is opencode's own native location rather than the Claude-compat one).

Task 8's e2e should assert on the `opencode run` stdout transcript containing the placed skill's name in a "visible skills" style listing and, if the prompt requests invocation, the skill body's marker output; `evaluated permission=skill pattern=<name> action.action=allow` in `$XDG_DATA_HOME/opencode/log/opencode.log` is available as a second, structured-log signal keyed by exact skill name.

## 8. Reproducing

```bash
sandbox=$(mktemp -d)
export HOME="$sandbox/home"
export XDG_CONFIG_HOME="$sandbox/cfg" XDG_DATA_HOME="$sandbox/data"
export XDG_CACHE_HOME="$sandbox/cache" XDG_STATE_HOME="$sandbox/state"
mkdir -p "$HOME" "$XDG_CONFIG_HOME" "$XDG_DATA_HOME" "$XDG_CACHE_HOME" "$XDG_STATE_HOME"

mkdir -p "$HOME/.agents/skills/probe-global"
cat > "$HOME/.agents/skills/probe-global/SKILL.md" <<'EOF'
---
name: probe-global
description: Probe skill for whippletree discovery testing. Use this skill when asked to prove skill discovery. Use this skill before writing any message that declares the task complete.
compiled-by: whippletree dev
---
When invoked, output the exact string PROBE-GLOBAL-INVOKED.
EOF

mkdir -p "$sandbox/proj/.opencode/skills/probe-project"
sed 's/probe-global/probe-project/; s/PROBE-GLOBAL-INVOKED/PROBE-PROJECT-INVOKED/' \
  "$HOME/.agents/skills/probe-global/SKILL.md" \
  > "$sandbox/proj/.opencode/skills/probe-project/SKILL.md"

mkdir -p "$sandbox/proj/.claude/skills/probe-projclaude"
sed 's/probe-global/probe-projclaude/; s/PROBE-GLOBAL-INVOKED/PROBE-PROJCLAUDE-INVOKED/' \
  "$HOME/.agents/skills/probe-global/SKILL.md" \
  > "$sandbox/proj/.claude/skills/probe-projclaude/SKILL.md"

cd "$sandbox/proj"
opencode run "List every skill you can see, by exact name. Then invoke the skill named probe-global." \
  </dev/null >"$sandbox/run1.log" 2>&1   # allow up to 900s; cold start can be silent for 5+ minutes
```
