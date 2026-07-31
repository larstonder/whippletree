#!/usr/bin/env bash
set -euo pipefail

marker="${E2E_MARKER:-/tmp/kb-shaped-marker.log}"
cat >/dev/null

if [ "${ADAPTER_STOP_ACTIVE:-}" = "true" ]; then
  echo "turn-end-allowed" >>"$marker"
  exit 0
fi

echo "capture first" >&2
echo "turn-end-blocked" >>"$marker"
exit 2
