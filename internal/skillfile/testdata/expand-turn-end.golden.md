---
name: cap
description: Captures notes. Use this skill before writing any message that declares the task complete.
compiled-by: whippletree 1.2.3
---
Authored body.

<!-- compiled-tier: T3
     source-requirement: capture-gate (blocking-gate, turn-end)
     fidelity: best-effort, no harness-level enforcement on this target: the model is instructed to run the step and usually will, but can skip it under pressure
     compiled-by: whippletree 1.2.3 (https://whippletree.dev), do not hand-edit (edit the bundle contract instead) -->
## Manual step on this harness (turn-end)

This harness has no enforced turn-end hook. Before writing any message that
declares the task complete, run:

    echo '{}' | ADAPTER_EVENT=turn-end ADAPTER_PRIMITIVE=turn-end \
      ADAPTER_TARGET=opencode ADAPTER_CWD="$PWD" ADAPTER_STOP_ACTIVE=false ADAPTER_PATH= \
      __WHIPPLETREE_BUNDLE_ROOT__/handlers/capture.sh

If it exits 2, read its stderr and do what it says. Then run the same command
once more with ADAPTER_STOP_ACTIVE=true and continue; a second exit 2 means the
step still failed and you should tell the user rather than silently finish.
