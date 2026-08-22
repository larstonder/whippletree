#!/usr/bin/env bash
# Headless e2e: install the kb-shaped example bundle into a fresh, isolated
# copilot home and confirm the vendored whippletree-hook fires end to end
# against the real copilot CLI, through the marketplace route `install`
# actually prints.
#
# Unlike run-claude.sh and run-codex.sh this needs a working login. The events
# asserted below only fire once an agent takes a turn; whether copilot has any
# pre-auth event was not probed. COPILOT_HOME is still sandboxed so the script
# cannot collide with the caller's plugin state; credentials do not live there.
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$repo_root"

sandbox=$(mktemp -d)
export COPILOT_HOME="$sandbox/home"
mkdir -p "$COPILOT_HOME"
export E2E_MARKER="$sandbox/marker.log"
: >"$E2E_MARKER"

version=$(copilot --version 2>/dev/null | grep -oE '[0-9]+\.[0-9]+\.[0-9]+' | head -1)
echo "harness=copilot version=${version:-unknown} date=$(date -u +%Y-%m-%dT%H:%M:%SZ)"

go build -o examples/kb-shaped/bin/whippletree-hook ./cmd/whippletree-hook

copilot plugin marketplace add "$repo_root/examples/kb-shaped" </dev/null
copilot plugin install kb-shaped@kb-shaped-mkt </dev/null

mkdir -p "$sandbox/proj"
echo "readme contents" >"$sandbox/proj/readme.txt"
cd "$sandbox/proj"
copilot --allow-all-tools -p "Read the file readme.txt and say what it contains." </dev/null || true

fail=0
# session-start proves the hooks file registered at all; file-read proves the
# tool-class alias resolved to Copilot's Claude-named Read tool.
for want in session-start file-read; do
  if grep -q "^$want " "$E2E_MARKER"; then
    echo "PASS: $want fired on copilot"
  else
    echo "FAIL: $want did not fire on copilot"
    fail=1
  fi
done

if [ "$fail" -ne 0 ]; then
  echo "--- marker contents ---"
  cat "$E2E_MARKER" || true
  exit 1
fi
