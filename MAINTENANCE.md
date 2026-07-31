# Maintenance

## Why this file exists

Whippletree owning a target definition instead of leaning on a harness's own plugin
system is only worth doing if it stays cheap to keep correct. The opencode target is the
test case: the bet is that keeping `targets/opencode/target.yaml`, the ts-plugin
compiler, and the dispatcher's opencode-specific decoding correct against real upstream
opencode releases costs under 2 hours per month, sustained over a full quarter. Every
entry below is a data point toward proving or disproving that, logged as it happens
rather than reconstructed afterward. If a quarter's total lands under roughly 6 hours,
the bet holds. If it doesn't, a project-owned target definition is too expensive for a
harness that moves this fast, and hooks-json-style manual upkeep or dropping the target
should be reconsidered.

Every `test/e2e/run-*.sh` script prints a `harness=<name> version=<probed> date=<iso>`
line before it does anything else. That line is the grep-able evidence trail behind the
"harness", "upstream version", and "date" columns below: when logging an entry, grep the
run's output for that line rather than trusting memory for which version was actually
under test.

## Entry format

| date | harness | upstream version | what broke | minutes spent | commit |
|---|---|---|---|---|---|

- **date**: when the break was found or the entry was logged, `YYYY-MM-DD`.
- **harness**: `opencode`, `claude-code`, or `codex`.
- **upstream version**: the exact version from that run's `harness=... version=...` line.
- **what broke**: what changed upstream and what it broke here, in one line. For a risk
  logged before anything has actually broken, say so plainly rather than inventing a
  fake incident.
- **minutes spent**: time spent diagnosing and fixing, rounded to the nearest 5. This is
  the number the quarterly total is measured against.
- **commit**: the commit that fixed it, or, for a logged risk with no fix yet, the
  commit where the risk was first documented.

## Log

| date | harness | upstream version | what broke | minutes spent | commit |
|---|---|---|---|---|---|
| 2026-07-31 | opencode | 1.18.10 | Bring-up baseline. ts-plugin backend, `whippletree install`, and the opencode e2e script all pass live against this version; the quarter's clock starts here. | (bring-up, not counted) | 925a42b |
| 2026-07-31 | claude-code | 2.1.220 | Bring-up baseline, cross-reference only. Not the falsifying target, but its e2e still passes live against this version as of the same bring-up. | (bring-up, not counted) | 925a42b |
| 2026-07-31 | codex | 0.144.5 | Bring-up baseline, cross-reference only. Not the falsifying target, but its e2e still passes live against this version as of the same bring-up. | (bring-up, not counted) | 925a42b |
| 2026-07-31 | opencode | 1.18.10 | Nothing broken yet, risk logged: opencode ships roughly 20 tagged releases every 30 days upstream, several days with 2 or more releases in one day. At that cadence there are many chances per month for a behavior this target relies on to shift before the next check happens. | 15 | 4aa005c |
| 2026-07-31 | opencode | 1.18.10 | Nothing broken yet, risk logged: the `bash` tool id is explicitly slated for rename with opencode 2.0 (a source comment says so verbatim, and a `2.0` branch already exists upstream). When that lands it will break `targets/opencode/target.yaml`'s `toolClassMap` `shell: bash` mapping outright, not just degrade it. | 15 | 4aa005c |
