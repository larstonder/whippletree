# Windows spawn probe findings (empirical)

Probe run: 2026-08-21, GitHub Actions `windows-latest`.

| Item | Value |
|---|---|
| Runner image | `win25-vs2026`, image version `20260818.207.1` |
| Go | `go1.27.0 windows/amd64` |
| Node | `v22.23.2` |
| PowerShell | 7 (`pwsh`) |
| `sh` | Git Bash, as shipped on the runner |
| Default `PATHEXT` | `.COM;.EXE;.BAT;.CMD;.VBS;.VBE;.JS;.JSE;.WSF;.WSH;.MSC;.CPL` |
| Workflow | `.github/workflows/windows-probe.yml` |
| Run | [32530644447](https://github.com/larstonder/whippletree/actions/runs/32530644447) |

Two questions, both of which decide something in the code and neither of which
any harness documents.

1. Every hooks file names the dispatcher without a file extension. On Windows
   the binary is `whippletree-hook.exe`. Does the extensionless path resolve?
2. `internal/dispatch/run.go` execs a handler by path with no interpreter.
   Which handler extensions does that actually launch?

There is no single answer to the first, because there is no single resolver.
`cmd` and PowerShell consult `PATHEXT`; Go's `lookExtensions` and libuv each
append extensions in their own loop; `CreateProcess` alone does none of it. So
the probe measures each spawn path a harness plausibly uses, rather than
reasoning from one of them to the rest.

## 1. Does an extensionless path resolve?

Three phases, because what else is on disk changes the answer. Each row reports
*which file it reached*, not merely whether something launched: the two binaries
print different marker strings.

**Phase 1 — only `whippletree-hook.exe` present. This is what whippletree ships.**

| spawn path | reached |
|---|---|
| `.exe` direct (control) | the `.exe` |
| pwsh, `&` operator | the `.exe` |
| `cmd /c` | the `.exe` |
| `Start-Process` | the `.exe` |
| `sh -c`, forward slashes | the `.exe` |
| Go `exec.Command` | the `.exe` |
| Node `spawnSync`, argv array, no shell | the `.exe` |

Unanimous. The emitted command can stay extensionless.

**Phase 2 — both names present, the extensionless one a valid PE.**

| spawn path | reached |
|---|---|
| `.exe` direct (control) | the `.exe` |
| pwsh, `&` operator | neither: no output, exit 1 |
| `cmd /c` | the `.exe` |
| `Start-Process` | **the extensionless file** |
| `sh -c` | **the extensionless file** |
| Go `exec.Command` | the `.exe` |
| Node `spawnSync` | the `.exe` |

**Phase 3 — both names present, the extensionless one not a PE. This is what a
single bundle carrying both binaries would produce on Windows.**

| spawn path | reached |
|---|---|
| `.exe` direct (control) | the `.exe` |
| pwsh, `&` operator | neither: no output, exit 1 |
| `cmd /c` | the `.exe` |
| `Start-Process` | **fails**: `%1 is not a valid Win32 application` |
| `sh -c` | **the text file, via its shebang** |
| Go `exec.Command` | the `.exe` |
| Node `spawnSync` | the `.exe` |

### What this settles

**Ship only the `.exe`, and keep the emitted command extensionless.** Phase 1 is
unanimous across every spawn path, including the two that matter most here: Node
`spawnSync` with an argv array and no shell, which is how `internal/compile/tsplugin.go`
invokes the dispatcher on the opencode target, and Go `exec.Command`, which is
what the dispatcher itself uses. A compiled bundle stays portable, and no
artifact-format or contract change is needed.

**Do not ship both names.** Not because everything breaks — the more awkward
truth is that the resolvers disagree. `cmd`, Go and Node prefer the `.exe` and
keep working; `Start-Process` and `sh` reach the extensionless file instead. In
phase 3 that means `sh` silently executes the wrong file while `cmd` silently
executes the right one, on the same machine, from the same path. A defect that
depends on which harness spawned you is worse than one that fails everywhere,
which is why `ensureDispatcher` provisions exactly one name.

The pwsh row is genuinely inconclusive in phases 2 and 3: `&` produced no output
and exit 1, so it reached something and printed nothing. It is recorded rather
than explained.

## 2. Which handler extensions can the dispatcher launch?

`runHandler` calls `exec.Command(handlerPath)` — no shell, no interpreter — so
the set of usable `handlerWindows` values is whatever the loader starts on its
own.

| handler | result |
|---|---|
| `h.exe` | ran |
| `h.cmd` | ran |
| `h.bat` | ran |
| `h.ps1` | **failed**: `fork/exec handlers\h.ps1: %1 is not a valid Win32 application` |
| `h.sh` | **failed**: `fork/exec handlers\h.sh: %1 is not a valid Win32 application` |

`.ps1` is absent from the default `PATHEXT` and does not launch from a bare
path. This matters more than it looks: a spawn failure fails open, so a
hard-required `blocking-gate` declaring a `.ps1` handler would have stopped
enforcing while reporting nothing a user would notice. `internal/contract`
now refuses those extensions at build time.

## 3. Incidental: `build` cannot provision its own dispatcher

Reproduced by the probe rather than sought. `ensureDispatcher` looked for
`bin/whippletree-hook` while a Windows build produces `whippletree-hook.exe`,
so the sibling copy missed and `build` stopped. Fixed alongside this document;
recorded because the old error message told the user to run
`go build -o bin/whippletree-hook`, which creates precisely the phase 2/3
arrangement above.

## 4. What this does not establish

- **No harness was installed.** Every row is a spawn path a harness *plausibly*
  uses, chosen by reading how each backend invokes the dispatcher. Whether
  Claude Code, Codex or opencode on Windows actually spawns its hook this way is
  unverified, and an end-to-end probe on a real install is the thing that would
  settle it.
- **`${CLAUDE_PLUGIN_ROOT}` expansion is untested.** The emitted command is
  `"${CLAUDE_PLUGIN_ROOT}/bin/whippletree-hook" run session-start --target claude-code`.
  Every row here used an already-resolved absolute path, so the probe says
  nothing about who expands that variable on Windows. `cmd` treats `${NAME}` as
  literal text and PowerShell reads `${NAME}` as a *PowerShell* variable rather
  than an environment variable, so something above the shell — the harness —
  must be doing the substitution. This is a larger unknown than the one measured
  above and deserves its own probe.
- **Which layer resolved is not distinguished.** Each row measures a resolver
  stacked on Windows — PowerShell command discovery, `cmd`'s `PATHEXT` walk,
  .NET, Go's `lookExtensions`, libuv — not `CreateProcessW` itself. The rows
  answer "does this spawn path work", which is the question the code needs, and
  not "what does Windows do", which they cannot answer.
- **One runner image.** `windows-latest` moves. The image is pinned in the table
  above so a future disagreement is attributable.

## 5. Reproducing

`workflow_dispatch` only — it produces a finding, it does not gate anything. It
must be on the default branch before GitHub will offer it, which is why the
original run used a temporary `push:` trigger that is no longer in the file.
