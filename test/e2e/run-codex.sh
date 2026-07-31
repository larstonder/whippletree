#!/usr/bin/env bash
# Headless e2e: install the kb-shaped example bundle into a fresh,
# isolated codex home and confirm SessionStart fires the vendored
# whippletree-hook end to end against the real codex CLI. Runs fully
# unauthenticated: no auth.json is copied in, so the script can't
# collide with the caller's real login. SessionStart is verified
# (2026-07-29) to fire before any auth/model call, so `codex exec ...
# || true` tolerating the auth failure still proves the hook wiring.
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$repo_root"

sandbox=$(mktemp -d)
export CODEX_HOME="$sandbox/home"
mkdir -p "$CODEX_HOME"
export E2E_MARKER="$sandbox/marker.log"
: >"$E2E_MARKER"

version=$(codex --version 2>/dev/null | grep -oE '[0-9]+\.[0-9]+\.[0-9]+' | head -1)
echo "harness=codex version=${version:-unknown} date=$(date -u +%Y-%m-%dT%H:%M:%SZ)"

go build -o examples/kb-shaped/bin/whippletree-hook ./cmd/whippletree-hook

codex plugin marketplace add "$repo_root/examples/kb-shaped"
codex plugin add kb-shaped@kb-shaped-mkt

mkdir -p "$sandbox/proj"
cd "$sandbox/proj"
codex exec --dangerously-bypass-hook-trust --skip-git-repo-check "say hi" </dev/null || true

if grep -q "session-start" "$E2E_MARKER"; then
  echo "PASS: session-start fired on codex"
else
  echo "FAIL: session-start did not fire on codex"
  echo "--- marker contents ---"
  cat "$E2E_MARKER" || true
  exit 1
fi
