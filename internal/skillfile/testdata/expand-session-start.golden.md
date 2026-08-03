---
name: cap
description: Captures notes. Use this skill at the start of a session, before other work.
compiled-by: whippletree 1.2.3
---
Authored body.

<!-- compiled-tier: T3
     source-requirement: pull-signal (lifecycle-signal, session-start)
     fidelity: best-effort, no harness-level enforcement on this target: the model is instructed to run the step and usually will, but can skip it under pressure
     compiled-by: whippletree 1.2.3, do not hand-edit (edit the bundle contract instead) -->
## Manual step on this harness (session-start)

This harness has no session-start hook seam. At the start of a session, before
other work, run:

    echo '{}' | ADAPTER_EVENT=session-start ADAPTER_PRIMITIVE=session-start \
      ADAPTER_TARGET=cursor-x ADAPTER_CWD="$PWD" ADAPTER_STOP_ACTIVE= ADAPTER_PATH= \
      __WHIPPLETREE_BUNDLE_ROOT__/handlers/pull.sh

If it fails, tell the user what went wrong rather than silently continuing.
