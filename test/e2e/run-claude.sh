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
