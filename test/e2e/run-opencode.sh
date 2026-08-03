#!/usr/bin/env bash
# Headless e2e against the real opencode CLI, in a fresh XDG-isolated
# home. Two things are proven here:
#
#   1. Loud degradation. The kb-shaped bundle as shipped hard-requires a
#      blocking stop gate; opencode has no stop event at all, so
#      preflight must REFUSE rather than install something weaker.
#   2. The class-1 spine. With that one requirement softened, install
#      places the compiled ts-plugin shim and session-start fires the
#      vendored whippletree-hook exactly once on a real run.
#
# Runs fully unauthenticated: opencode 1.18.10 serves its default hosted
# model (opencode/big-pickle) anonymously, so no key and no auth file are
# involved. Because that model is a network service, the assertions are
# on the hook firing, never on the model's prose, and the run itself is
# tolerated with `|| true`.
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$repo_root"

sandbox=$(mktemp -d)
# Every opencode invocation below, version probe included, runs with all
# four XDG dirs inside the sandbox so nothing touches the caller's real
# config, database or credentials. Skills are the one exception opencode
# makes: it still discovers ~/.claude/skills and ~/.agents/skills
# read-only regardless of XDG, which shows up in its logs.
export XDG_CONFIG_HOME="$sandbox/cfg"
export XDG_DATA_HOME="$sandbox/data"
export XDG_CACHE_HOME="$sandbox/cache"
export XDG_STATE_HOME="$sandbox/state"
mkdir -p "$XDG_CONFIG_HOME" "$XDG_DATA_HOME" "$XDG_CACHE_HOME" "$XDG_STATE_HOME"
export E2E_MARKER="$sandbox/marker.log"
: >"$E2E_MARKER"

version=$(opencode --version 2>/dev/null | grep -oE '[0-9]+\.[0-9]+\.[0-9]+' | head -1)
echo "harness=opencode version=${version:-unknown} date=$(date -u +%Y-%m-%dT%H:%M:%SZ)"

go build -o examples/kb-shaped/bin/whippletree-hook ./cmd/whippletree-hook

# --allow-refuse because this build covers every target and opencode is
# expected to refuse the stop gate; the refusal is asserted next.
go run ./cmd/whippletree build examples/kb-shaped --targets-dir targets --allow-refuse

refuse_out=$(go run ./cmd/whippletree preflight examples/kb-shaped \
  --target opencode --assume-version "$version" --targets-dir targets \
  </dev/null 2>&1) && refuse_rc=0 || refuse_rc=$?

if [ "$refuse_rc" -eq 1 ] &&
  printf '%s\n' "$refuse_out" | grep -q "REFUSE" &&
  printf '%s\n' "$refuse_out" | grep -q "stop-gate"; then
  echo "PASS: preflight refuses the hard stop gate on opencode"
else
  echo "FAIL: preflight should exit 1 naming REFUSE and stop-gate, got exit $refuse_rc"
  echo "--- preflight output ---"
  printf '%s\n' "$refuse_out"
  exit 1
fi

# Soft variant of the same bundle: only stop-gate.hardRequired flips, so
# bin-reachable stays hard (opencode reaches T1 on it through
# installerPath) and the spine requirements are untouched.
bundle="$sandbox/bundle"
cp -R examples/kb-shaped "$bundle"
python3 - "$bundle/plugin.json" <<'PY'
import json
import sys

path = sys.argv[1]
with open(path) as f:
    manifest = json.load(f)

requires = manifest["extensions"]["dev.whippletree.v1"]["requires"]
gates = [r for r in requires if r.get("id") == "stop-gate"]
if len(gates) != 1:
    sys.exit("expected exactly one stop-gate requirement, found %d" % len(gates))
gates[0]["hardRequired"] = False

with open(path, "w") as f:
    json.dump(manifest, f, indent=2)
    f.write("\n")
PY

go run ./cmd/whippletree build "$bundle" --targets-dir targets
mkdir -p "$sandbox/proj"
go run ./cmd/whippletree install "$bundle" --target opencode \
  --project "$sandbox/proj" --assume-version "$version" --targets-dir targets </dev/null

shim="$sandbox/proj/.opencode/plugin/whippletree-kb-shaped.ts"
if [ -f "$shim" ]; then
  echo "PASS: install placed the plugin shim at .opencode/plugin/"
else
  echo "FAIL: no plugin shim at $shim"
  ls -la "$sandbox/proj/.opencode/plugin" || true
  exit 1
fi

