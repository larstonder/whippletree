#!/usr/bin/env bash
# Headless e2e: install the kb-shaped example bundle into a fresh,
# isolated codex home and confirm SessionStart fires the vendored
# whippletree-hook end to end against the real codex CLI. Runs fully
# unauthenticated: no auth.json is copied in (deliberate deviation from
# the original brief, avoids refresh-token conflicts against the
# caller's real login). SessionStart is verified (2026-07-29) to fire
# before any auth/model call, so `codex exec ... || true` tolerating
# the auth failure still proves the hook wiring.
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$repo_root"

S=$(mktemp -d)
export CODEX_HOME="$S/home"
mkdir -p "$CODEX_HOME"
export E2E_MARKER="$S/marker.log"
: >"$E2E_MARKER"

go build -o examples/kb-shaped/bin/whippletree-hook ./cmd/whippletree-hook

codex plugin marketplace add "$repo_root/examples/kb-shaped"
codex plugin add kb-shaped@kb-shaped-mkt

mkdir -p "$S/proj"
cd "$S/proj"
codex exec --dangerously-bypass-hook-trust --skip-git-repo-check "say hi" </dev/null || true

if grep -q "session-start" "$E2E_MARKER"; then
  echo "PASS: session-start fired on codex"
else
  echo "FAIL: session-start did not fire on codex"
  echo "--- marker contents ---"
  cat "$E2E_MARKER" || true
  exit 1
fi
