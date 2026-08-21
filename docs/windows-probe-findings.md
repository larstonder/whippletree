# Windows dispatcher-path probe (empirical)

Probe run: 2026-08-21, GitHub Actions `windows-latest`. Reproducible with
`.github/workflows/windows-probe.yml` (manual dispatch).

Question, from issue #1: every hooks file `compile.Build` writes names the
dispatcher without an extension.

```
"${PLUGIN_ROOT}/bin/whippletree-hook" run <event> --target <name>
```

On Windows the binary must be `whippletree-hook.exe` to be launchable. Does the
extensionless path resolve anyway? The answer decides something structural. If
it resolves, a compiled bundle stays portable across platforms. If it does not,
the hook command has to name a host-specific binary and portability is lost.

## 1. Setup

`whippletree-hook.exe` built into a directory containing **no** extensionless
file. That condition matters: PATHEXT is only consulted when the exact path
misses, so a bundle shipping both binaries would resolve to the Unix one and
fail. The probe asserts the extensionless file is absent before measuring.

The binary exits 1 with a usage message when run with no arguments, which is
what distinguishes "launched" from "not found".

## 2. Results

| spawn path | extensionless absolute path |
|---|---|
| `.exe` named explicitly (control) | launched |
| PowerShell, `& $path` | **launched** |
| `cmd /c "<path>"` | **launched** |
| `Start-Process -FilePath` | **launched** |
| Go `exec.Command(path)` | **launched** (exit status 1) |

Every path resolved `whippletree-hook` to `whippletree-hook.exe`.

## 3. What this does not establish

The Go row is weaker than it looks. Go's `os/exec` performs its own
`lookExtensions` pass on Windows, so that row shows Go resolving the name, not
`CreateProcess` resolving it. A spawner that calls `CreateProcessW` directly
with no extension handling of its own, which is what Node's `child_process`
does without `shell: true`, was not measured.

That gap is narrow in practice. A hooks-file entry is a shell command string,
not an argv array, so a harness firing one is running it through a shell. But it
is unmeasured, and a per-harness probe on Windows would close it.

## 4. Consequence

Keep the emitted command extensionless. Ship `whippletree-hook.exe` on Windows,
and only that: shipping both names is not a safe fallback, it is the one
arrangement guaranteed to break.

A compiled bundle stays portable across platforms, which means the artifact
format needs no change and `spec.discovery` does not need a per-target override.

Still open in issue #1: `whippletree build` cannot yet provision the dispatcher
on Windows, because `ensureDispatcher` and `copyFromSiblingExecutable` look for
a sibling named exactly `whippletree-hook`. This finding says what they should
do, which is write `whippletree-hook.exe` into `bin/` while leaving the emitted
command alone.
