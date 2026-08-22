# opencode target notes


opencode has no blocking stop event: there is no native primitive `turn-end` can map to
on this target. A hooks-json target blocks a whole turn; opencode can block only a single
tool call, and that block is a Whippletree convention layered on top rather than a native
gate: the compiled shim throws a JavaScript error when the dispatcher exits 2, that error
fails the one `tool.execute` call it was thrown from, and the agent loop continues past
it. There is nothing on this target an author can point a `hardRequired` stop-gate at and
have it stop anything.

The backend is different too. `targets/opencode/target.yaml` sets `backend: ts-plugin`
instead of the hooks-json default, so `whippletree build` writes `hooks/opencode.ts`
rather than a manifest pair: an in-process TypeScript plugin shim, zero npm imports,
that spawns the compiled dispatcher directly via `node:child_process`'s `spawnSync`.

Because opencode cannot satisfy the stop-gate requirement, `examples/kb-shaped` as shipped
refuses preflight and install there, by design: its `stop-gate` requirement is
`hardRequired: true`, and the best tier opencode can ever land it at is Absent, never
Satisfy or Degrade.

```
$ go run ./cmd/whippletree preflight examples/kb-shaped --target opencode --assume-version 1.18.10
whippletree preflight · target opencode (probed 1.18.10)

  stop-gate             want ≥T1  got —  REFUSE    no native mapping for turn-end on this target
  session-start-signal  want ≥T2  got T1  SATISFY   native event:session.created
  file-read-signal      want ≥T4  got T1  SATISFY   native matcher read
  bin-reachable         want ≥T1  got T1  SATISFY   installer-resolved absolute path

Plan: 3 satisfy, 0 degrade, 1 refuse.
```

Soften that one field to `hardRequired: false` and the requirement lands Absent instead
of refusing; `install` then places the shim rather than stopping:

```
$ go run ./cmd/whippletree install my-tool --target opencode --project my-project --assume-version 1.18.10
whippletree preflight · target opencode (probed 1.18.10)

  stop-gate             want ≥T1  got —  ABSENT    no native mapping for turn-end on this target
  session-start-signal  want ≥T2  got T1  SATISFY   native event:session.created
  file-read-signal      want ≥T4  got T1  SATISFY   native matcher read
  bin-reachable         want ≥T1  got T1  SATISFY   installer-resolved absolute path

Plan: 3 satisfy, 0 degrade, 0 refuse, 1 absent.
```

The shim lands at `my-project/.opencode/plugin/whippletree-my-tool.ts` with the
dispatcher path baked in absolute (there's no plugin-root environment variable on this
target to resolve it from at runtime, the way `${CLAUDE_PLUGIN_ROOT}` works for Claude
Code). See "`whippletree install`" under Commands for the overwrite-protection rule.

