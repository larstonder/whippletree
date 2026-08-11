---
name: whippletree-authoring
description: Use when creating or editing a whippletree bundle or tool: writing or changing a contract (plugin.json with dev.whippletree.v1 requirements), handlers, bundled skills, or running whippletree init, build, preflight, or install.
---

# Building whippletree bundles

Whippletree packages an agent tool (skills, hooks, executables) once and
compiles it onto multiple AI coding harnesses (claude-code, codex,
opencode), with an honest per-requirement report of how faithfully each
harness can enforce it. Reach for it when a tool must survive a harness
switch. This guide is the working summary; `references/AUTHORING.md` in
this skill directory is the full authoring reference, read it when a
detail here is not enough. If the `whippletree` binary is not on PATH,
tell the user to install it first rather than improvising files by hand.

## Workflow

1. Scaffold: `whippletree init <dir> --kinds <csv> --yes` (kinds from:
   skill, lifecycle-signal, observation-signal, blocking-gate,
   executable-path; add `--hard <csv>` for hard requirements). This
   writes plugin.json, handler stubs, a skill stub, marketplace.json,
   .gitignore, and a README of the chosen defaults.
2. Edit the contract in plugin.json and fill in the handlers and the
   skill body.
3. Build: `whippletree build <dir>` compiles per-harness artifacts for
   all targets and reports satisfy/degrade/refuse/absent counts.
4. Check one target before installing: `whippletree preflight <dir>
   --target <t>` (add `--assume-version X` to skip live probing).
5. Install into a project: `whippletree install <dir> --target <t>
   --project <projectDir>`. On harnesses with their own plugin
   mechanism (claude-code, codex) this prints the two commands to run
   instead of placing files itself.

## Contract cheat sheet

Requirements live in plugin.json under extensions["dev.whippletree.v1"].
Five kinds:

| kind | needs | what it is |
|---|---|---|
| blocking-gate | handler, event | can block the agent at an event (turn-end today) |
| lifecycle-signal | handler, event | runs at session lifecycle points |
| observation-signal | handler, event | runs when the agent uses tools |
| executable-path | path | a file that must exist in the bundle |
| skill | path | knowledge that ships with the tool |

Events: nine primitives (session-start, session-end, turn-end, tool-pre,
tool-post, subagent-start, subagent-stop, compact-pre, compact-post) and
three aliases (file-read, file-write, shell-exec). Every requirement
declares `minTier` (worst fidelity you accept, T1 best) and
`hardRequired` (true = refuse install rather than degrade silently; no
default, always explicit). `loopGuardRequired` (blocking-gate only)
demands a native double-fire guard. `fallbackSkill` (blocking-gate with
turn-end, or lifecycle-signal with session-start) names a skill
requirement the step compiles into as instructions when a target has no
native event for it.

Skill requirements: `path` must be `./skills/<dir>` and `<dir>` must
equal the SKILL.md frontmatter `name`.

## Reading preflight

Tiers are per requirement, per target: T1 native event, T2 heuristic
approximation, T3 compiled to instructions (via fallbackSkill), Absent.
Verdicts: SATISFY (achieved at or above minTier), DEGRADE (below floor,
soft), REFUSE (below floor or absent, hard; install stops), ABSENT
(absent, soft). On a REFUSE, either lower minTier, set hardRequired
false, add a fallbackSkill (gates and session-start signals only), or
accept that this target refuses by design.

## Handler rules

Handlers get the normalized event JSON on stdin and six env vars:
ADAPTER_EVENT, ADAPTER_TARGET, ADAPTER_PRIMITIVE, ADAPTER_STOP_ACTIVE,
ADAPTER_CWD, ADAPTER_PATH. Exit 0 allows; exit 2 blocks with the reason
on stderr; anything else is logged and ignored (fail-open). Stdout is
forwarded to the harness (on claude-code, SessionStart stdout becomes
context the agent reads); keep diagnostics on stderr or in files. A
handler referenced by fallbackSkill must tolerate `{}` on stdin and be
idempotent. Test handlers standalone:
`echo '{}' | ADAPTER_EVENT=session-start ./handlers/x.sh`

## Common errors

- Build error "skill frontmatter name must equal its directory name":
  rename the directory or the frontmatter to match.
- Build error about a missing bin/whippletree-hook: run the go build
  command the error prints, or pass --allow-missing-dispatcher for
  bundles with no hooks.
- REFUSE on opencode for a hard turn-end gate with no fallbackSkill:
  that is loud degradation working as designed; see "Reading preflight".