# The first opencode run against a fresh XDG_CONFIG_HOME silently
# installs a node_modules tree under $XDG_CONFIG_HOME/opencode. That
# takes over five minutes on a cold cache and prints nothing at all
# while it happens, so it looks exactly like a hang. It is not one.
# Later runs in the same config dir finish in 20 to 40 seconds.
run_log="$sandbox/opencode.log"
run_timeout=900
echo "running opencode (cold start can exceed five minutes); log: $run_log"

cd "$sandbox/proj"
opencode run "say hi" </dev/null >"$run_log" 2>&1 &
run_pid=$!
(
  sleep "$run_timeout"
  kill -9 "$run_pid" 2>/dev/null
) &
watchdog_pid=$!
# Disowned so killing it below is silent: an ordinary background job
# would print a "Terminated" notice into the middle of the assertions.
disown "$watchdog_pid" 2>/dev/null || true
wait "$run_pid" || true
kill "$watchdog_pid" 2>/dev/null || true

count=$(grep -c "session-start" "$E2E_MARKER" || true)

if [ "$count" -eq 1 ]; then
  echo "PASS: session-start fired exactly once on opencode"
else
  echo "FAIL: session-start fired $count times on opencode, want exactly 1"
  echo "--- marker contents ---"
  cat "$E2E_MARKER" || true
  echo "--- opencode output ---"
  tail -40 "$run_log" || true
  exit 1
fi

# Phase 3: skill content. A copy of kb-shaped gains a skill and a hard
# T3 stop gate falling back to it; install must place the expanded
# skill and the REFUSE-by-design invariant above must be unaffected
# (kb-shaped itself never gains a skill).
cd "$repo_root"
skillbundle="$sandbox/skillbundle"
cp -R examples/kb-shaped "$skillbundle"
mkdir -p "$skillbundle/skills/capture-skill"
cat > "$skillbundle/skills/capture-skill/SKILL.md" <<'EOF'
---
name: capture-skill
description: Captures knowledge before finishing work.
---
Authored capture guidance.
EOF
authored_before=$(shasum "$skillbundle/skills/capture-skill/SKILL.md")

python3 - "$skillbundle/plugin.json" <<'PY'
import json, sys
path = sys.argv[1]
with open(path) as f:
    manifest = json.load(f)
requires = manifest["extensions"]["dev.whippletree.v1"]["requires"]
requires.append({"id": "capture-skill", "kind": "skill",
                 "path": "./skills/capture-skill", "minTier": "T1",
                 "hardRequired": False})
gates = [r for r in requires if r.get("id") == "stop-gate"]
assert len(gates) == 1
gates[0]["fallbackSkill"] = "capture-skill"
gates[0]["minTier"] = "T3"
with open(path, "w") as f:
    json.dump(manifest, f, indent=2)
    f.write("\n")
PY

# Compile the CLI once for a stable binary path (avoids `go run`'s
# module/build-cache churn across the two invocations below). The
# probe (docs/skill-discovery-probe.md) settled on outcome (a): the
# shipped opencode target.yaml's skillChannel dest (".opencode/skills")
# is project-relative, so install resolves it against --project and no
# HOME override is needed for placement.
go build -o "$sandbox/whippletree" ./cmd/whippletree
go build -o "$skillbundle/bin/whippletree-hook" ./cmd/whippletree-hook
"$sandbox/whippletree" build "$skillbundle" --targets-dir targets

"$sandbox/whippletree" install "$skillbundle" --target opencode \
  --project "$sandbox/proj" --assume-version "$version" --targets-dir targets </dev/null

placed="$sandbox/proj/.opencode/skills/kb-shaped-capture-skill/SKILL.md"
if [ -f "$placed" ]; then
  echo "PASS: install placed the expanded skill"
else
  echo "FAIL: no placed skill at $placed"; exit 1
fi

if grep -q "Use this skill before writing any message that declares the task complete." "$placed"; then
  echo "PASS: description carries the trigger clause"
else
  echo "FAIL: trigger clause missing"; sed -n '1,6p' "$placed"; exit 1
fi

if grep -q "$skillbundle/handlers/capture.sh" "$placed" && ! grep -q "__WHIPPLETREE_BUNDLE_ROOT__" "$placed"; then
  echo "PASS: handler path baked absolutely"
else
  echo "FAIL: placeholder not baked"; grep -n "capture.sh\|BUNDLE_ROOT" "$placed" || true; exit 1
fi

authored_after=$(shasum "$skillbundle/skills/capture-skill/SKILL.md")
if [ "$authored_before" = "$authored_after" ]; then
  echo "PASS: authored skill source untouched"
else
  echo "FAIL: authored skills/ was modified"; exit 1
fi
