#!/usr/bin/env bash
# Headless e2e: install the kb-shaped example bundle into a fresh,
# isolated claude config dir and confirm SessionStart fires the
# vendored whippletree-hook end to end against the real claude CLI, exactly
# once (proving hooks/hooks.json is never emitted alongside the
# per-target hooks file, so there's no double-fire). Runs fully
# unauthenticated; SessionStart is verified (2026-07-29) to fire before
# any auth/model call, so `claude -p ... || true` tolerating the auth
# failure still proves the hook wiring.
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$repo_root"

sandbox=$(mktemp -d)
export CLAUDE_CONFIG_DIR="$sandbox/home"
mkdir -p "$CLAUDE_CONFIG_DIR"
export E2E_MARKER="$sandbox/marker.log"
: >"$E2E_MARKER"

version=$(claude --version 2>/dev/null | grep -oE '[0-9]+\.[0-9]+\.[0-9]+' | head -1)
echo "harness=claude-code version=${version:-unknown} date=$(date -u +%Y-%m-%dT%H:%M:%SZ)"

go build -o examples/kb-shaped/bin/whippletree-hook ./cmd/whippletree-hook

claude plugin marketplace add "$repo_root/examples/kb-shaped"
claude plugin install kb-shaped@kb-shaped-mkt

mkdir -p "$sandbox/proj"
cd "$sandbox/proj"
claude -p "say hi" --permission-mode bypassPermissions </dev/null || true

count=$(grep -c "session-start" "$E2E_MARKER" || true)

if [ "$count" -eq 1 ]; then
  echo "PASS: session-start fired exactly once on claude"
else
  echo "FAIL: session-start fired $count times on claude, want exactly 1"
  echo "--- marker contents ---"
  cat "$E2E_MARKER" || true
  exit 1
fi

# Variant phase: a copy of kb-shaped gains a plain (non-fallback) skill
# requirement. claude-code's skillChannel is plugin-dir, so build must
# not generate an expanded variant under .whippletree/skills/ at all;
# the authored skill ships to the plugin verbatim, and preflight must
# still land it at T1 SATISFY. --allow-refuse because kb-shaped's hard
# stop-gate refuses on opencode during the all-targets build.
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
python3 - "$skillbundle/plugin.json" <<'PY'
import json, sys
path = sys.argv[1]
with open(path) as f:
    manifest = json.load(f)
manifest["extensions"]["dev.whippletree.v1"]["requires"].append(
    {"id": "capture-skill", "kind": "skill", "path": "./skills/capture-skill",
     "minTier": "T1", "hardRequired": False})
with open(path, "w") as f:
    json.dump(manifest, f, indent=2)
    f.write("\n")
PY
go build -o "$skillbundle/bin/whippletree-hook" ./cmd/whippletree-hook
go run ./cmd/whippletree build "$skillbundle" --targets-dir targets --allow-refuse

if [ ! -d "$skillbundle/.whippletree/skills/claude-code" ] &&
   grep -q "name: capture-skill" "$skillbundle/skills/capture-skill/SKILL.md" &&
   ! grep -q "compiled-by" "$skillbundle/skills/capture-skill/SKILL.md"; then
  echo "PASS: plugin-dir target carries the authored skill verbatim, no variant"
else
  echo "FAIL: unexpected skill variant or modified authored skill"; exit 1
fi

pf_out=$(go run ./cmd/whippletree preflight "$skillbundle" --target claude-code \
  --assume-version "$version" --targets-dir targets </dev/null 2>&1) || true
if printf '%s\n' "$pf_out" | grep -q "capture-skill" &&
   printf '%s\n' "$pf_out" | grep "capture-skill" | grep -q "SATISFY"; then
  echo "PASS: skill lands T1 SATISFY on claude-code"
else
  echo "FAIL: skill line wrong on claude-code"; printf '%s\n' "$pf_out"; exit 1
fi
